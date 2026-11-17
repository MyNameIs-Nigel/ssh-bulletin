package kit

import (
	"image/color"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Hint is one key binding shown in the footer: {key, label}.
type Hint = [2]string

// Hints renders key chips ("⏎ open  ? help") fitted to width, dropping
// trailing hints that do not fit rather than wrapping.
func Hints(hints []Hint, th Theme, accent []color.Color, width int) string {
	var parts []string
	used := 0
	for i, h := range hints {
		key := lipgloss.NewStyle().Foreground(At(accent, float64(i)/float64(max(1, len(hints)-1)))).Bold(true).Render(h[0])
		part := key + " " + S(th.Muted).Render(h[1])
		w := lipgloss.Width(part) + 3
		if used+w > width {
			break
		}
		used += w
		parts = append(parts, part)
	}
	return strings.Join(parts, S(th.Faint).Render(" · "))
}

// Crumbs renders a breadcrumb trail; the last crumb wears the accent.
func Crumbs(crumbs []string, th Theme, accent []color.Color) string {
	var out []string
	for i, c := range crumbs {
		if i == len(crumbs)-1 {
			out = append(out, GradientBold(c, accent))
		} else {
			out = append(out, S(th.Muted).Render(c))
		}
	}
	return strings.Join(out, S(th.Faint).Render("  ›  "))
}

// Spinner frames: a comet orbiting in braille.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner renders frame f of the spinner in a color drawn from stops.
func Spinner(f int, stops []color.Color) string {
	c := Cyclic(stops, float64(f%40)/40)
	return S(c).Bold(true).Render(spinnerFrames[f%len(spinnerFrames)])
}

// Pulse is a 0..1 breathing value for frame-driven glows.
func Pulse(t time.Duration, period time.Duration) float64 {
	x := float64(t%period) / float64(period)
	if x > 0.5 {
		x = 1 - x
	}
	return x * 2
}

// FrameMsg drives animation. Gen guards against stale ticks: only the
// newest loop's frames keep the loop alive.
type FrameMsg struct{ Gen int }

// FrameRate is low on purpose: every frame crosses the arcade bridge.
const FrameRate = 20

// Frame schedules the next animation frame.
func Frame(gen int) tea.Cmd {
	return tea.Tick(time.Second/FrameRate, func(time.Time) tea.Msg { return FrameMsg{Gen: gen} })
}

// StyleInput themes a bubbles textinput: accent prompt and cursor, no
// blinking (a blinking cursor would keep the render loop awake forever).
func StyleInput(ti *textinput.Model, th Theme, accent []color.Color) {
	st := ti.Styles()
	st.Focused.Prompt = S(At(accent, 0)).Bold(true)
	st.Focused.Text = S(th.Text)
	st.Focused.Placeholder = S(th.Faint).Italic(true)
	st.Blurred.Prompt = S(th.Faint)
	st.Blurred.Text = S(th.Subtle)
	st.Blurred.Placeholder = S(th.Faint).Italic(true)
	st.Cursor.Color = At(accent, 0.5)
	st.Cursor.Blink = false
	ti.SetStyles(st)
}

// StyleArea themes a bubbles textarea the same way.
func StyleArea(ta *textarea.Model, th Theme, accent []color.Color) {
	st := ta.Styles()
	for _, s := range []*textarea.StyleState{&st.Focused, &st.Blurred} {
		s.Base = lipgloss.NewStyle()
		s.Text = S(th.Text)
		s.CursorLine = lipgloss.NewStyle()
		s.CursorLineNumber = S(At(accent, 0.3))
		s.LineNumber = S(th.Faint)
		s.EndOfBuffer = S(th.Faint)
		s.Placeholder = S(th.Faint).Italic(true)
		s.Prompt = S(At(accent, 0.2))
	}
	st.Blurred.Prompt = S(th.Faint)
	st.Blurred.Text = S(th.Subtle)
	st.Cursor.Color = At(accent, 0.5)
	st.Cursor.Blink = false
	ta.SetStyles(st)
}
