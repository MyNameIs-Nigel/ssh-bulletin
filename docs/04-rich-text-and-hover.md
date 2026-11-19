# 04 — Rich text, links and hover

Posts and chat take a small Markdown dialect. References to users, boards,
threads and posts become links you can follow with the keyboard or the
mouse, and anything interactive lights up when the pointer rests on it.

## Formatting

`internal/markup` parses it. It is line-oriented on purpose: every line the
author typed stays a line, as it always has in the bulletin, and a line's
leading marker decides what it is.

| Typed | Shown as |
| --- | --- |
| `**bold**`, `*italic*` / `_italic_`, `~~strike~~` | emphasis (underscores only at word edges, so `snake_case` survives) |
| `` `code` `` | a tinted code span; nothing inside it is parsed |
| `- item`, `* item`, `+ item` (indent 2 spaces per level) | bullets `•` `◦` `▪` with hanging indents |
| `1. item`, `1) item` | numbered items |
| `> text` | a quote with a bar in the board's colors |
| `# Title` (1–3 `#` and a space) | a heading; `#lobby` with no space stays a board link |
| a fenced block between lines starting with ```` ``` ```` | code lines, shown verbatim |
| `---` alone on a line | a rule |
| `\*` | a literal `*` (any ASCII punctuation can be escaped) |

Chat messages and bios are one line, so they take the inline forms only: a
chat line starting with `- ` is a dash, not a list.

Parsing never fails and never drops text. Anything that is not markup shows
as typed, and the stored body is unchanged: formatting is applied when it is
drawn, so a rendering change never needs a migration. The moderation queue
still shows the raw text, so staff see exactly what was written, including
the real target behind a `[label](link)`.

## Links

| Typed | Goes to | Looks like |
| --- | --- | --- |
| `@handle` | the user's profile | bold, in the user's palette |
| `#slug` | the board | bold, in the board's palette |
| `#12` | thread 12 | bold `#12` in its board's palette, followed by the thread's title in italics |
| `#12/3` | the third post of thread 12 (the opening post is 1) | as above, landing on that post |
| `https://…`, `http://…` | outside the bulletin, after a warning | underlined in a fixed link blue, scheme dropped, ending in `↗` |
| `[label](@handle)`, `[label](#12)`, `[label](https://…)` | the target | the label, in the target's look |

The two looks never overlap: internal links are bold gradients and never
underlined, while external links are always underlined and always end in
`↗`. A thread number gains its title so `#12` tells you where it goes before
you follow it.

**References are resolved, not trusted.** When posts, a preview, chat
messages or a bio load, the same command asks the board service to resolve
every reference in them (`Service.ResolveText` / `ResolveLines`): handles
case-insensitively, board slugs, thread ids, and a post's position in its
thread. Only what exists becomes a link; `@nobody`, `#nowhere` and `#99999`
stay plain text. One load resolves at most `board.MaxRefsPerLoad` (64)
internal references, and the rest stay plain, so a post full of `#1 #2 #3…`
cannot make a load do unbounded work. Results are merged into the session's
`refs` map, and each reload resolves its content again, so a renamed user
or a removed thread is reflected the next time the content loads.

Post positions are stable because posts are never hard-deleted: a deleted or
removed post leaves a placeholder in place. In a thread, the post options
menu has **Reply linking this post**, which opens a reply that starts with
`#12/3`. The thread header shows the thread's `#id`.

### External links

Following an external link only ever opens a warning. It says that someone
posted the link and nobody has checked it, shows the host on its own line,
and shows the full address. That matters because the text of a link can
lie. `[google.com](https://evil.example)` shows a friendly label, and
`https://google.com@evil.example` puts a familiar name in front of the real
host. A host with letters from other alphabets, such as a Cyrillic `а`
standing in for `a`, gets an extra line of warning.

From the warning, `c` copies the address with OSC 52 (`tea.SetClipboard`),
which most terminals pass to the system clipboard. The address is also
drawn as a real OSC 8 hyperlink, so ctrl- or cmd-click opens it in
terminals that support links. Links in posts are never real hyperlinks,
because a terminal's own link handling would skip the warning.

The startup splash's art credit (`jgs`) opens the warning. The idle splash
includes a terminal hyperlink labeled `asciiart.eu` to the cat's source.

## Keyboard

