# 01 — Adding the bulletin to the arcade

**Goal:** the bulletin appears in the lobby menu and players reach it through the
router's trusted-proxy bridge, on the same durability pattern as the other games.

Fleet configuration lives in `ssh-arcadelobby/deploy/`. Submit the Compose
service and registry entry together in a lobby PR. Apply the host and IAM
settings before enabling the Bulletin deploy job.

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
      BULLETIN_OWNER_FINGERPRINT: ${BULLETIN_OWNER_FINGERPRINT:-}
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

Make sure `/srv/ssharcade/proxy_keys` contains the router's proxy public key
and is readable by the container. Confirm that the EC2 instance profile's
`ssharcade-litestream-s3-policy` allows bucket listing for the Bulletin prefix
and object access under `bulletin/db/`. Litestream must be able to restore
and replicate with that role.

### Reserve and verify the super admin

1. On the intended owner's machine, run `ssh-keygen -l -E sha256 -f ~/.ssh/id_ed25519.pub`.
   Use the public key for the identity that will sign in to Bulletin.
2. On the fleet host, set `BULLETIN_OWNER_FINGERPRINT` in `/srv/ssharcade/.env`
   to the full `SHA256:…` value. Include the environment mapping shown in the
   Bulletin Compose service above. Run `docker compose config --format json`
   and check `services.bulletin.environment.BULLETIN_OWNER_FINGERPRINT`
   against the fingerprint from step 1.
3. Keep public access to Bulletin closed during verification. After starting
   the service, sign in with the intended key through the restricted listener
   or a staging lobby and finish onboarding. Open **View your profile** from
   the `/` menu and confirm it shows `SU` and the expected fingerprint. Sign in
   with a second key and confirm it has no staff badge. Then open public access.

Bulletin reserves the configured key before it opens the listener. Startup
fails if the value is missing, malformed, or different from the owner already
recorded in the database. For an existing deployment, resolve an owner
mismatch before rollout and preserve the production database.

## 4. Enable deployment and publish a release

1. Extend the `ssharcade-deploy` role's GitHub OIDC trust to the Bulletin
   repository's `production` environment. Match the fleet's existing
   subject format. For an ID-based subject, fetch the repo ID with
   `gh api repos/MyNameIs-Nigel/ssh-bulletin --jq .id` and use
   `repo:MyNameIs-Nigel@38890734/ssh-bulletin@<repo-id>:environment:production`.
   If the repo uses the name-only subject, use
   `repo:MyNameIs-Nigel/ssh-bulletin:environment:production`.
2. In `ssh-bulletin`, create the `production` GitHub environment and permit
   release **tags** matching `v*` in its deployment policy. Restrict who can
   create release tags with a repository ruleset. The release job verifies the
   exact semantic version, tag commit, and `main` ancestry before publishing.
3. Set the GitHub Actions variables `ARCADE_DEPLOY_ROLE` (role ARN) and
   `ARCADE_INSTANCE_ID` (fleet EC2 instance ID). If the host pulls images
   anonymously, publish the first release with deployment disabled, then make
   the Bulletin GHCR package public before enabling deployment.
4. Deploy the updated lobby release so the fleet host has the new Compose
   service and registry entry. The lobby can show BULLETIN offline until the
   service starts. At this point the host key and instance S3 permissions
   should already be in place.
5. Set the **repository-level** Actions variable `ARCADE_DEPLOY_ENABLED=true`
   and publish a stable `vMAJOR.MINOR.PATCH` release from `main`. While the
   switch is unset, stable releases still smoke-test, scan, attest, and tag
   images in GHCR. The deploy job pulls the approved image by digest over
   SSM, writes `BULLETIN_IMAGE` in the fleet's `.env`, and starts only
   Bulletin. It checks the SSH banner for the expected version and restores
   the previous build if that check fails.

## 5. Verify the lobby and replication

- Check the running container for `BULLETIN_REQUIRE_REPLICATION=true` using
  `docker inspect ssharcade-bulletin-1 --format '{{json .Config.Env}}'`.
- Connect with a player key using `ssh play.ssharcade.dev`. Confirm BULLETIN
  is online, shows the release version, and opens under the player's key.
- Make a test write and check for WAL objects under
  `s3://ssharcade-snoigel-data/bulletin/db/`. Add `bulletin` to the game list
  in `ssh-arcadelobby/deploy/monitoring/replication-check.sh` once it is live.
