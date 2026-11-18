package kit

import (
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Item is one menu entry. Each item may carry its own gradient, so a list of
// boards reads as a list of distinct colored places.
type Item struct {
	Title    string
	Desc     string
	Meta     string      // right-aligned on the title row (counts, times)
	Badge    string      // a small chip after the title ("NEW", "PINNED")
	BadgeInk color.Color // optional chip color
	Stops    []color.Color
}

// Menu is a vertically scrolling list with a spring-animated selection
// glow: the highlight slides between rows rather than jumping.
type Menu struct {
	Items  []Item
	Cursor int
	Top    int     // first visible item
	glow   float64 // animated highlight position, in items
	vel    float64
	Rows   int // rows per item: 2 (title+desc) or 1
}

// NewMenu builds a two-row menu.
func NewMenu(items []Item) Menu { return Menu{Items: items, Rows: 2} }

// SetItems replaces the items, keeping the cursor in range.
func (m *Menu) SetItems(items []Item) {
	m.Items = items
	m.Cursor = max(0, min(m.Cursor, len(items)-1))
}

// Move shifts the cursor by d (clamped). Returns true if it moved.
func (m *Menu) Move(d int) bool {
	if len(m.Items) == 0 {
		return false
	}
	next := max(0, min(len(m.Items)-1, m.Cursor+d))
	if next == m.Cursor {
		return false
	}
	m.Cursor = next
	return true
}

// Select puts the cursor on i directly.
func (m *Menu) Select(i int) {
	if i >= 0 && i < len(m.Items) {
		m.Cursor = i
	}
}

// Selected returns the current item index, or -1.
func (m Menu) Selected() int {
	if len(m.Items) == 0 {
		return -1
	}
	return m.Cursor
}

// Animating reports whether the glow is still travelling.
func (m Menu) Animating() bool {
	return math.Abs(m.glow-float64(m.Cursor)) > 0.01 || math.Abs(m.vel) > 0.01
}

// Step advances the spring one frame (dt seconds). A slightly
// under-damped spring: quick, with a whisper of overshoot.
func (m *Menu) Step(dt float64) {
	const stiffness, damping = 260.0, 24.0
	target := float64(m.Cursor)
	acc := stiffness*(target-m.glow) - damping*m.vel
	m.vel += acc * dt
	m.glow += m.vel * dt
	if !m.Animating() {
		m.glow, m.vel = target, 0
	}
}

// Snap ends any animation immediately.
func (m *Menu) Snap() { m.glow, m.vel = float64(m.Cursor), 0 }

func (m Menu) rows() int {
	if m.Rows <= 0 {
		return 2
	}
	return m.Rows
}

// Visible returns how many items fit in height (each item + 1 spacer row).
func (m Menu) Visible(height int) int {
	per := m.rows() + 1
	return max(1, (height+1)/per)
}

// ItemAtRow maps a row inside the rendered menu back to an item index.
func (m Menu) ItemAtRow(row int) int {
	per := m.rows() + 1
	if row < 0 || row%per == m.rows() {
		return -1
	}
	i := m.Top + row/per
	if i >= len(m.Items) {
		return -1
	}
	return i
}

// Scroll moves the visible window just enough to contain the cursor.
func (m *Menu) Scroll(height int) {
	vis := m.Visible(height)
	if m.Cursor < m.Top {
		m.Top = m.Cursor
	}
	if m.Cursor >= m.Top+vis {
		m.Top = m.Cursor - vis + 1
	}
	m.Top = max(0, min(m.Top, max(0, len(m.Items)-vis)))
}

// Render draws the menu in width×height. focused selects whether the
// highlight glows in the item's colors or rests in a neutral surface.
func (m *Menu) Render(width, height int, th Theme, focused bool) string {
	if len(m.Items) == 0 || width < 8 {
		return ""
	}
	m.Scroll(height)
	vis := m.Visible(height)

	var out []string
	for i := m.Top; i < min(len(m.Items), m.Top+vis); i++ {
		it := m.Items[i]
		stops := it.Stops
		if len(stops) == 0 {
			stops = th.Brand.Stops
		}
		// The glow's distance from this row: 0 = fully lit.
		dist := math.Abs(m.glow - float64(i))
		lit := math.Max(0, 1-dist)
		selected := i == m.Cursor

		// Left rail: a vertical gradient bar that brightens with the glow.
		rail := func(row int) string {
			if lit <= 0.02 {
				return " "
			}
			c := At(stops, float64(row)/float64(max(1, m.rows()-1)))
			if !focused {
				c = th.Muted
			}
			return S(Lerp(th.Faint, c, lit)).Render("▌")
		}

		titleW := width - 3
		meta := ""
		if it.Meta != "" {
			meta = S(th.Muted).Render(it.Meta)
		}
		badge := ""
		if it.Badge != "" {
			ink := it.BadgeInk
			if ink == nil {
				ink = At(stops, 0.8)
			}
			badge = " " + Chip(it.Badge, ink, true)
		}
		var title string
		switch {
		case selected && focused:
			title = Gradient(it.Title, lipgloss.NewStyle().Bold(true), stops, 0)
		case selected:
			title = S(th.Text).Bold(true).Render(it.Title)
		default:
			title = S(Lerp(th.Subtle, th.Text, lit)).Render(it.Title)
		}
		avail := titleW - lipgloss.Width(meta) - lipgloss.Width(badge) - 1
		// Truncate the title alone: a long title must not cut the badge off.
		title = Fit(ansi.Truncate(title, max(1, avail), "…")+badge, max(1, avail+lipgloss.Width(badge)))
		row0 := rail(0) + " " + title + " " + meta
		row0 = Fit(row0, width)
		if selected {
			row0 = glowRow(row0, width, th, stops, lit, focused)
		}
		out = append(out, row0)

		if m.rows() > 1 {
			desc := S(th.Muted).Render(it.Desc)
			if selected {
				desc = S(th.Subtle).Render(it.Desc)
			}
			row1 := Fit(rail(1)+" "+Fit(desc, titleW+1), width)
			if selected {
				row1 = glowRow(row1, width, th, stops, lit, focused)
			}
			out = append(out, row1)
		}
		out = append(out, "")
	}
	return Block(strings.Join(out, "\n"), width, height)
}

// glowRow lays a horizontal background fade under a selected row: tinted at
// the left, dissolving to the surface on the right.
func glowRow(row string, width int, th Theme, stops []color.Color, lit float64, focused bool) string {
	tint := Lerp(th.Surface, At(stops, 0), 0.22*lit)
	if !focused {
		tint = th.Surface
	}
	return paintBg(row, width, []color.Color{tint, th.Surface, Lerp(th.Surface, th.BgTop, 0.6)})
}

// paintBg re-renders a styled row with a horizontal background gradient by
// compositing through a one-row Scene, which preserves each cell's foreground.
func paintBg(row string, width int, stops []color.Color) string {
	sc := NewScene(width, 1, Theme{Text: Hex("#ffffff")})
	sc.Base = row
	sc.BackdropFn = func(x, _ int) color.Color {
		return At(stops, float64(x)/float64(max(1, width-1)))
	}
	return sc.Render()
}
