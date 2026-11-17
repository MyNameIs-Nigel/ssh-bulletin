// Package kit is the bulletin's rendering framework: gradients, a half-block
// pixel canvas, panels with flowing gradient borders, menus with animated
// highlights, toasts, modals and a compositing Scene that layers them with a
// painted backdrop. Screens in internal/tui are built only from these parts.
//
// Everything assumes 24-bit color: the server forces the TrueColor profile,
// because nothing but TERM survives the arcade bridge and the palettes here
// cannot survive ANSI-256 quantization.
package kit

import (
	"image/color"
	"math"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/lucasb-eyer/go-colorful"
)

// Hex parses "#rrggbb".
func Hex(s string) color.Color { return lipgloss.Color(s) }

// Lerp blends a→b in CIE-L*a*b*, which keeps mid-tones luminous instead of
// the muddy grey an RGB lerp produces between complementary hues.
func Lerp(a, b color.Color, t float64) color.Color {
	ca, okA := colorful.MakeColor(a)
	cb, okB := colorful.MakeColor(b)
	if !okA || !okB {
		if t < 0.5 {
			return a
		}
		return b
	}
	return ca.BlendLab(cb, clamp01(t)).Clamped()
}

// Dim darkens c toward black by amount (0..1).
func Dim(c color.Color, amount float64) color.Color {
	if c == nil {
		return nil
	}
	return Lerp(c, color.Black, amount)
}

// Lift lightens c toward white by amount (0..1).
func Lift(c color.Color, amount float64) color.Color {
	if c == nil {
		return nil
	}
	return Lerp(c, color.White, amount)
}

// Luma is perceived brightness 0..1, for choosing text over a fill.
func Luma(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 0xffff
}

// Ink picks readable text (near-black or near-white) for a fill color.
func Ink(fill color.Color) color.Color {
	if Luma(fill) > 0.55 {
		return Hex("#0d0b1f")
	}
	return Hex("#fbf8ff")
}

// Sample returns n colors evenly spaced along stops.
func Sample(stops []color.Color, n int) []color.Color {
	if n <= 0 || len(stops) == 0 {
		return nil
	}
	out := make([]color.Color, n)
	for i := range n {
		t := 0.0
		if n > 1 {
			t = float64(i) / float64(n-1)
		}
		out[i] = At(stops, t)
	}
	return out
}

// At samples a gradient at t in [0,1].
func At(stops []color.Color, t float64) color.Color {
	switch len(stops) {
	case 0:
		return nil
	case 1:
		return stops[0]
	}
	t = clamp01(t)
	seg := t * float64(len(stops)-1)
	i := int(math.Floor(seg))
	if i >= len(stops)-1 {
		return stops[len(stops)-1]
	}
	return Lerp(stops[i], stops[i+1], seg-float64(i))
}

// Cyclic samples a gradient that wraps end→start seamlessly, so an offset
// can rotate it forever without a visible seam.
func Cyclic(stops []color.Color, t float64) color.Color {
	if len(stops) < 2 {
		return At(stops, 0)
	}
	t -= math.Floor(t)
	loop := append(append([]color.Color{}, stops...), stops[0])
	return At(loop, t)
}

// MixStops blends two gradients position-by-position. Used to cross-fade the
// app's accent when moving between boards.
func MixStops(a, b []color.Color, t float64) []color.Color {
	const n = 4
	sa, sb := Sample(a, n), Sample(b, n)
	out := make([]color.Color, n)
	for i := range n {
		out[i] = Lerp(sa[i], sb[i], t)
	}
	return out
}

func clamp01(t float64) float64 { return math.Max(0, math.Min(1, t)) }

// Palette is a named gradient a user or board can wear.
type Palette struct {
	Name  string
	Label string
	Stops []color.Color
}

// Mid is the palette's representative color.
func (p Palette) Mid() color.Color { return At(p.Stops, 0.5) }

// Start and End are the palette's ends.
func (p Palette) Start() color.Color { return At(p.Stops, 0) }
func (p Palette) End() color.Color   { return At(p.Stops, 1) }

func pal(name, label string, hexes ...string) Palette {
	stops := make([]color.Color, len(hexes))
	for i, h := range hexes {
		stops[i] = Hex(h)
	}
	return Palette{Name: name, Label: label, Stops: stops}
}

// Palettes is every gradient on offer, in picker order.
var Palettes = []Palette{
	pal("aurora", "Aurora", "#3ef2c5", "#31a8ff", "#8b5cf6", "#f472b6"),
	pal("sunset", "Sunset", "#ffb347", "#ff6a88", "#c850c0", "#6a5bff"),
	pal("reef", "Reef", "#00f2fe", "#4facfe", "#43e97b"),
	pal("orchid", "Orchid", "#f9a8d4", "#e879f9", "#a78bfa", "#818cf8"),
	pal("ember", "Ember", "#ffe259", "#ffa751", "#ff5f6d", "#c33764"),
	pal("mint", "Mint", "#d4fc79", "#96e6a1", "#3cd3ad", "#2bc0e4"),
	pal("glacier", "Glacier", "#e0f2fe", "#7dd3fc", "#38bdf8", "#6366f1"),
	pal("neon", "Neon", "#ff00cc", "#9d4edd", "#00f5ff"),
	pal("citrus", "Citrus", "#fdfc47", "#a8ff78", "#24fe41"),
	pal("rose", "Rosé", "#ffdee9", "#ff8fab", "#fb6f92", "#b5179e"),
	pal("lagoon", "Lagoon", "#43cea2", "#185a9d", "#8e2de2"),
	pal("solar", "Solar", "#fff200", "#ff7b00", "#ff0058", "#7a00ff"),
}

var paletteIndex = sync.OnceValue(func() map[string]Palette {
	m := make(map[string]Palette, len(Palettes))
	for _, p := range Palettes {
		m[p.Name] = p
	}
	return m
})

// PaletteNamed looks up a palette, falling back to Aurora.
func PaletteNamed(name string) Palette {
	if p, ok := paletteIndex()[name]; ok {
		return p
	}
	return Palettes[0]
}

// PaletteIndex returns the picker position of a palette name (0 if unknown).
func PaletteIndex(name string) int {
	for i, p := range Palettes {
		if p.Name == name {
			return i
		}
	}
	return 0
}
