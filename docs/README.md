# ssh-bulletin docs

| Doc | What |
| --- | --- |
| [01-arcade-onboarding.md](01-arcade-onboarding.md) | Adding the bulletin to the fleet: games.toml, compose, host keys, deploy role |
| [02-tui-kit.md](02-tui-kit.md) | The `internal/tui/kit` framework: how rendering, color and animation work |

## Deliberately deferred

- **Moderation.** No word filter, reporting, or admin tooling yet. Input is
  cleaned of control/escape/bidi characters and length-capped
  (`internal/board/text.go`) — that is a terminal-safety measure, not
  moderation. When it lands it should follow farm's pattern: a list loaded
  from a host path that never enters git (workspace hard rule 5).
- **Edit / delete** of posts and threads.
- **Deploy job.** `release.yml` stops at publish. See doc 01 § Deploy.
