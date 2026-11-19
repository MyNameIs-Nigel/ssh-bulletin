package tui

import (
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

const (
	idleSplashAfter = 5 * time.Minute
	idleBreathHold  = 2500 * time.Millisecond
	catSource       = "https://www.asciiart.eu/art/8d0a25efc30a5dd7"
)

type idleBreathMsg struct{ gen int }

func (m Model) idleBreathTick() tea.Cmd {
	gen := m.gen
	return tea.Tick(idleBreathHold, func(time.Time) tea.Msg { return idleBreathMsg{gen: gen} })
}

// The wake input belongs to the splash. It must never reach a draft or run a
// shortcut on the screen underneath it.
func (m *Model) wakeIdleSplash() tea.Cmd {
	m.idleSplash = false
	m.hover = ""
	m.trackLit()
	return m.ensureTicking()
}

// Both breathing poses are static at a given size. Background events can
// update the session while the terminal keeps displaying a cached frame.
type idleSplashFrames struct {
	w, h           int
	exhale, inhale tea.View
}

func (m Model) viewIdleSplash() tea.View {
	cache := &m.ui.idle
	if cache.w != m.w || cache.h != m.h {
		*cache = idleSplashFrames{
			w: m.w, h: m.h,
			exhale: m.renderIdleSplash(false),
			inhale: m.renderIdleSplash(true),
		}
	}
	if m.idleInhale {
		return cache.inhale
	}
	return cache.exhale
}

func (m Model) renderIdleSplash(inhale bool) tea.View {
	gray := kit.Hex("#8a8a8a")
	var rows []string
	cat := []string{
		"     _",
		"  |\\'/-..--.",
		" / _ _   ,  ;",
		"`~=`Y'~_<._./",
		" <`-....__.'  fsc",
	}
	if inhale {
		cat[1] = "  |\\'/-..---."
		cat[2] = " / _ _   ,   ;"
	}
	if m.w >= lipgloss.Width(cat[4]) && m.h >= len(cat)+2 {
		art := kit.S(kit.Hex("#c4c4c4")).Render(strings.Join(cat, "\n"))
		credit := kit.Link(catSource, kit.S(gray).Underline(true).Render("asciiart.eu"))
		rows = append(rows, strings.Split(kit.Center(art, m.w), "\n")...)
		rows = append(rows, kit.Center(credit, m.w))
	}
	if logo := kit.Wordmark("BULLETIN", []color.Color{gray}, 0); m.w >= lipgloss.Width(logo) && m.h >= 14 {
		rows = append(rows, "")
		rows = append(rows, strings.Split(kit.Center(logo, m.w), "\n")...)
	} else if m.w >= len("BULLETIN") && m.h >= len(rows)+4 {
		if len(rows) > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, kit.Center(kit.S(gray).Bold(true).Render("BULLETIN"), m.w))
	}
	if len(rows) > 0 && m.h >= len(rows)+2 {
		rows = append(rows, "")
	}
	prompt := "press any key or click to resume"
	if m.w < len(prompt) {
		prompt = "press any key"
	}
	rows = append(rows, kit.Center(kit.S(gray).Render(kit.Fit(prompt, min(m.w, len(prompt)))), m.w))
	body := strings.Join(rows, "\n")
	sc := kit.NewScene(m.w, m.h, m.th)
	sc.Base = strings.Repeat("\n", max(0, (m.h-lipgloss.Height(body))/2)) + body
	return m.terminalView(sc.Render(), tea.MouseModeCellMotion)
}
