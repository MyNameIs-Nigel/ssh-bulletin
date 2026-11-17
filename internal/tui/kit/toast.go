package kit

import (
	"image/color"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// Toast is a transient card that slides in from the right edge, lingers,
// and slides back out.
type Toast struct {
	Title string
	Body  string
	Stops []color.Color
	born  time.Time
	ttl   time.Duration
	slide float64 // 0 = fully off-screen, 1 = fully in
}

// Toasts is a stack of toasts, newest on top.
type Toasts struct {
	items []Toast
}

const toastWidth = 34

// Push adds a toast.
func (t *Toasts) Push(now time.Time, title, body string, stops []color.Color) {
	t.items = append([]Toast{{Title: title, Body: body, Stops: stops, born: now, ttl: 4 * time.Second}}, t.items...)
	if len(t.items) > 3 {
		t.items = t.items[:3]
	}
}

// Len is the number of live toasts.
func (t Toasts) Len() int { return len(t.items) }

// Animating is true while any toast exists (each has a lifetime to run).
func (t Toasts) Animating() bool { return len(t.items) > 0 }

// Step eases toasts in or out and drops the expired.
func (t *Toasts) Step(now time.Time, dt float64) {
	kept := t.items[:0]
	for _, it := range t.items {
		leaving := now.Sub(it.born) > it.ttl
		target := 1.0
		if leaving {
			target = 0
		}
		// Exponential ease: frame-rate independent.
		it.slide += (target - it.slide) * (1 - math.Exp(-dt*14))
		if leaving && it.slide < 0.02 {
			continue
		}
		kept = append(kept, it)
	}
	t.items = kept
}

// Draw overlays the stack in the top-right corner of a scene.
func (t Toasts) Draw(sc *Scene, th Theme, top int) {
	y := top
	for _, it := range t.items {
		card := renderToast(it, th)
		w := lipgloss.Width(card)
		x := sc.W - int(math.Round(float64(w+1)*easeOutBack(it.slide)))
		x = max(x, sc.W-w-1)
		if x < sc.W {
			sc.Overlay(clipLeft(card, sc.W-x), x, y, th.Overlay)
		}
		y += lipgloss.Height(card)
	}
}

func renderToast(it Toast, th Theme) string {
	body := GradientBold(it.Title, it.Stops)
	if it.Body != "" {
		body += "\n" + S(th.Subtle).Render(Fit(it.Body, toastWidth-4))
	}
	p := Panel{Width: toastWidth, Height: lipgloss.Height(body) + 2, Stops: it.Stops, Focused: true, Fill: th.Overlay, Theme: th}
	return p.Render(body)
}

// clipLeft keeps only the leftmost w cells of each line (the part on screen).
func clipLeft(s string, w int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if lipgloss.Width(l) > w {
			lines[i] = Fit(l, w)
		}
	}
	return strings.Join(lines, "\n")
}

func easeOutBack(t float64) float64 {
	const c1 = 1.70158
	const c3 = c1 + 1
	t = clamp01(t)
	return 1 + c3*math.Pow(t-1, 3) + c1*math.Pow(t-1, 2)
}
