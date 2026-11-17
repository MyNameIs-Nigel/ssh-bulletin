# ssh-bulletin

A bulletin board you reach over SSH — part of the **ssharcade** fleet.

```
ssh play.ssharcade.dev     # then pick BULLETIN in the lobby
```

**Your SSH key is your account.** No password, no email, and — unlike the other
games in the arcade — not your username either: `ssh alice@…` and `ssh bob@…`
with the same key are the same poster. The username is only used to suggest a
handle the first time you connect.

## What's here

| Path | What |
| --- | --- |
| `internal/identity` | Router protocol v2: proxy-key trust, strict `<fp-hex64>.<slot>` parsing, guests refused fail-closed |
| `internal/store` | SQLite (modernc, WAL): accounts, boards, threads, posts, kudos |
| `internal/board` | Service layer, input cleaning/limits, live-event broker |
| `internal/server` | wish v2 server: middleware stack, per-key session caps, rate limit |
| `internal/tui/kit` | **The TUI framework** — truecolor gradients, animated panels, spring menus, a layer compositor, toasts, pixel art |
| `internal/tui` | The screens built on it: splash, onboarding, boards, threads, profile, compose, command palette |

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
| `BULLETIN_DEV_MODE` | `false` | seed demo content |
| `BULLETIN_REQUIRE_REPLICATION` | — | entrypoint refuses to start without `LITESTREAM_REPLICA_URL` |
| `BULLETIN_HOST_KEY_SOURCE` | — | read-only host mount the entrypoint seeds the host key from |

## Not yet

Moderation, editing/deleting posts, and the deploy job are deliberately out of
scope for the scaffold — see [docs/README.md](docs/README.md).
