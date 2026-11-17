package kit

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func visibleWidth(s string) int { return lipgloss.Width(s) }

// Fit forces s into exactly width columns: truncated with an ellipsis or
// right-padded with spaces. ANSI-aware.
func Fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := lipgloss.Width(s)
	switch {
	case w > width:
		return ansi.Truncate(s, width, "…")
	case w < width:
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

// Block forces s into exactly width×height: lines fitted, extra lines
// dropped, missing lines blank.
func Block(s string, width, height int) string {
	if height <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, l := range lines {
		lines[i] = Fit(l, width)
	}
	return strings.Join(lines, "\n")
}

// Wrap word-wraps plain or styled text to width, hard-breaking long words.
func Wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.Wrap(s, width, "")
}

// SideBySide joins blocks horizontally with a gap, aligned to the top.
func SideBySide(gap int, blocks ...string) string {
	sp := strings.Repeat(" ", gap)
	parts := make([]string, 0, len(blocks)*2)
	for i, b := range blocks {
		if i > 0 && gap > 0 {
			parts = append(parts, sp)
		}
		parts = append(parts, b)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// Ago renders a compact relative time: "now", "4m", "3h", "2d", "Mar 4".
func Ago(unix int64, now time.Time) string {
	if unix == 0 {
		return "—"
	}
	d := now.Sub(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return time.Unix(unix, 0).UTC().Format("Jan 2")
}

// Plural formats a count with a noun.
func Plural(n int64, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
