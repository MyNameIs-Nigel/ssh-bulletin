package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

func (m *Model) openHelp() tea.Cmd {
	m.helpTop = 0
	m.compose.lastShown = ovHelp
	m.openOverlay(ovHelp)
	return nil
}

func (m *Model) updateHelp(msg tea.Msg) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return
	}
	switch k.String() {
	case "esc", "?", "q":
		m.closeOverlay()
	case "up", "k":
		m.scrollHelp(-1)
	case "down", "j":
		m.scrollHelp(1)
	case "pgup", "ctrl+u":
		m.scrollHelp(-m.helpRows())
	case "pgdown", "ctrl+d", "space":
		m.scrollHelp(m.helpRows())
	case "home", "g":
		m.helpTop = 0
	case "end", "G":
		p := m.helpPanel()
		w, _ := p.Inner()
		m.helpTop = max(0, len(m.helpLines(w))-m.helpRows())
	}
}

func (m Model) helpRows() int { return max(1, m.h-7) } // four margins, two border rows, one controls row

func (m Model) helpPanel() kit.Panel {
	return kit.Panel{Title: "? Help", Width: min(m.w-4, 78), Height: m.helpRows() + 3, Stops: m.accent(),
		Focused: true, Phase: m.panelPhase(), Fill: m.th.Overlay, Theme: m.th, PadX: 2}
}

func (m *Model) scrollHelp(delta int) {
	w, _ := m.helpPanel().Inner()
	last := max(0, len(m.helpLines(w))-m.helpRows())
	m.helpTop = max(0, min(min(m.helpTop, last)+delta, last))
}

func (m Model) helpLines(w int) []string {
	acc := m.accent()
	heading := func(title string) string {
		return kit.Gradient(strings.ToUpper(title), lipgloss.NewStyle().Bold(true), acc, 0)
	}
	groups := []struct {
		title string
		keys  []kit.Hint
	}{
		{"navigation", []kit.Hint{{"/ ctrl+k", "jump menu"}, {"u", "user directory"}, {"p", "edit your profile"}, {"i", "inbox from Boards"}, {"esc", "back"}, {"ctrl+c", "leave"}}},
		{"boards", []kit.Hint{{"↑↓ j k", "move"}, {"⏎ → l", "open"}, {"g G", "top / bottom"}, {"n", "new thread"}, {"o", "thread options"}, {"tab", "focus chat, then Enter"}}},
		{"threads", []kit.Hint{{"↑↓", "post by post"}, {"tab", "links in post"}, {"o", "post options"}, {"r ⏎", "reply"}, {"f +", "kudos"}, {"!", "report"}, {"z", "reveal a muted post"}}},
		{"writing", []kit.Hint{{"ctrl+s", "post"}, {"tab", "title ↔ body"}, {"esc", "save draft"}, {"ctrl+x", "discard draft"}, {"⏎", "send in chat"}, {"tab", "chat: pick message"}}},
	}
	if m.canModerate() {
		groups[0].keys = append(groups[0].keys, kit.Hint{"m", "moderation"})
	}
	col := func(gi int) string {
		g := groups[gi]
		lines := []string{heading(g.title)}
		for i, k := range g.keys {
			key := kit.S(kit.At(acc, float64(i)/float64(max(1, len(g.keys)-1)))).Bold(true).Render(kit.Fit(k[0], 11))
			lines = append(lines, key+kit.S(m.th.Subtle).Render(k[1]))
		}
		return strings.Join(lines, "\n")
	}
	left, right := col(0), col(1)
	threads, writing := col(2), col(3)
	controls := []string{left, right, threads, writing}
	if lipgloss.Width(left)+4+lipgloss.Width(right) <= w && lipgloss.Width(threads)+4+lipgloss.Width(writing) <= w {
		controls = []string{kit.SideBySide(4, left, right), kit.SideBySide(4, threads, writing)}
	}
	code := kit.S(kit.At(acc, 0.5)).Bold(true)
	wrap := func(s string) string { return kit.S(m.th.Subtle).Render(kit.Wrap(s, w)) }
	sections := []string{m.rtt.String()}
	sections = append(sections, controls...)
	sections = append(sections,
		heading("formatting")+"\n"+
			code.Render(kit.Wrap("**bold**  *italic*  `code`  ~~strike~~  - list  1. list  > quote", w))+"\n"+
			wrap("@user, #board, #12 (thread), #12/3 (post), [label](link)")+"\n"+
			wrap("Links outside the Bulletin show their destination first."),
		heading("public visibility")+"\n"+
			wrap("Your SSH key is your account. Posts and chat are public to every arcade player.")+"\n"+
			wrap("To send a private message or request a review, choose Contact staff from the jump menu."))
	return strings.Split(strings.Join(sections, "\n\n"), "\n")
}

func (m Model) viewHelp() string {
	p := m.helpPanel()
	w, _ := p.Inner()
	lines := m.helpLines(w)
	rows := min(m.helpRows(), len(lines))
	top := min(m.helpTop, len(lines)-rows)
	p.Height = rows + 3
	if rows < len(lines) {
		p.Note = fmt.Sprintf("%d–%d / %d", top+1, top+rows, len(lines))
	}
	hints := []kit.Hint{{"↑", "up"}, {"↓", "down"}, {"pgup/dn", "page"}, {"esc", "close"}}
	return p.Render(strings.Join(lines[top:top+rows], "\n") + "\n" + m.hintBar(hints, m.accent(), w, "help"))
}
