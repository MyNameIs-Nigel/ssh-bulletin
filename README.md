# ssh-bulletin

A bulletin board you reach over SSH — part of the **ssharcade** fleet.

```
ssh play.ssharcade.dev     # pick BULLETIN once it is listed in the lobby
```

**Your SSH key is your account.** No password, no email, and — unlike the other
games in the arcade — not your username either: `ssh alice@…` and `ssh bob@…`
with the same key are the same poster. The username is only used to suggest a
handle the first time you connect.

## Profiles and friends

- Press `u` from the boards to search usernames. `Tab` switches between the directory and your private friends list.
- Press `v` on a selected post or reply, or click the author's name, to see their bio and recent activity. Open an activity entry to jump to that post.
- Press `f` on another player's profile to add or remove them as a friend. A blue `F` appears by their name for you.
- Press `p` to edit your profile and choose a flair. New threads and replies use the flair you saved when you post them.

## What's here

| Path | What |
| --- | --- |
| `internal/identity` | Router protocol v2: proxy-key trust, strict `<fp-hex64>.<slot>` parsing, guests refused fail-closed |
| `internal/store` | SQLite (modernc, WAL): accounts, boards, posts, roles, moderation history |
| `internal/board` | Service layer, input cleaning/limits, live-event broker |
| `internal/server` | wish v2 server: middleware stack, session caps by key and direct network, connection rate limit |
| `internal/tui/kit` | **The TUI framework** — truecolor gradients, animated panels, spring menus, a layer compositor, toasts, pixel art |
| `internal/tui` | The screens built on it: splash, onboarding, boards, threads, profile, compose, command palette, moderation |

See [docs/README.md](docs/README.md) for the design notes and the arcade
onboarding steps.

## Develop

```bash
go build ./... && go vet ./... && go test ./...
BULLETIN_DEV_MODE=true BULLETIN_LISTEN_PORT=2226 go run ./cmd/ssh-bulletin
ssh -p 2226 localhost
```

Dev mode seeds demo boards and posts into an empty database. Render the TUI
goldens to inspect them as ANSI:

```bash
BULLETIN_RENDER_DIR=/tmp/renders go test ./internal/tui -run TestRenderGoldens
```

## Configuration

| Variable | Default | |
| --- | --- | --- |
| `BULLETIN_LISTEN_HOST` / `_PORT` | `0.0.0.0` / `22` | `2222` in the image |
| `BULLETIN_HOST_KEY_PATH` | `var/ssh_host_key` | |
| `BULLETIN_DB_PATH` | `var/bulletin.db` | |
| `BULLETIN_PROXY_KEYS_PATH` | — | authorized_keys file of trusted routers; unset = direct only |
| `BULLETIN_IDLE_TIMEOUT` | `30m` | |
| `BULLETIN_MAX_SESSIONS_PER_KEY` | `3` | |
| `BULLETIN_MAX_SESSIONS_PER_SOURCE` | `6` | direct sessions per IPv4 address or IPv6 /64 |
| `BULLETIN_OWNER_FINGERPRINT` | — | optional full SSH SHA-256 fingerprint for initial owner setup |
| `BULLETIN_DEV_MODE` | `false` | seed demo content |
| `BULLETIN_REQUIRE_REPLICATION` | — | entrypoint refuses to start without `LITESTREAM_REPLICA_URL` |
| `BULLETIN_HOST_KEY_SOURCE` | — | read-only host mount the entrypoint seeds the host key from |

## Moderation

Select a post and press `o` to see the actions available to you. Anyone can
view the author's profile, authors can delete their own posts and replies, and
staff can remove content or pin threads. Deleted and removed posts appear as
placeholders in the conversation. Removing an opening post closes the thread
to new replies until staff restore it.

Press `!` to report a post. Staff can open Moderation with `m` to review
reports, see decisions, and restore removed posts. The super admin
can grant and revoke `A` by matching the player's handle and full SSH
fingerprint. See the [moderation proposal and rollout notes](docs/03-moderation.md)
for first-run and upgrade behavior.

The release workflow can deploy Bulletin after the lobby, AWS, and GitHub
setup in the [arcade onboarding guide](docs/01-arcade-onboarding.md).
