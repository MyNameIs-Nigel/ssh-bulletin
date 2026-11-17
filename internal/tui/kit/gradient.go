package kit

import (
	"image/color"
	"math"
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

// GlowRule draws a width-wide rule along stops that dissolves into bg at
// both ends, with a bright highlight crossing it. sweep runs 0..1: the
// highlight enters from the left at 0 and has left on the right by 1, so
// sweep 0 and sweep 1 draw the same resting rule and an animation that ends
// on either never jumps.
func GlowRule(width int, ch string, stops []color.Color, bg color.Color, sweep float64) string {
	if width <= 0 {
		return ""
	}
	const band = 0.18 // highlight half-width, as a fraction of the rule
	fade := max(1, min(width/6, 16))
	center := -band + sweep*(1+2*band)
	var b strings.Builder
	n := float64(max(width-1, 1))
	for i := range width {
		x := float64(i) / n
		c := At(stops, x)
		if d := math.Abs(x-center) / band; d < 1 {
			k := (1 - d) * (1 - d)
			c = Lerp(c, Lift(c, 0.6), k)
		}
		// Ease into the backdrop over the last few cells at each end.
		edge := min(i, width-1-i)
		if edge < fade {
			e := float64(edge+1) / float64(fade+1)
			c = Lerp(bg, c, e*e*(3-2*e))
		}
		b.WriteString(lipgloss.NewStyle().Foreground(c).Render(ch))
	}
	return b.String()
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

func graphemes(s string) []string {
	var out []string
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		out = append(out, g.Str())
	}
	return out
}