- **Thread:** `tab` and `shift+tab` walk the links in the selected post,
  wrapping around. `⏎` follows the focused link; with no link focused it
  replies, as before. `esc` puts the link down first, and moving to
  another post does too.
- **Chat:** `tab` moves focus from the input to a message, then through
  that message's links, then back to the input, like focus moving through
  a web page. `shift+tab` goes back. `⏎` follows the focused link. With no
  link focused, `⏎` or `o` opens the message's options.
- The footer swaps in `⏎ open link`, `tab next link` and `esc done` while a
  link is focused.

## Hover

### What it takes

Hover needs the terminal to report the pointer when it moves, not just when
a button is pressed. That is xterm's "any event" mouse tracking (DEC mode
1003, `tea.MouseModeAllMotion`). Before this change the bulletin asked
for cell motion (mode 1002), which reports motion only during a drag.
Mainstream terminals support mode 1003, including iTerm2, kitty, WezTerm,
Alacritty, Windows Terminal and GNOME Terminal. A terminal that doesn't
loses nothing but the hover: clicks and the keyboard work as before.
Nothing changes on the arcade router. It relays the session's bytes both ways
(`ssh-arcadelobby/internal/bridge`) and already turns mode 1003 off when a
game exits (`terminalReset` in `ssh-arcadelobby/internal/server/handler.go`).

The costs, and what keeps them small:

| Cost | Without care | What the bulletin does |
| --- | --- | --- |
| Input | a motion report for every cell the pointer crosses, a few bytes each, inbound over SSH | negligible next to output |
| CPU | every report runs `Update` and `View`; a full frame is about 2.4 ms at 160×50 | a motion that does not change what is lit reuses the last frame (`Model.reuse`, `frameState.view`): about 0.04 ms |
| Output | an animated highlight redraws while it moves | only the changed cells cross the wire (the renderer diffs), and the flow stops `hoverFlow` (6 s) after the last input |
| Hit-testing | a second copy of each screen's layout math, which drifts from the drawing | the frame itself is the hit map (below) |

`TestAnimationLoopStops`, `TestHoverLightsAndClickFollows` (which also
checks that a resting hover settles) and the frame-reuse assertion in the
same test hold those lines.

### Hot spots

Anything interactive is drawn through `Model.hot(key, run, text)`, which
wraps the text with `kit.Hot`, a zero-width OSC 8 mark on a private
`bulletin-hot:` scheme, and records `run` as what a click does. Layout
code needs no changes for this. The marks have no width, truncation keeps
them, and wrapping, panels and the menu glow carry them along untouched.
When the outer `kit.Scene` composites the frame, it reads every mark off the
finished cells into a `kit.HotMap` and erases them, so they never reach the
terminal. A click or hover is then a lookup in the frame the player is
actually looking at. While a modal is open (`Scene.Modal`), the scene strips
the marks from the frame behind it, so only the modal is interactive.

What is a hot spot now:

- every link in posts, the board preview pane, chat (screen and preview pane) and bios;
- author names on post cards, chat headers, the preview pane, the home screen's recent threads, and your own name in the masthead (your profile);
- breadcrumbs, except the one for where you are;
- footer hints and the hints inside compose, options and the link warning. A click presses the key, so a click and a key press always do the same thing (`↑↓` and other ranges are drawn but not hot);
- list rows: boards, threads, the user directory, the jump palette and options menus. Hovering lights a row without moving the selection, the first click selects it, and a click on the selected row opens it;
- the board screen's chat section, the home screen's recent-thread titles, profile activity rows, and the splash credit.

### Lit

The pointer and the keyboard light things the same way, through
`kit.Flow`. The gradient runs (`kit.FlowPhase`, 0.55 cycles a second) over
a faint tint of its own colour, and the text is underlined. List rows flow
their title and show a thin rail. The flow starts over whenever something
new lights up (`Model.litAt`), and after it settles a redraw keeps the
settled phase rather than jumping.

Hover is cleared when the screen or overlay changes under a key press or a
click, and when the wheel scrolls content under the pointer. It lights up
again on the next motion. That keeps a stale highlight from landing on
whatever the new view happens to draw at the same key.

## Follow-ups

- A navigation history, so `esc` after following a link returns to where you were rather than to the link target's parent.
- A preview pane in compose.
- Notifying a user when they are `@mentioned`.
