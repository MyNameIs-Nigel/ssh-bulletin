# ssh-bulletin

[![CI](https://github.com/MyNameIs-Nigel/ssh-bulletin/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/MyNameIs-Nigel/ssh-bulletin/actions/workflows/ci.yml)
[![Release](https://github.com/MyNameIs-Nigel/ssh-bulletin/actions/workflows/release.yml/badge.svg?event=release)](https://github.com/MyNameIs-Nigel/ssh-bulletin/actions/workflows/release.yml)
[![Latest release](https://img.shields.io/github/v/release/MyNameIs-Nigel/ssh-bulletin?label=latest%20release)](https://github.com/MyNameIs-Nigel/ssh-bulletin/releases/latest)

A bulletin board you reach over SSH — part of the **ssharcade** fleet.

```
ssh ssharcade.dev     # pick BULLETIN in the lobby
```

**Your SSH key is your account.** No password, no email, and — unlike the other
games in the arcade — not your username either: `ssh alice@…` and `ssh bob@…`
with the same key are the same poster. The username is only used to suggest a
handle the first time you connect.

Open **Start here / introduce yourself** at the top of the Lobby for the
community rules and an invitation to say hello. Posts and chat are public.
Use **Contact staff** in the jump menu for a private message.

## Profiles and friends

- Press `u` from the boards to search usernames. `Tab` switches between the directory and your private friends list.
- Press `v` on a selected post or reply, or click the author's name, to see their bio and recent activity. Open an activity entry to jump to that post.
- Press `f` on another player's profile to add or remove them as a friend. A blue `F` appears by their name for you.
- Use **Mute player** in a profile, post, or chat message's `o` menu to hide that player's chat and collapse their posts. Press `z` or click the collapsed post to reveal it. Unmute from a profile or post options.
- Press `p` to edit your profile and choose a flair. New threads and replies use the flair you saved when you post them.

## Formatting and links

New-thread and reply drafts autosave as you type. Reopen the composer on
the same board or thread after reconnecting. `esc` saves and closes it, `ctrl+x`
discards it after confirmation, and `ctrl+s` posts it.

Posts and chat take light Markdown: `**bold**`, `*italic*`, `` `code` ``,
`~~strike~~`, `- lists`, `1. lists`, `> quotes`, headings and fenced code.
They also take links:

- `@handle` links a user, `#lobby` a board, `#12` a thread and `#12/3` the third post in it. A link only appears when its target exists. Internal links are bold and wear the colours of the user or board they point to.
- `https://…` links leave the bulletin. They are underlined, end in `↗`, and always open a warning first that shows where they really go. From the warning you can copy the address, or ctrl/cmd-click it if your terminal opens links.
- In a thread, `tab` walks the links in the selected post and `⏎` follows one. In chat, `tab` moves from the input to the selected message's links.
- With a mouse, links, names, list rows, breadcrumbs and the key hints in the footer all light up under the pointer, and a click does what the key would.

See [docs/04-rich-text-and-hover.md](docs/04-rich-text-and-hover.md) for the full syntax and how hover works.

Press `s` from Boards to search thread titles and post text. Open a result
to jump to the matching post. Press `esc` to return to the results.

Choose **Edit opening post** or **Edit reply** from your own post's options.
Saved changes display an `edited` indicator. Staff can review previous
versions in Moderation History.

## Inbox

Press `i` from Boards to open your inbox. It groups replies to your threads,
direct replies made with **Reply linking this post**, and `@handle` mentions
in forum posts. Open a thread group and select an entry to jump to that post
and mark it read. Unread counts survive reconnects.

Press `m` in the inbox to mute a thread. Use the thread's `o` menu to unmute
it. Activity from muted players and muted threads does not alert you.

## Live chat

Every board has a live chat under its thread list.

- Press `tab` on a board, or `↓` past the last thread, to focus the chat section, then `⏎` to join. The jump palette (`/`) also lists a chat for every board.
- In the chat, everything you type goes to the message box. `⏎` sends, `↑↓` and `pgup`/`pgdn` scroll, and `ctrl+k` opens the jump palette.
- The chat opens at the newest message and follows the conversation. If you scroll up to read history, new messages wait below with a count, and older history loads as you reach the top. `esc` jumps back to the latest, and a second `esc` leaves.
- `tab` switches to picking a message. Then `o` gives you options: view the author's profile, delete your own message, or, for staff, remove someone else's.

## What's here

| Path | What |
| --- | --- |
| `internal/identity` | Router protocol v2: proxy-key trust, strict `<fp-hex64>.<slot>` parsing, guests refused fail-closed |
| `internal/store` | SQLite (modernc, WAL): accounts, boards, posts, roles, moderation history |
| `internal/board` | Service layer, input cleaning/limits, live-event broker, link resolution |
| `internal/markup` | The post and chat Markdown dialect and its `@user` / `#board` / `#12` references |
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
| `BULLETIN_OWNER_FINGERPRINT` | — | full OpenSSH SHA-256 fingerprint of the super admin, required outside dev mode |
| `BULLETIN_DEV_MODE` | `false` | seed demo content and allow first-session ownership when no owner key is set |
| `BULLETIN_REQUIRE_REPLICATION` | — | entrypoint refuses to start without `LITESTREAM_REPLICA_URL` |
| `BULLETIN_HOST_KEY_SOURCE` | — | read-only host mount the entrypoint seeds the host key from |

## Moderation

Select a post and press `o` to see the actions available to you. Anyone can
view the author's profile, authors can delete their own posts and replies, and
staff can remove content or pin threads. Deleted and removed posts appear as
placeholders in the conversation. Staff can lock a thread from its options
to pause replies. The thread stays readable and shows the lock reason.

Choose **Report post** in its `o` menu or press `!`. Chat messages and other
players' profiles also have a Report action. Use **Contact staff** in the `/` jump menu
to send a private message or request review of a restriction. In chat, open
the jump menu with `ctrl+k`.

Staff can open Moderation with `m` to review reports, see decisions, and
restore removed posts. Staff can time out or suspend a player from their
profile options. The super admin can grant and revoke `A` by matching the
player's handle and full SSH fingerprint. See the [moderation guide](docs/03-moderation.md).

The release workflow can deploy Bulletin after the lobby, AWS, and GitHub
setup in the [arcade onboarding guide](docs/01-arcade-onboarding.md).
