package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

func threadItems(ts []board.Thread, now time.Time) []kit.Item {
	items := make([]kit.Item, len(ts))
	for i, t := range ts {
		badge := ""
		switch {
		case t.Pinned:
			badge = "PINNED"
		case now.Unix()-t.CreatedAt < 6*3600:
			badge = "NEW"
		}
		items[i] = kit.Item{
			Title: t.Title,
			Desc:  fmt.Sprintf("%s · %s", t.AuthorHandle, kit.Plural(t.ReplyCount, "reply", "replies")),
			Meta:  kit.Ago(t.LastPostAt, now),
			Badge: badge,
			Stops: kit.PaletteNamed(t.AuthorPalette).Stops,
		}
	}
	return items
}

func (m Model) boardLayout() (list, preview rect) {
	b := m.body()
	if m.w < 100 {
		return rect{1, b.y, m.w - 2, b.h}, rect{}
	}
	lw := m.w * 52 / 100
	return rect{1, b.y, lw, b.h}, rect{lw + 2, b.y, m.w - lw - 3, b.h}
}

func (m *Model) updateBoard(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "up", "k":
		if m.threadMenu.Move(-1) {
			return m.wantPreview()
		}
	case "down", "j":
		if m.threadMenu.Move(1) {
			return m.wantPreview()
		}
	case "home", "g":
		m.threadMenu.Select(0)
		return m.wantPreview()
	case "end", "G":
		m.threadMenu.Select(len(m.threads) - 1)
		return m.wantPreview()
	case "enter", "right", "l":
		if i := m.threadMenu.Selected(); i >= 0 {
			return m.enterThread(m.threads[i])
		}
	case "n":
		m.openCompose(composeThread)
	}
	return nil
}

// wantPreview fetches the highlighted thread's opening post if the preview
// pane is visible and it is not cached yet.
func (m *Model) wantPreview() tea.Cmd {
	if _, pv := m.boardLayout(); pv.w == 0 {
		return nil
	}
	i := m.threadMenu.Selected()
	if i < 0 || i >= len(m.threads) {
		return nil
	}
	id := m.threads[i].ID
	if _, ok := m.previews[id]; ok {
		return nil
	}
	return loadPreview(m.d, id)
}

func (m Model) viewBoard() string {
	list, pv := m.boardLayout()
	acc := m.accent()
	stops := kit.PaletteNamed(m.curBoard.Palette).Stops

	lp := kit.Panel{Title: "◆ " + m.curBoard.Title, Note: kit.Plural(int64(len(m.threads)), "thread", "threads"),
		Width: list.w, Height: list.h, Stops: acc, Focused: true, Phase: m.panelPhase(), Theme: m.th}
	iw, ih := lp.Inner()
	var inner string
	switch {
	case m.loading && len(m.threads) == 0:
		inner = "\n" + kit.Spinner(m.frame, stops) + kit.S(m.th.Muted).Render(" loading threads…")
	case len(m.threads) == 0:
		// The orb sits three rows into the panel; its rim fades into the
		// backdrop there.
		inner = emptyBoard(m.th, stops, iw, m.th.Backdrop(list.y+3, m.h))
	default:
		menu := m.threadMenu
		inner = "\n" + menu.Render(iw, ih-1, m.th, true)
	}
	left := lp.Render(inner)
	if pv.w == 0 {
		return kit.Indent(left, list.x)
	}
	return kit.Indent(kit.SideBySide(1, left, m.threadPreview(pv, stops)), list.x)
}

func emptyBoard(th kit.Theme, stops []colorT, w int, bg colorT) string {
	art := kit.Orb(4, kit.At(stops, 0.2), kit.At(stops, 0.9), bg)
	msg := kit.GradientBold("A quiet board.", stops) + "\n" +
		kit.S(th.Muted).Render("Every board starts with one brave post.") + "\n\n" +
		kit.S(th.Faint).Render("press ") + kit.S(kit.At(stops, 0.4)).Bold(true).Render("n") + kit.S(th.Faint).Render(" to start the first thread")
	return "\n\n" + kit.Center(art, w) + "\n\n" + kit.Center(lipgloss.NewStyle().Align(lipgloss.Center).Render(msg), w)
}

// threadPreview shows the highlighted thread's opening post.
func (m Model) threadPreview(r rect, stops []colorT) string {
	p := kit.Panel{Title: "preview", Width: r.w, Height: r.h, Stops: stops, Theme: m.th}
	iw, _ := p.Inner()
	i := m.threadMenu.Selected()
	if i < 0 || i >= len(m.threads) {
		return p.Render("")
	}
	t := m.threads[i]
	astops := kit.PaletteNamed(t.AuthorPalette).Stops
	var b strings.Builder
	b.WriteString("\n" + kit.Wrap(kit.Gradient(t.Title, lipgloss.NewStyle().Bold(true), stops, 0), iw) + "\n")
	b.WriteString(kit.S(m.th.Muted).Render("started by ") + kit.GradientBold(t.AuthorHandle, astops) +
		kit.S(m.th.Muted).Render(" · "+kit.Ago(t.CreatedAt, m.now())) + "\n\n")
	if posts, ok := m.previews[t.ID]; ok && len(posts) > 0 {
		b.WriteString(kit.S(m.th.Subtle).Render(kit.Wrap(posts[0].Body, iw)))
	} else {
		b.WriteString(kit.Spinner(m.frame, stops))
	}
	b.WriteString("\n\n" + kit.FadeRule(iw, "─", kit.At(stops, 0.5), m.th.BgBottom) + "\n")
	b.WriteString(kit.S(m.th.Faint).Render("⏎ read the whole thread"))
	return p.Render(b.String())
}
