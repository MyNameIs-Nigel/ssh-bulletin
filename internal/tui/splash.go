package tui

import (
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

// viewSplash is the title card: a drifting pixel wordmark with a drop
// shadow, a tagline that types itself out, and a breathing prompt.
func (m Model) viewSplash() string {
	t := m.now().Sub(m.born).Seconds()
	phase := t / 3
	stops := m.th.Brand.Stops

	logo := kit.Wordmark("BULLETIN", stops, kit.Dim(kit.At(stops, 0.4), 0.78), phase)

	tagline := "a message board for people with keys"
	typed := int(math.Min(float64(len(tagline)), math.Max(0, (t-0.35)*34)))
	line := kit.Gradient(tagline[:typed], lipgloss.NewStyle().Italic(true), kit.Sample(stops, 3), 0)
	if typed < len(tagline) && m.frame%8 < 5 {
		line += kit.S(kit.At(stops, 0.9)).Render("▌")
	}

	var prompt string
	if m.acct != nil {
		prompt = kit.S(m.th.Muted).Render("welcome back, ") + kit.GradientBold(m.acct.Handle, kit.PaletteNamed(m.acct.Palette).Stops)
	} else {
		glow := kit.Lerp(m.th.Faint, m.th.Text, 0.4+0.6*kit.Pulse(m.now().Sub(m.born), 1600*time.Millisecond))
		prompt = kit.S(glow).Render("press any key to begin")
	}

	orbs := kit.SideBySide(18,
		kit.Orb(4, kit.At(stops, 0), kit.At(stops, 0.3), m.th.BgTop),
		kit.Orb(3, kit.At(stops, 0.6), kit.At(stops, 1), m.th.BgTop),
	)

	content := []string{
		kit.Center(orbs, m.w), "",
		kit.Center(logo, m.w), "",
		kit.Center(line, m.w), "", "",
		kit.Center(prompt, m.w),
	}
	block := strings.Join(content, "\n")
	top := max(0, (m.h-lipgloss.Height(block))/2-1)
	return strings.Repeat("\n", top) + block
}
