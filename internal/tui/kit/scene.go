package kit

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// Scene composites a frame. It owns the three things plain string-joining
// cannot do:
//
//   - a painted backdrop: every cell nobody colored gets the theme's
//     gradient, so the app has its own sky regardless of terminal theme;
//   - true overlays: modals, toasts and palettes are drawn over the base
//     frame at absolute positions, not spliced into lines;
//   - dimming: everything under a modal is darkened cell by cell, keeping
//     its colors but pushing it back.
type Scene struct {
	W, H  int
	Theme Theme
	Base  string
	Dim   float64 // 0..1, darkens Base before overlays are drawn
	// BackdropFn overrides the theme backdrop (x, y → color).
	BackdropFn func(x, y int) color.Color
	// OverrideBg paints the backdrop even where Base set a background.
	OverrideBg bool
	// Hot receives where each hot spot landed (see Hot). Hot marks are
	// always erased from the output, unless Nested is set.
	Hot *HotMap
	// Nested passes hot marks through untouched, for a scene rendered into
	// a piece of a larger frame (the outer scene collects them).
	Nested bool
	// Modal drops Base's hot spots: a modal is open over it, so only the
	// overlays are interactive. A dimmed base is always treated as modal.
	Modal bool

	overlays []overlay
}

type overlay struct {
	content string
	x, y    int
	fill    color.Color
}

// NewScene starts a frame of w×h.
func NewScene(w, h int, th Theme) *Scene { return &Scene{W: w, H: h, Theme: th} }

// Overlay draws content at (x, y) above the base. fill, when set, is the
// background for any of its cells that did not choose one (an opaque card).
func (s *Scene) Overlay(content string, x, y int, fill color.Color) {
	s.overlays = append(s.overlays, overlay{content: content, x: x, y: y, fill: fill})
}

// Center overlays content centered on the frame.
func (s *Scene) Center(content string, fill color.Color) {
	w, h := lipgloss.Width(content), lipgloss.Height(content)
	s.Overlay(content, max(0, (s.W-w)/2), max(0, (s.H-h)/2), fill)
}

func (s *Scene) backdrop(x, y int) color.Color {
	if s.BackdropFn != nil {
		return s.BackdropFn(x, y)
	}
	return s.Theme.Backdrop(y, s.H)
}

// Render flattens the scene into a string exactly W×H cells.
func (s *Scene) Render() string {
	if s.W <= 0 || s.H <= 0 {
		return ""
	}
	cv := lipgloss.NewCanvas(s.W, s.H)
	cv.Compose(lipgloss.NewLayer(s.Base))
	rowBg := make([]color.Color, s.H)
	for y := range s.H {
		rowBg[y] = s.backdrop(0, y)
	}
	for y := range s.H {
		for x := range s.W {
			c := cv.CellAt(x, y)
			if c == nil || c.IsZero() {
				continue
			}
			// Nothing falls back to the terminal's own colors: a cell with
			// no foreground gets the theme's text color.
			if c.Style.Fg == nil && s.Theme.Text != nil {
				c.Style.Fg = s.Theme.Text
			}
			if c.Style.Bg == nil || s.OverrideBg {
				if s.BackdropFn != nil {
					c.Style.Bg = s.backdrop(x, y)
				} else {
					c.Style.Bg = rowBg[y]
				}
			}
			if (s.Modal || s.Dim > 0) && strings.HasPrefix(c.Link.URL, hotScheme) {
				c.Link = uv.Link{}
			}
			if s.Dim > 0 {
				fg := c.Style.Fg
				if fg == nil {
					fg = s.Theme.Text
				}
				c.Style.Fg = Lerp(fg, c.Style.Bg, s.Dim*0.75)
				c.Style.Bg = Dim(c.Style.Bg, s.Dim*0.45)
				c.Style.Attrs &^= uv.AttrBold
			}
		}
	}
	for _, o := range s.overlays {
		// Draw the styled string straight into its own rectangle. (A
		// lipgloss Layer's Draw ignores its X/Y and clears the whole target
		// area, which would erase the base frame under every overlay.)
		w, h := lipgloss.Width(o.content), lipgloss.Height(o.content)
		uv.NewStyledString(o.content).Draw(cv, uv.Rect(o.x, o.y, w, h))
		for y := o.y; y < min(s.H, o.y+h); y++ {
			for x := o.x; x < min(s.W, o.x+w); x++ {
				c := cv.CellAt(x, y)
				if c == nil || c.IsZero() {
					continue
				}
				if c.Style.Fg == nil && s.Theme.Text != nil {
					c.Style.Fg = s.Theme.Text
				}
				if c.Style.Bg != nil {
					continue
				}
				if o.fill != nil {
					c.Style.Bg = o.fill
				} else {
					c.Style.Bg = rowBg[y]
				}
			}
		}
	}
	if !s.Nested {
		s.Hot.collect(cv, s.W, s.H)
	}
	return cv.Render()
}
