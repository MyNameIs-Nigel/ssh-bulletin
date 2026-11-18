package tui

import (
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

// viewSplash is the title card: a bridge over moving water with birds
// crossing, a drifting pixel wordmark, a tagline that types itself out, and a
// breathing prompt. It waits for a key: nobody is carried past it on a timer.
func (m Model) viewSplash() string {
	t := m.now().Sub(m.born).Seconds()
	phase := t / 3
	stops := m.th.Brand.Stops

	logo := kit.Wordmark("BULLETIN", stops, phase)

	tagline := "a message board for devs, engineers, terminal dwellers, and everyone else"
	typed := int(math.Min(float64(len(tagline)), math.Max(0, (t-0.35)*34)))
	line := kit.Gradient(tagline[:typed], lipgloss.NewStyle().Italic(true), kit.Sample(stops, 3), 0)
	if typed < len(tagline) && m.frame%8 < 5 {
		line += kit.S(kit.At(stops, 0.9)).Render("▌")
	}

	key := kit.S(kit.Lerp(m.th.Faint, m.th.Text, 0.4+0.6*kit.Pulse(m.now().Sub(m.born), 1600*time.Millisecond))).Render("press any key to begin")
	prompt := []string{kit.Center(key, m.w)}
	if m.acct != nil {
		welcome := kit.S(m.th.Muted).Render("welcome back, ") + kit.GradientBold(m.acct.Handle, kit.PaletteNamed(m.acct.Palette).Stops)
		prompt = []string{kit.Center(welcome, m.w), kit.Center(key, m.w)}
	}
	promptRows := len(prompt)

	// Fit the bridge to the terminal. Short screens first lose the rows the
	// scene can spare, then breathing room, then the bridge itself; the
	// wordmark and the prompt always stay.
	var scene string
	gap := 2
	if art := theBridge(); m.w >= art.w {
		for _, g := range []int{2, 1} {
			chrome := 1 + lipgloss.Height(logo) + 1 + 1 + g + promptRows
			if rows := art.fit(m.h - chrome); rows != nil {
				scene, gap = art.render(rows, m.w, t, m.th), g
				break
			}
		}
	}

	var content []string
	if scene != "" {
		content = append(content, scene, "")
	}
	content = append(content,
		kit.Center(logo, m.w), "",
		kit.Center(line, m.w),
	)
	for range gap {
		content = append(content, "")
	}
	content = append(content, prompt...)

	body := strings.Join(content, "\n")
	top := max(0, (m.h-lipgloss.Height(body))/2-1)
	return strings.Repeat("\n", top) + body
}
