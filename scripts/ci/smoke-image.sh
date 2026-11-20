#!/bin/sh
# Check the image entrypoint, writable data volume, and release banner together.
set -eu
image=${1:?image required}
version=${2:?version required}
container=$(docker run -d --read-only --tmpfs /tmp --cap-drop ALL \
  --security-opt no-new-privileges \
  -e BULLETIN_OWNER_FINGERPRINT=SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA \
  "$image")
trap '
  if [ "$?" -ne 0 ]; then docker logs "$container" >&2 || true; fi
  docker rm -fv "$container" >/dev/null
' EXIT

for _ in $(seq 1 15); do
  banner=$(docker exec "$container" sh -c 'timeout 2 nc 127.0.0.1 2222 </dev/null | head -n 1' | tr -d '\r')
  if [ "$banner" = "SSH-2.0-$version" ]; then exit 0; fi
  sleep 1
done
echo "Expected SSH-2.0-$version, got ${banner:-no banner}" >&2
exit 1
