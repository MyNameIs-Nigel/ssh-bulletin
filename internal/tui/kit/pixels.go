package kit

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Pixels is a canvas at double vertical resolution: each terminal cell holds
// two square-ish pixels via the upper-half block (fg = top pixel, bg = bottom
// pixel). A nil pixel is transparent — the backdrop shows through — so logos
// and sigils sit on the scene instead of on a box.
type Pixels struct {
	W, H int // in pixels; H is rounded up to even on render
	px   []color.Color
}

// NewPixels allocates a transparent canvas.
func NewPixels(w, h int) *Pixels {
	if h%2 == 1 {
		h++
	}
	return &Pixels{W: w, H: h, px: make([]color.Color, w*h)}
}

// Set paints one pixel; out-of-range writes are ignored.
func (p *Pixels) Set(x, y int, c color.Color) {
	if x < 0 || y < 0 || x >= p.W || y >= p.H {
		return
	}
	p.px[y*p.W+x] = c
}

// At reads one pixel.
func (p *Pixels) At(x, y int) color.Color {
	if x < 0 || y < 0 || x >= p.W || y >= p.H {
		return nil
	}
	return p.px[y*p.W+x]
}

// Render converts the canvas to cells.
func (p *Pixels) Render() string {
	rows := make([]string, 0, p.H/2)
	for y := 0; y < p.H; y += 2 {
		var b strings.Builder
		for x := range p.W {
			top, bot := p.At(x, y), p.At(x, y+1)
			switch {
			case top == nil && bot == nil:
				b.WriteByte(' ')
			case bot == nil:
				b.WriteString(lipgloss.NewStyle().Foreground(top).Render("▀"))
			case top == nil:
				b.WriteString(lipgloss.NewStyle().Foreground(bot).Render("▄"))
			case sameColor(top, bot):
				b.WriteString(lipgloss.NewStyle().Foreground(top).Render("█"))
			default:
				b.WriteString(lipgloss.NewStyle().Foreground(top).Background(bot).Render("▀"))
			}
		}
		rows = append(rows, b.String())
	}
	return strings.Join(rows, "\n")
}

func sameColor(a, b color.Color) bool {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return ar == br && ag == bg && ab == bb
}
