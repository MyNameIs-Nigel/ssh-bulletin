#!/bin/sh
# Runs on the arcade EC2 host via SSM.
set -eu
: "${IMAGE:?}" "${VERSION:?}"
repo=ghcr.io/mynameis-nigel/ssh-bulletin
digest=${IMAGE#"$repo"@sha256:}
[ "$digest" != "$IMAGE" ]
printf '%s\n' "$digest" | grep -Eq '^[0-9a-f]{64}$'
printf '%s\n' "$VERSION" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
cd "${COMPOSE_DIR:-/srv/ssharcade}"
# Shared with the other arcade repositories.
exec 9>.deploy.lock
flock -w 600 9

if ! docker compose config --services | grep -Fxq bulletin; then
  echo 'Release the lobby configuration with the bulletin service first.' >&2
  exit 1
fi
unset BULLETIN_IMAGE

banner() {
  docker compose exec -T bulletin sh -c 'timeout 5 nc 127.0.0.1 2222 </dev/null | head -n 1' | tr -d '\r\n'
}

wait_banner() {
  for _ in $(seq 1 "${PROBE_ATTEMPTS:-30}"); do
    if [ "$(banner 2>/dev/null)" = "$1" ]; then return 0; fi
    sleep 2
  done
  return 1
}

start() {
  docker compose up -d --no-deps --pull never bulletin
}

set_image() {
  target=$(mktemp .env.bulletin.XXXXXX) || return 1
  if ! {
    if [ -f .env ]; then
      awk '!/^[[:space:]]*(export[[:space:]]+)?BULLETIN_IMAGE([[:space:]]*[=:]|[[:space:]]*$)/' .env
    fi &&
      printf 'BULLETIN_IMAGE=%s\n' "$1"
  } > "$target" || ! mv "$target" .env; then
    rm -f "$target"
    return 1
  fi
}

previous=''
previous_banner=''
container=$(docker compose ps --status running --quiet bulletin)
if [ -n "$container" ]; then
  previous_banner=$(banner)
  case "$previous_banner" in
    SSH-2.0-?*) ;;
    *) echo 'Cannot probe the running Bulletin; stopping rollout.' >&2; exit 1 ;;
  esac
  previous=$(docker inspect --format '{{.Config.Image}}' "$container")
  case "$previous" in
    "$repo"@sha256:*) ;;
    *)
      # Pulling the candidate can change what a mutable tag resolves to.
      running_image=$(docker inspect --format '{{.Image}}' "$container")
      previous="$repo:rollback"
      docker tag "$running_image" "$previous"
      ;;
  esac
fi

# A pull failure leaves the service and fleet configuration intact.
docker pull --quiet "$IMAGE"

backup=$(mktemp -d)
changed=0
restarted=0
rollback() {
  echo 'Rollout failed; restoring the prior Bulletin configuration.' >&2
  if [ -f "$backup/.env" ]; then
    mv "$backup/.env" .env || return 1
  else
    rm -f .env || return 1
  fi
  if [ "$restarted" = 0 ]; then return 0; fi
  if [ -z "$previous" ]; then
    docker compose stop bulletin || return 1
    return 0
  fi
  set_image "$previous" && start && wait_banner "$previous_banner" || return 1
  echo 'Previous Bulletin build restored.'
}
cleanup() {
  status=$?
  trap - EXIT
  if [ "$status" -ne 0 ] && [ "$changed" = 1 ]; then
    rollback || echo 'ROLLBACK FAILED: inspect Bulletin on the fleet host.' >&2
  fi
  rm -rf "$backup"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
if [ -f .env ]; then cp -p .env "$backup/.env"; fi

changed=1
set_image "$IMAGE"
if [ "$(docker compose config --images bulletin)" != "$IMAGE" ]; then
  echo 'Bulletin compose service must use BULLETIN_IMAGE.' >&2
  exit 1
fi
restarted=1
start
if ! wait_banner "SSH-2.0-$VERSION"; then
  echo "Bulletin did not advertise release $VERSION." >&2
  exit 1
fi
changed=0
echo "Deployed Bulletin $VERSION ($IMAGE)"
