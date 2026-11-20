package tui

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/markup"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
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
	if m.idleSplash {
		return m.viewIdleSplash()
	}
	if m.reuse && m.ui.valid && m.ui.stamp == m.stamp {
		return m.ui.view
	}
	clear(m.ui.targets)
	var content string
	if m.w < minW || m.h < minH {
		content = m.viewTooSmall()
	} else {
		content = m.compose3()
	}
	// All motion, not just drags: hover needs to know where the pointer is.
	// Motion that changes nothing reuses this frame (see Update).
	v := m.terminalView(content, tea.MouseModeAllMotion)
	m.ui.view, m.ui.stamp, m.ui.valid = v, m.stamp, true
	return v
}

func (m Model) terminalView(content string, mouseMode tea.MouseMode) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = mouseMode
	v.BackgroundColor = m.th.BgTop
	v.WindowTitle = "bulletin · ssharcade"
	return v
}

// compose3 builds the base frame for the current screen and layers
// overlays and toasts over it.
func (m Model) compose3() string {
	sc := kit.NewScene(m.w, m.h, m.th)
	sc.Hot = &m.ui.hot
	sc.Modal = m.overlay != ovNone
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
		case scrChat:
			bodyStr = m.viewChat()
		case scrModeration:
			bodyStr = m.viewModeration()
		case scrProfile:
			bodyStr = m.viewIdentity(true)
		case scrDirectory:
			bodyStr = m.viewDirectory()
		case scrUserProfile:
			bodyStr = m.viewUserProfile()
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

	crumbs, runs := m.crumbs()
	mid := m.crumbTrail(crumbs, runs, acc)

	online := m.d.Service.Broker().Subscribers()
	dot := kit.S(kit.Lerp(m.th.Good, m.th.BgTop, 0.35*kit.Pulse(time.Duration(m.frame)*time.Second/kit.FrameRate, 2*time.Second))).Render("●")
	right := dot + kit.S(m.th.Subtle).Render(fmt.Sprintf(" %d online ", online))
	if m.acct != nil {
		// The handle wears its palette as ink rather than as a filled tag: a
		// filled cell can only have square corners in a terminal.
		right += "  " + m.authorName("me", m.acct.ID, m.acct.Handle, kit.PaletteNamed(m.acct.Palette).Stops) + staffFlair(m.acct.Role, m.th.Warn) + " "
	}

	lw, mw, rw := lipgloss.Width(brand), lipgloss.Width(mid), lipgloss.Width(right)
	if m.w-lw-mw-rw < 4 && len(crumbs) > 1 {
		mid = m.crumbTrail([]string{"…", crumbs[len(crumbs)-1]}, []func(*Model) tea.Cmd{runs[len(runs)-2], nil}, acc)
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

// crumbs is the breadcrumb trail and what clicking each crumb does; the last
// crumb is where you are, so it does nothing.
func (m Model) crumbs() ([]string, []func(*Model) tea.Cmd) {
	home := func(m *Model) tea.Cmd { m.goHome(); return m.loadHome() }
	toBoard := (*Model).back // from a thread or the chat, back is the board
	switch m.screen {
	case scrBoard:
		return []string{"boards", m.curBoard.Title}, []func(*Model) tea.Cmd{home, nil}
	case scrThread:
		return []string{"boards", m.curBoard.Title, clip(m.curThread.Title, 32)}, []func(*Model) tea.Cmd{home, toBoard, nil}
	case scrChat:
		return []string{"boards", m.curBoard.Title, "live chat"}, []func(*Model) tea.Cmd{home, toBoard, nil}
	case scrModeration:
		return []string{"moderation"}, []func(*Model) tea.Cmd{nil}
	case scrProfile:
		return []string{"edit profile"}, []func(*Model) tea.Cmd{nil}
	case scrDirectory:
		return []string{"directory"}, []func(*Model) tea.Cmd{nil}
	case scrUserProfile:
		return []string{"profile"}, []func(*Model) tea.Cmd{nil}
	}
	return []string{"boards"}, []func(*Model) tea.Cmd{nil}
}

// crumbTrail draws the breadcrumbs; every crumb but the last is a hot spot.
func (m Model) crumbTrail(crumbs []string, runs []func(*Model) tea.Cmd, acc []colorT) string {
	var out []string
	for i, c := range crumbs {
		key := ""
		if runs[i] != nil {
			key = "crumb:" + strconv.Itoa(i) + ":" + c
		}
		switch {
		case i == len(crumbs)-1:
			out = append(out, kit.GradientBold(c, acc))
		case m.lit(key):
			out = append(out, m.hot(key, runs[i], kit.Flow(c, lipgloss.NewStyle(), acc, true, m.phase(), m.th)))
		default:
			out = append(out, m.hot(key, runs[i], kit.S(m.th.Muted).Render(c)))
		}
	}
	return strings.Join(out, kit.S(m.th.Faint).Render("  ›  "))
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
	if m.restriction != nil && m.overlay == ovNone {
		label := m.hot("foot:contact", (*Model).openStaffContact, "Contact staff")
		return kit.Fit(kit.S(m.th.Warn).Render(" Account "+string(m.restriction.Kind)+" · ")+label+kit.S(m.th.Muted).Render(" · jump menu"), m.w)
	}
	if m.acct != nil && m.acct.HandleChangeRequired && m.overlay == ovNone {
		edit := m.hot("foot:rename", func(m *Model) tea.Cmd { m.openProfile(); return nil }, "Edit profile")
		return kit.Fit(kit.S(m.th.Warn).Render(" Choose a new handle to post · ")+edit, m.w)
	}
	hints := m.hints()
	left := " " + m.hintBar(hints, m.accent(), m.w-24, "foot")
	right := kit.S(m.th.Faint).Render(versionLabel(m.d.Version) + " ")
	if m.flash != "" && m.now().Sub(m.flashAt) < 6*time.Second {
		right = kit.S(m.th.Warn).Render(m.flash+" ") + right
	}
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return kit.Fit(left, m.w)
	}
	return left + strings.Repeat(" ", gap) + right
}

func staffFlair(role board.Role, ink color.Color) string {
	if role.CanModerate() {
		return " " + kit.Chip(string(role), ink, true)
	}
	return ""
}

// pinnedChip marks a sticky thread wherever its age is shown.
func pinnedChip(th kit.Theme) string { return kit.Chip("PINNED", th.Warn, true) }

func versionLabel(v string) string {
	if v == "" || v == "dev" {
		return "dev build"
	}
	return "v" + v
}

func (m Model) hints() []kit.Hint {
	switch m.screen {
	case scrHome:
		if m.canModerate() {
			return []kit.Hint{{"↑↓", "move"}, {"⏎", "open"}, {"u", "directory"}, {"m", "moderation"}, {"?", "help"}}
		}
		return []kit.Hint{{"↑↓", "move"}, {"⏎", "open"}, {"u", "users"}, {"p", "edit"}, {"n", "new thread"}, {"/", "jump"}, {"?", "help"}}
	case scrBoard:
		if m.chatFocus {
			return []kit.Hint{{"⏎", "join chat"}, {"tab", "threads"}, {"n", "new thread"}, {"esc", "back"}, {"?", "help"}}
		}
		return []kit.Hint{{"⏎", "read"}, {"o", "options"}, {"↑↓", "move"}, {"tab", "chat"}, {"n", "new thread"}, {"esc", "back"}, {"?", "help"}}
	case scrChat:
		return m.chatHints()
	case scrThread:
		links := m.selectedLinks()
		if m.linkSel >= 0 && m.linkSel < len(links) {
			verb := "open link"
			if links[m.linkSel].Ref.Kind == markup.URL {
				verb = "check link"
			}
			return []kit.Hint{{"⏎", verb}, {"tab", "next link"}, {"esc", "done"}, {"↑↓", "posts"}}
		}
		hints := []kit.Hint{{"↑↓", "posts"}, {"o", "options"}}
		if len(links) > 0 {
			hints = append(hints, kit.Hint{"tab", "links"})
		}
		if !m.curThread.Removed {
			hints = append(hints, kit.Hint{"r", "reply"})
		}
		if m.postCursor < len(m.posts) && m.posts[m.postCursor].Placeholder() == "" {
			hints = append(hints, kit.Hint{"f", "kudos"}, kit.Hint{"!", "report"})
		}
		return append(hints, kit.Hint{"?", "help"})
	case scrModeration:
		return m.moderationHints()
	case scrProfile:
		return []kit.Hint{{"tab", "field"}, {"←→", "choose"}, {"ctrl+s", "save"}, {"esc", "back"}}
	case scrDirectory:
		return []kit.Hint{{"↑↓", "select"}, {"⏎", "profile"}, {"esc", "back"}}
	case scrUserProfile:
		var hints []kit.Hint
		if a := m.userProfile.data; a.ID != 0 {
			if a.ID != m.viewerID() && m.acct != nil {
				hints = append(hints, kit.Hint{"o", "options"})
			}
			switch {
			case a.ID == m.viewerID():
				hints = append(hints, kit.Hint{"e", "edit profile"})
			case m.userProfile.saving:
				hints = append(hints, kit.Hint{"f", "saving…"})
			case m.friends[a.ID]:
				hints = append(hints, kit.Hint{"f", "remove friend"})
			default:
				hints = append(hints, kit.Hint{"f", "add friend"})
			}
			if len(a.Recent) > 0 {
				hints = append(hints, kit.Hint{"⏎", "read post"})
			}
		}
		return append(hints, kit.Hint{"esc", "back"}, kit.Hint{"↑↓", "select"})
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
	sc.Hot = &m.ui.hot // nothing is clickable here
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
