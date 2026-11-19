# Working on ssh-bulletin

Bulletin is ssharcade's SSH community board. Players browse threads, post and reply, use live board chat, and moderate content through a Bubble Tea terminal UI. SSH identity connects sessions to accounts, with a separate handle for display.

## Where to work

| Path | Responsibility |
| --- | --- |
| `cmd/ssh-bulletin/`, `internal/app/`, `internal/config/` | Bootstrap, dev seed data, and `BULLETIN_*` settings |
| `internal/server/`, `internal/identity/` | SSH sessions and arcade proxy identity |
| `internal/board/`, `internal/store/` | Board operations, chat event delivery, moderation policy, SQLite, and migrations |
| `internal/markup/` | Rich text and link parsing |
| `internal/tui/`, `internal/tui/kit/` | Screens, overlays, help, navigation, terminal styling, and animation |

Start with [the docs index](docs/README.md). Check the moderation docs for permission or visibility changes and the rich-text docs for formatting or links. Keep board authorization in the service layer. Check both keyboard navigation and mouse interaction when changing focus or hit targets.

## Tools and verification

- Run `go build ./...`, `go vet ./...`, and `go test ./...` with the Go version in `go.mod`. Focus on `./internal/board` for service behavior, `./internal/markup` for parsing, and `./internal/tui/...` for input and rendering.
- Start a local listener with `BULLETIN_DEV_MODE=true` and a temporary database for repeatable demo content. Follow the [terminal tools guide](scripts/tui-tools/README.md) for the full setup.
- Record `scene.json` as a GIF for splash motion and `help.json` as a PNG for onboarding and Help. Use the loopback latency relay to check interaction delays, and compare idle output bytes when changing animation.
- Run `go test ./internal/board -run '^$' -bench BenchmarkChatWithSlowReaders -benchmem` when changing chat fan-out. The benchmark compares an active reader across several counts of stalled subscribers.
- Work on a branch and open a PR against `main`. Check the release and deployment workflows before any production release work.
