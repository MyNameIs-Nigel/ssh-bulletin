# 01 — Adding the bulletin to the arcade

**Goal:** the bulletin appears in the lobby menu and players reach it through the
router's trusted-proxy bridge, on the same durability pattern as the other games.

Nothing in this doc has been applied. Per workspace hard rule 7 the fleet compose
file and `games.toml` belong to `ssh-arcadelobby/deploy/`, so the changes below land
as a PR **there**, not here. Host and IAM steps are Nigel's to run.

## What is already proven

- The server speaks router protocol v2. `internal/server/server_test.go` plays the
  router's half against a real server:
  - proxy-key auth;
  - strict `<fp-hex64>.<slot>` usernames;
  - `ARCADE_PLAYER_KEY`;
  - guest refusal;
  - an untrusted key that sends a compound username lands on its own identity.
- **An end-to-end run through an unmodified router build.** It used a scratch proxy
  key and a scratch registry, never the real key. The results:
  - lobby → BULLETIN → onboarding → account created;
  - the account's fingerprint equals the player's real key, and the session logged
    `proxied=true`;
  - the lobby showed the bulletin's version, read from its SSH banner.
- **The image builds for `linux/amd64` and runs hardened.**
  - It runs as uid 65532 with a read-only rootfs, `cap_drop ALL` and
    `no-new-privileges`.
  - The DB is created 0600.

## 1. Registry entry — `ssh-arcadelobby/deploy/games.toml`

```toml
[[games]]
id      = "bulletin"
allow_guests = false # the bulletin refuses guest.<token> fail-closed anyway
name    = "BULLETIN"
tagline = "Post under your key."
descriptors = ["community", "boards", "live"]
addr    = "bulletin:2222"
host_key = ""
order   = 40
# No `version`: read live from the SSH banner by the prober.
```

You can list it before the container exists. The menu then shows
`○ BULLETIN OFFLINE`, which is the designed behaviour.

Add the dev twin to `ssh-arcadelobby/games.local.toml` with
`addr = "127.0.0.1:2226"`. To include the bulletin in `scripts/dev-local-fleet.sh`,
add a `bulletin_host_key` to its key loop and three pieces beside the existing ones:

```bash
BULLETIN="$(cd "$ARCADE/../ssh-bulletin" && pwd)"                          # with FARM=, MOON=, CHESS=
(cd "$BULLETIN" && go build -o "$VAR/ssh-bulletin" ./cmd/ssh-bulletin)    # in the build block

BULLETIN_LISTEN_HOST=127.0.0.1 BULLETIN_LISTEN_PORT=2226 BULLETIN_HOST_KEY_PATH="$VAR/bulletin_host_key" \
BULLETIN_DB_PATH="$VAR/bulletin.db" BULLETIN_PROXY_KEYS_PATH="$VAR/proxy_keys" \
BULLETIN_DEV_MODE=true BULLETIN_LOG_LEVEL=info \
  "$VAR/ssh-bulletin" &
PIDS+=("$!")
```

## 2. Compose service — `ssh-arcadelobby/deploy/docker-compose.yml`

```yaml
  bulletin:
    image: ${BULLETIN_IMAGE:-ghcr.io/mynameis-nigel/ssh-bulletin:latest}
    environment:
      BULLETIN_LISTEN_PORT: "2222"
      BULLETIN_HOST_KEY_PATH: /var/lib/bulletin/ssh_host_key
      BULLETIN_DB_PATH: /var/lib/bulletin/bulletin.db
      # Every connection arrives from the router's one IP; the router does the
      # public throttling. The default (2/s) would rate-limit the whole arcade.
      BULLETIN_RATE_LIMIT_PER_SECOND: "50"
      BULLETIN_PROXY_KEYS_PATH: /etc/bulletin/proxy_keys
      LITESTREAM_REPLICA_URL: s3://ssharcade-snoigel-data/bulletin/db
      BULLETIN_REQUIRE_REPLICATION: "true"
      LITESTREAM_S3_REGION: us-east-1
      BULLETIN_HOST_KEY_SOURCE: /etc/bulletin/hostkey/ssh_host_key
    volumes:
      - bulletin-data:/var/lib/bulletin
      - ./private/keys/bulletin:/etc/bulletin/hostkey:ro
      - ./proxy_keys:/etc/bulletin/proxy_keys:ro # PUBLIC half of the router's proxy key
    networks: [ssharcade]
    restart: unless-stopped
    stop_grace_period: 45s
    read_only: true
    tmpfs:
      - /tmp
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
    mem_limit: 256m
    cpus: 1.0
```

…and `bulletin-data: {}` under the top-level `volumes:`. The service has no `ports:`.
The bulletin is reachable only on the private network.

The volume will be `ssharcade_bulletin-data`. It follows the game pattern, unlike the
router's.

## 3. Host provisioning (Nigel)

The host key is mounted read-only, the same way as the other three games. The
entrypoint seeds the volume from it only when the volume has no key. If the source is
unreadable, it is FATAL.

```bash
sudo install -d -m 0711 /srv/ssharcade/private/keys/bulletin
sudo ssh-keygen -t ed25519 -N '' -C ssharcade-bulletin -f /srv/ssharcade/private/keys/bulletin/ssh_host_key
sudo chown 65532:65532 /srv/ssharcade/private/keys/bulletin/ssh_host_key
sudo chmod 0400 /srv/ssharcade/private/keys/bulletin/ssh_host_key
sudo rm /srv/ssharcade/private/keys/bulletin/ssh_host_key.pub   # not needed on the mount
```

The instance role's `ssharcade-litestream-s3-policy` must cover the `bulletin/` prefix.
**Check its resource list before the first deploy.** If it enumerates per-game
prefixes rather than the whole bucket, `BULLETIN_REQUIRE_REPLICATION=true` will stop
the container from starting. That is fail-closed, as intended, but confusing if you
don't expect it.

Then, once the image exists:

```bash
cd /srv/ssharcade && docker compose up -d bulletin
```

## 4. Deploy (later)

`release.yml` stops at **publish**. To add the SSM deploy job (mirror ssh-farm's):

1. Add `repo:MyNameIs-Nigel@38890734/ssh-bulletin@<repo-id>:environment:production`
   to the `ssharcade-deploy` trust policy, plus the name-only form.
   - Get `<repo-id>` from `gh api repos/mynameis-nigel/ssh-bulletin --jq .id`.
   - Better still, read it from the CloudTrail `AssumeRoleWithWebIdentity` event of
     the first failed run, as the workspace CLAUDE.md describes.
2. Create the `production` environment with a `main`-only deployment branch policy.
   Because `sub` pins the environment and not the branch, that policy is what
   restricts deploys to `main`.
3. Set the `ARCADE_DEPLOY_ROLE` and `ARCADE_INSTANCE_ID` repo variables.
4. Add `scripts/deploy/remote-deploy.sh` (from farm) and the `deploy` job.

## 5. After the first deploy

Run the §4.5-style checks:

- the container env has `BULLETIN_REQUIRE_REPLICATION=true`. Inspect the
  **running container**, not compose:
  `docker inspect ssharcade-bulletin-1 --format '{{json .Config.Env}}' | tr ',' '\n' | grep REQUIRE_REPLICATION`
- WAL objects appear under `s3://ssharcade-snoigel-data/bulletin/db/`;
- the lobby shows BULLETIN online with its version.

Add `bulletin` to `deploy/monitoring/`'s game list, so the replication check covers it.
