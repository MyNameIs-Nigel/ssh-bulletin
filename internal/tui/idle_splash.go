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
	catTag          = "fsc"
	idleCreditKey   = "idle:credit"
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
	m.ui.idle = idleSplashFrames{}
	m.hover = ""
	m.trackLit()
	return m.ensureTicking()
}

func (m *Model) clickIdleSplash(msg tea.MouseClickMsg) tea.Cmd {
	if cmd, ok := m.clickHot(msg); ok {
		return cmd
	}
	if msg.Button == tea.MouseLeft && m.idleLink.url == "" {
		return m.wakeIdleSplash()
	}
	return nil
}

func (m *Model) updateIdleSplash(msg tea.Msg) tea.Cmd {
	if m.idleLink.url == "" {
		return m.wakeIdleSplash()
	}
	cmd, close := m.idleLink.update(msg)
	if close {
		m.idleLink = linkModalState{}
	}
	return cmd
}

// Both breathing poses are static at a given size. Background events can
// update the session while the terminal keeps displaying a cached frame.
type idleSplashFrames struct {
	w, h           int
	exhale, inhale tea.View
	link           linkModalState
	hot            kit.HotMap
}

func (m Model) viewIdleSplash() tea.View {
	cache := &m.ui.idle
	if cache.w != m.w || cache.h != m.h || cache.link != m.idleLink {
		clear(m.ui.targets)
		*cache = idleSplashFrames{w: m.w, h: m.h, link: m.idleLink}
		cache.exhale = m.renderIdleSplash(false, &cache.hot)
		cache.inhale = m.renderIdleSplash(true, &cache.hot)
	}
	if m.idleInhale {
		return cache.inhale
	}
	return cache.exhale
}

func (m Model) renderIdleSplash(inhale bool, hot *kit.HotMap) tea.View {
	gray := kit.Hex("#8a8a8a")
	var rows []string
	cat := []string{
		"     _   Zz",
		"  |\\'/-..--.",
		" / _ _   ,  ;",
		"`~=`Y'~_<._./",
		" <`-....__.'  ",
	}
	if inhale {
		cat[0] = "     _   zZ"
		cat[1] = "  |\\'/-..---."
		cat[2] = " / _ _   ,   ;"
	}
	last := len(cat) - 1
	if m.w >= len(cat[last])+len(catTag) && m.h >= len(cat)+1 {
		ink := kit.S(kit.Hex("#c4c4c4"))
		for i, line := range cat {
			cat[i] = ink.Render(line)
		}
		credit := ink.Render(catTag)
		if m.w >= minW && m.h >= minH {
			credit = m.hot(idleCreditKey, func(m *Model) tea.Cmd {
				m.idleLink = newLinkModal(catSource)
				m.hover = ""
				return nil
			}, kit.S(kit.At(m.th.Brand.Stops, 0)).Bold(true).Underline(true).Render(catTag))
		}
		cat[last] += credit
		rows = append(rows, strings.Split(kit.Center(strings.Join(cat, "\n"), m.w), "\n")...)
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
	sc.Hot = hot
	if m.idleLink.url != "" {
		sc.Dim = 1
		sc.Center(m.viewLinkModal(m.idleLink, 0), m.th.Overlay)
	}
	return m.terminalView(sc.Render(), tea.MouseModeCellMotion)
}
