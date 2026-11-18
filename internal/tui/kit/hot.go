package kit

import (
	"image/color"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Hot spots: interactive regions found from the frame itself.
//
// A screen marks text as interactive with Hot(key, text). The mark is an
// OSC 8 hyperlink on a private scheme, which every layout primitive already
// carries along untouched (it has no width, and truncation keeps escape
// codes). When the outer Scene composites the frame it reads the marks off
// the finished cells into a HotMap and erases them, so they never reach the
// terminal. A click or hover is then a lookup in what was actually drawn:
// there is no second copy of the layout math to drift out of step with it.
//
// Hot spots must not nest: the inner mark's end would end the outer one too.

const hotScheme = "bulletin-hot:"

// Hot marks every line of s as belonging to key.
func Hot(key, s string) string {
	if key == "" || s == "" {
		return s
	}
	open, end := ansi.SetHyperlink(hotScheme+key), ansi.ResetHyperlink()
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = open + l + end
		}
	}
	return strings.Join(lines, "\n")
}

// Link makes s a real terminal hyperlink to url, which a terminal that
// supports OSC 8 opens on ctrl- or cmd-click. Only use it after the reader
// has been told where the link goes.
func Link(url, s string) string {
	return ansi.SetHyperlink(url) + s + ansi.ResetHyperlink()
}

// HotSpan is a run of cells on one row belonging to one key.
type HotSpan struct {
	X, Y, W int
	Key     string
}

// HotMap is where the last frame drew each hot spot.
type HotMap struct {
	Spans []HotSpan
}

// At is the key drawn at (x, y), or "".
func (h *HotMap) At(x, y int) string {
	if h == nil {
		return ""
	}
	for _, s := range h.Spans {
		if s.Y == y && x >= s.X && x < s.X+s.W {
			return s.Key
		}
	}
	return ""
}

// Find is the first cell of key, if it was drawn.
func (h *HotMap) Find(key string) (x, y int, ok bool) {
	if h == nil {
		return 0, 0, false
	}
	for _, s := range h.Spans {
		if s.Key == key {
			return s.X, s.Y, true
		}
	}
	return 0, 0, false
}

// Keys lists the keys drawn, in reading order, once each.
func (h *HotMap) Keys() []string {
	if h == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, s := range h.Spans {
		if !seen[s.Key] {
			seen[s.Key] = true
			out = append(out, s.Key)
		}
	}
	return out
}

// collect records the hot cells of cv and erases their marks.
func (h *HotMap) collect(cv *lipgloss.Canvas, w, hgt int) {
	if h != nil {
		h.Spans = h.Spans[:0]
	}
	for y := range hgt {
		for x := 0; x < w; x++ {
			c := cv.CellAt(x, y)
			if c == nil || !strings.HasPrefix(c.Link.URL, hotScheme) {
				continue
			}
			key := c.Link.URL[len(hotScheme):]
			c.Link = uv.Link{}
			if h == nil {
				continue
			}
			cw := max(1, c.Width)
			if n := len(h.Spans); n > 0 && h.Spans[n-1].Key == key && h.Spans[n-1].Y == y && h.Spans[n-1].X+h.Spans[n-1].W == x {
				h.Spans[n-1].W += cw
			} else {
				h.Spans = append(h.Spans, HotSpan{X: x, Y: y, W: cw, Key: key})
			}
		}
	}
}

// ---- the lit look ----------------------------------------------------------

// FlowSpeed is how many times a lit gradient cycles per second.
const FlowSpeed = 0.55

// FlowPhase is a lit gradient's offset after it has been lit for d. It never
// returns exactly 0, which Gradient treats as "not flowing".
func FlowPhase(d time.Duration) float64 {
	p := math.Mod(d.Seconds()*FlowSpeed, 1)
	return 0.001 + p*0.998
}

// LitTint is the background behind a lit hot spot: a breath of its own
// color over the surface, so a keyboard focus reads even where the
// gradient is subtle.
func LitTint(th Theme, stops []color.Color) color.Color {
	return Lerp(th.Surface, At(stops, 0.2), 0.28)
}

// Flow paints s along stops; lit, the gradient runs at phase over a tint and
// is underlined. It is the one look shared by every hovered or focused hot
// spot, so a mouse and a keyboard highlight read the same.
func Flow(s string, base lipgloss.Style, stops []color.Color, lit bool, phase float64, th Theme) string {
	if !lit {
		return Gradient(s, base, stops, 0)
	}
	return Gradient(s, base.Underline(true).Background(LitTint(th, stops)), stops, phase)
}
