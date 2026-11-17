package kit

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Panel is a rounded box whose border is painted cell by cell along a
// gradient that flows clockwise around the perimeter. A focused panel's
// gradient can rotate (Phase) — the "living border" — while resting panels
// draw a quiet single-color line.
type Panel struct {
	Title   string        // shown inset in the top edge
	Note    string        // shown inset in the bottom-right edge
	Width   int           // outer width, border included
	Height  int           // outer height, border included
	Stops   []color.Color // gradient for a focused border
	Focused bool
	Phase   float64     // rotation of the border gradient, 0..1
	Fill    color.Color // optional opaque fill (modals); nil = transparent
	Theme   Theme
	PadX    int // horizontal padding inside the border (default 1)
}

// Inner returns the content area size.
func (p Panel) Inner() (w, h int) {
	px := p.padX()
	return max(0, p.Width-2-2*px), max(0, p.Height-2)
}

func (p Panel) padX() int {
	if p.PadX < 0 {
		return 0
	}
	if p.PadX == 0 {
		return 1
	}
	return p.PadX
}

// Render draws the panel around body (fitted to the inner area).
func (p Panel) Render(body string) string {
	w, h := p.Width, p.Height
	if w < 4 || h < 2 {
		return ""
	}
	iw, ih := p.Inner()
	perim := 2*(w-1) + 2*(h-1)
	colorAt := func(i int) color.Color {
		if !p.Focused || len(p.Stops) == 0 {
			return p.Theme.Line
		}
		return Cyclic(p.Stops, float64(i)/float64(perim)+p.Phase)
	}
	cell := func(ch string, c color.Color) string {
		st := lipgloss.NewStyle().Foreground(c)
		if p.Fill != nil {
			st = st.Background(p.Fill)
		}
		return st.Render(ch)
	}

	// Top edge: index 0..w-1 left→right, with the title inset after "╭─".
	var top strings.Builder
	title := ""
	if p.Title != "" {
		title = " " + p.Title + " "
		if lipgloss.Width(title) > w-6 {
			title = Fit(title, w-6)
		}
	}
	titleW := lipgloss.Width(title)
	for x := 0; x < w; x++ {
		switch {
		case x == 0:
			top.WriteString(cell("╭", colorAt(x)))
		case x == w-1:
			top.WriteString(cell("╮", colorAt(x)))
		case title != "" && x == 2:
			top.WriteString(p.titleStyle(title))
			x += titleW - 1
		default:
			top.WriteString(cell("─", colorAt(x)))
		}
	}

	lines := strings.Split(Block(body, iw, ih), "\n")
	pad := strings.Repeat(" ", p.padX())
	if p.Fill != nil {
		pad = lipgloss.NewStyle().Background(p.Fill).Render(pad)
	}
	rows := make([]string, 0, h)
	rows = append(rows, top.String())
	for y := 0; y < ih; y++ {
		right := colorAt(w - 1 + 1 + y)
		left := colorAt(perim - 1 - y)
		rows = append(rows, cell("│", left)+pad+lines[y]+pad+cell("│", right))
	}

	// Bottom edge: indices run right→left.
	var bot strings.Builder
	note := ""
	if p.Note != "" {
		note = " " + p.Note + " "
	}
	noteW := lipgloss.Width(note)
	base := (w - 1) + (h - 1)
	for x := 0; x < w; x++ {
		idx := base + (w - 1 - x)
		switch {
		case x == 0:
			bot.WriteString(cell("╰", colorAt(idx)))
		case x == w-1:
			bot.WriteString(cell("╯", colorAt(idx)))
		case note != "" && x == w-2-noteW && x > 1:
			st := lipgloss.NewStyle().Foreground(p.Theme.Muted)
			if p.Fill != nil {
				st = st.Background(p.Fill)
			}
			bot.WriteString(st.Render(note))
			x += noteW - 1
		default:
			bot.WriteString(cell("─", colorAt(idx)))
		}
	}
	rows = append(rows, bot.String())
	return strings.Join(rows, "\n")
}

func (p Panel) titleStyle(title string) string {
	base := lipgloss.NewStyle().Bold(true)
	if p.Fill != nil {
		base = base.Background(p.Fill)
	}
	if p.Focused && len(p.Stops) > 0 {
		return Gradient(title, base, p.Stops, 0)
	}
	return base.Foreground(p.Theme.Subtle).Render(title)
}
