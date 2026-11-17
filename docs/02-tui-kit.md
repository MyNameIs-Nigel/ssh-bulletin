# 02 — The TUI kit

`internal/tui/kit` is a small rendering framework on top of bubbletea v2 and
lipgloss v2. The screens in `internal/tui` contain layout and behaviour; every
visual idea lives in the kit.

## Color

- **Truecolor always.** The server forces `colorprofile.TrueColor` per program;
  every color is a 24-bit hex.
- **Palettes** (`color.go`) are lists of gradient stops. Twelve are named
  (aurora, sunset, reef, …); boards and users each pick one, so a post's
  author name, sigil and accents carry *their* palette.
- **Interpolation is in Lab space** (`Lerp`), so gradients stay vivid through
  the middle instead of going muddy as RGB lerps do.
- **Theme** (`theme.go`) holds semantic tokens (Surface, Text, Muted, Line…)
  and a vertical backdrop gradient. Screens never hard-code a color.

## Primitives

| | |
| --- | --- |
| `Gradient`, `GradientBold`, `Rule`, `FadeRule`, `Bar` | per-character gradients; `offset` animates them |
| `GlowRule` | the masthead rule: a gradient with a highlight that sweeps across once after a focus change; sweep 0 and 1 render identically, so it never jumps when it settles |
| `Indent` | shifts every line of a block right (prefixing `" "` to a joined block only indents its first line) |
| `Panel` | rounded box whose border is a gradient flowing clockwise; `Phase` rotates it, `Focused` brightens it |
| `Menu` | list with a spring-animated highlight glow that glides between rows |
| `Pixels` | half-block canvas (two pixels per cell) — `Wordmark`, `Sigil`, `Orb` draw on it |
| `Sigil` | deterministic identicon from a key fingerprint |
| `Scene` | the compositor: base layer + overlays at (x,y), backdrop fill, dimming |
| `Toasts` | slide-in notifications with easeOutBack, auto-expiring |

## Compositing — why `Scene` exists

Every frame is composed onto a full-screen cell canvas. Unstyled cells get the
theme's backdrop gradient, so the background is continuous behind everything.
Overlays (compose, command palette, help) draw on top with the base dimmed.

Two traps found while building it, both covered by `kit_test.go`:

1. lipgloss's `Layer.Draw` clears its whole area, erasing the base frame. The
   compositor draws overlays with `uv.NewStyledString(...).Draw(canvas, rect)`.
2. Wrapping *styled* text leaves continuation lines unstyled, which shows the
   terminal's default color. Wrap plain text first, then style it; the Scene
   also gives any cell with no foreground the theme's text color.

## Animation

One frame loop at 20 fps (`kit.Frame`), guarded by a generation counter so a
stale tick can never start a second loop. It runs **only while something
animates** (a menu glow in flight, a toast, a cross-fading accent, a focus
flow, and the splash/onboarding shimmer for 8 s after the last keypress) and
stops otherwise; a 15 s clock tick handles the idle timeout.
`TestAnimationLoopStops` and `TestAmbientScreensSettle` hold that line — an always-on loop at 20 fps across
every connected session is the main bandwidth and CPU cost this design has.
