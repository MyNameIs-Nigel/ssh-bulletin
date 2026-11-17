package kit

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme is the token set every component reads. Components never hard-code
// a color; they ask the theme or a palette.
type Theme struct {
	BgTop     color.Color // backdrop gradient, top row
	BgBottom  color.Color // backdrop gradient, bottom row
	Surface   color.Color // raised areas (selected rows, inputs)
	SurfaceHi color.Color // hovered / focused raised areas
	Overlay   color.Color // modal and toast fill

	Text   color.Color
	Subtle color.Color
	Muted  color.Color
	Faint  color.Color
	Line   color.Color // resting borders and rules

	Good color.Color
	Warn color.Color
	Bad  color.Color

	Brand Palette // the app's own gradient (masthead, splash)
}

// Midnight is the default (and, for now, only) theme: an ink-blue night sky
// that lets every palette glow.
var Midnight = Theme{
	BgTop:     Hex("#0a0918"),
	BgBottom:  Hex("#15102c"),
	Surface:   Hex("#1c1838"),
	SurfaceHi: Hex("#272150"),
	Overlay:   Hex("#120f26"),
	Text:      Hex("#ece9ff"),
	Subtle:    Hex("#b9b3df"),
	Muted:     Hex("#7f78ad"),
	Faint:     Hex("#4a4475"),
	Line:      Hex("#332d5c"),
	Good:      Hex("#5cf2a4"),
	Warn:      Hex("#ffcc66"),
	Bad:       Hex("#ff6b8b"),
	Brand:     pal("brand", "Bulletin", "#ff7ad9", "#a78bfa", "#5eead4", "#fde68a"),
}

// Backdrop returns the backdrop color for row y of h. The gradient is
// vertical only, so every cell in a row shares a background and the
// renderer emits one SGR per style run rather than one per cell.
func (t Theme) Backdrop(y, h int) color.Color {
	if h <= 1 {
		return t.BgTop
	}
	return Lerp(t.BgTop, t.BgBottom, float64(y)/float64(h-1))
}

// S is a convenience style constructor bound to a foreground.
func S(fg color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(fg) }
