package tui

import (
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

// viewSplash is the title card: a drifting pixel wordmark, a tagline that
// types itself out, and a breathing prompt.
func (m Model) viewSplash() string {
	t := m.now().Sub(m.born).Seconds()
	phase := t / 3
	stops := m.th.Brand.Stops

	logo := kit.Wordmark("BULLETIN", stops, phase)

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

	// The block's height does not depend on the orbs' colors, so lay it out
	// first: each orb's rim must fade into the backdrop at the rows it
	// actually occupies, or it wears a ring of darker pixels.
	const orbRows, orbGap = 4, 18
	height := orbRows + 1 + lipgloss.Height(logo) + 1 + 1 + 2 + 1
	top := max(0, (m.h-height)/2-1)
	bg := m.th.Backdrop(top+orbRows/2, m.h)
	orbs := kit.SideBySide(orbGap,
		kit.Orb(4, kit.At(stops, 0), kit.At(stops, 0.3), bg),
		kit.Orb(3, kit.At(stops, 0.6), kit.At(stops, 1), bg),
	)

	content := []string{
		kit.Center(orbs, m.w), "",
		kit.Center(logo, m.w), "",
		kit.Center(line, m.w), "", "",
		kit.Center(prompt, m.w),
	}
	return strings.Repeat("\n", top) + strings.Join(content, "\n")
}
