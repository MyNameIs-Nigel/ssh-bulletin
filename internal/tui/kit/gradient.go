package kit

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// Gradient paints s one grapheme at a time along stops. offset shifts the
// gradient (cyclically), which is how titles shimmer: bump offset per frame.
// base supplies everything except the foreground (bold, background, ...).
func Gradient(s string, base lipgloss.Style, stops []color.Color, offset float64) string {
	gs := graphemes(s)
	if len(gs) == 0 {
		return ""
	}
	var b strings.Builder
	n := float64(max(len(gs)-1, 1))
	for i, g := range gs {
		var c color.Color
		if offset == 0 {
			c = At(stops, float64(i)/n)
		} else {
			c = Cyclic(stops, float64(i)/n*0.5+offset)
		}
		b.WriteString(base.Foreground(c).Render(g))
	}
	return b.String()
}

// GradientBold is Gradient with bold text — the common case for names.
func GradientBold(s string, stops []color.Color) string {
	return Gradient(s, lipgloss.NewStyle().Bold(true), stops, 0)
}

// Rule draws a width-wide horizontal line in ch along a gradient.
func Rule(width int, ch string, stops []color.Color, offset float64) string {
	if width <= 0 {
		return ""
	}
	return Gradient(strings.Repeat(ch, width), lipgloss.NewStyle(), stops, offset)
}

// FadeRule draws a rule that fades from c into the backdrop at both ends —
// a glowing divider rather than a hard line.
func FadeRule(width int, ch string, c, bg color.Color) string {
	if width <= 0 {
		return ""
	}
	stops := []color.Color{bg, c, c, bg}
	return Rule(width, ch, stops, 0)
}

// Bar paints a width-wide background gradient with text laid over it, ink
// chosen per cell for contrast. Used for selected rows and chips.
func Bar(text string, width int, stops []color.Color, bold bool) string {
	if width <= 0 {
		return ""
	}
	text = ansi.Truncate(text, width, "…")
	gs := graphemes(text)
	pad := width - lipgloss.Width(text)
	for range pad {
		gs = append(gs, " ")
	}
	var b strings.Builder
	n := float64(max(len(gs)-1, 1))
	for i, g := range gs {
		bg := At(stops, float64(i)/n)
		st := lipgloss.NewStyle().Background(bg).Foreground(Ink(bg)).Bold(bold)
		b.WriteString(st.Render(g))
	}
	return b.String()
}

// Chip is a pill: text on a solid fill with half-block caps that soften its
// ends. Only block elements are used — no Nerd Font glyphs, since players
// arrive with whatever font their terminal has.
func Chip(text string, fill color.Color, bold bool) string {
	cap := lipgloss.NewStyle().Foreground(fill)
	body := lipgloss.NewStyle().Background(fill).Foreground(Ink(fill)).Bold(bold)
	return cap.Render("▐") + body.Render(text) + cap.Render("▌")
}

// GradientChip is a Chip whose fill flows along stops.
func GradientChip(text string, stops []color.Color) string {
	w := lipgloss.Width(text)
	return S(At(stops, 0)).Render("▐") + Bar(text, w, stops, true) + S(At(stops, 1)).Render("▌")
}

func graphemes(s string) []string {
	var out []string
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		out = append(out, g.Str())
	}
	return out
}
