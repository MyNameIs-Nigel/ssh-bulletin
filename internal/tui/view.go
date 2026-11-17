package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
	"github.com/mynameis-nigel/ssh-bulletin/internal/version"
)

// Minimum usable terminal. Below this, a friendly resize card replaces the UI.
const (
	minW = 64
	minH = 20
)

// rect is a screen region.
type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// body is the area between the masthead (2 rows) and the footer (1 row).
func (m Model) body() rect { return rect{0, 2, m.w, max(0, m.h-3)} }

// View renders the frame.
func (m Model) View() tea.View {
	var content string
	if m.w < minW || m.h < minH {
		content = m.viewTooSmall()
	} else {
		content = m.compose3()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.BackgroundColor = m.th.BgTop
	v.WindowTitle = "bulletin · ssharcade"
	return v
}

// compose3 builds the base frame for the current screen and layers
// overlays and toasts over it.
func (m Model) compose3() string {
	sc := kit.NewScene(m.w, m.h, m.th)
	switch m.screen {
	case scrSplash:
		sc.Base = m.viewSplash()
	case scrOnboard:
		sc.Base = m.viewIdentity(false)
	default:
		var bodyStr string
		switch m.screen {
		case scrHome:
			bodyStr = m.viewHome()
		case scrBoard:
			bodyStr = m.viewBoard()
		case scrThread:
			bodyStr = m.viewThread()
		case scrProfile:
			bodyStr = m.viewIdentity(true)
		}
		sc.Base = strings.Join([]string{m.masthead(), kit.Block(bodyStr, m.w, m.body().h), m.footer()}, "\n")
	}

	if m.overlayT > 0 {
		sc.Dim = smooth(m.overlayT)
		m.drawOverlay(sc)
	}
	m.toasts.Draw(sc, m.th, 2)
	return sc.Render()
}

// masthead is the top two rows: wordmark, breadcrumbs, presence, and a
// glowing rule in the current accent.
func (m Model) masthead() string {
	acc := m.accent()
	brand := kit.Gradient("◆ BULLETIN", lipgloss.NewStyle().Bold(true), m.th.Brand.Stops, 0)
	brand = " " + brand + kit.S(m.th.Faint).Render("  ssharcade")

	crumbs := []string{"boards"}
	switch m.screen {
	case scrBoard:
		crumbs = append(crumbs, m.curBoard.Title)
	case scrThread:
		crumbs = append(crumbs, m.curBoard.Title, clip(m.curThread.Title, 32))
	case scrProfile:
		crumbs = []string{"you"}
	}
	mid := kit.Crumbs(crumbs, m.th, acc)

	online := m.d.Service.Broker().Subscribers()
	dot := kit.S(kit.Lerp(m.th.Good, m.th.BgTop, 0.35*kit.Pulse(time.Duration(m.frame)*time.Second/kit.FrameRate, 2*time.Second))).Render("●")
	right := dot + kit.S(m.th.Subtle).Render(fmt.Sprintf(" %d online ", online))
	if m.acct != nil {
		// The handle wears its palette as ink rather than as a filled tag: a
		// filled cell can only have square corners in a terminal.
		right += "  " + kit.GradientBold(m.acct.Handle, kit.PaletteNamed(m.acct.Palette).Stops) + " "
	}

	lw, mw, rw := lipgloss.Width(brand), lipgloss.Width(mid), lipgloss.Width(right)
	if m.w-lw-mw-rw < 4 && len(crumbs) > 1 {
		mid = kit.Crumbs([]string{"…", crumbs[len(crumbs)-1]}, m.th, acc)
		mw = lipgloss.Width(mid)
	}
	gap := m.w - lw - mw - rw
	var row string
	if gap >= 4 {
		left := gap / 2
		row = brand + strings.Repeat(" ", left) + mid + strings.Repeat(" ", gap-left) + right
	} else {
		row = kit.Fit(brand+"   "+mid, m.w-rw-1) + " " + right
	}
	return kit.Fit(row, m.w) + "\n" + kit.GlowRule(m.w, "━", acc, m.th.Backdrop(1, m.h), m.sweep())
}

// sweep is where the masthead rule's highlight is after a focus change: it
// crosses once, left to right, on the same clock as the panel border flow.
// It starts and ends off the rule, so the rule never jumps when it stops.
func (m Model) sweep() float64 {
	since := m.now().Sub(m.focusAt)
	if since >= focusFlow {
		return 1
	}
	t := since.Seconds() / focusFlow.Seconds()
	return 1 - math.Pow(1-t, 3)
}

// footer shows the current screen's key hints and any flash message.
func (m Model) footer() string {
	hints := m.hints()
	left := " " + kit.Hints(hints, m.th, m.accent(), m.w-24)
	right := kit.S(m.th.Faint).Render(versionLabel() + " ")
	if m.flash != "" && m.now().Sub(m.flashAt) < 6*time.Second {
		right = kit.S(m.th.Warn).Render(m.flash+" ") + right
	}
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return kit.Fit(left, m.w)
	}
	return left + strings.Repeat(" ", gap) + right
}

func versionLabel() string {
	if version.Version == "dev" {
		return "dev build"
	}
	return "v" + version.Version
}

func (m Model) hints() []kit.Hint {
	switch m.screen {
	case scrHome:
		return []kit.Hint{{"↑↓", "move"}, {"⏎", "open"}, {"n", "new thread"}, {"/", "jump"}, {"p", "profile"}, {"?", "help"}, {"q", "quit"}}
	case scrBoard:
		return []kit.Hint{{"↑↓", "move"}, {"⏎", "read"}, {"n", "new thread"}, {"esc", "back"}, {"/", "jump"}, {"?", "help"}}
	case scrThread:
		return []kit.Hint{{"↑↓", "posts"}, {"r", "reply"}, {"f", "kudos"}, {"pgup/dn", "scroll"}, {"esc", "back"}, {"?", "help"}}
	case scrProfile:
		return []kit.Hint{{"tab", "field"}, {"←→", "palette"}, {"ctrl+s", "save"}, {"esc", "back"}}
	}
	return nil
}

// viewTooSmall is shown when the terminal cannot fit the layout.
func (m Model) viewTooSmall() string {
	msg := kit.GradientBold("bulletin", m.th.Brand.Stops) + "\n\n" +
		kit.S(m.th.Subtle).Render(fmt.Sprintf("needs %d×%d", minW, minH)) + "\n" +
		kit.S(m.th.Muted).Render(fmt.Sprintf("you have %d×%d", m.w, m.h))
	block := lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, lipgloss.NewStyle().Align(lipgloss.Center).Render(msg))
	sc := kit.NewScene(m.w, m.h, m.th)
	sc.Base = block
	return sc.Render()
}

// panelPhase is the border rotation for a focused panel: it flows quickly
// right after focus changes, then settles.
func (m Model) panelPhase() float64 {
	since := m.now().Sub(m.focusAt)
	if since >= focusFlow {
		return 0
	}
	t := since.Seconds() / focusFlow.Seconds()
	return 1 - math.Pow(1-t, 3) // ease-out: one full lap
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
