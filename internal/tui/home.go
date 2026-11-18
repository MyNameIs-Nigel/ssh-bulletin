package tui

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

type colorT = color.Color

func boardItems(bs []board.Board, now time.Time) []kit.Item {
	items := make([]kit.Item, len(bs))
	for i, b := range bs {
		meta := kit.Plural(b.ThreadCount, "thread", "threads")
		badge := ""
		if b.LastPostAt > 0 && now.Unix()-b.LastPostAt < 3600 {
			badge = "LIVE"
		}
		items[i] = kit.Item{Title: b.Title, Desc: b.Tagline, Meta: meta, Badge: badge, Stops: kit.PaletteNamed(b.Palette).Stops}
	}
	return items
}

// homeLayout splits the body into the board list and the detail pane.
func (m Model) homeLayout() (list, detail rect) {
	b := m.body()
	lw := max(34, min(56, m.w*42/100))
	if m.w < 96 {
		lw = m.w - 2
	}
	list = rect{1, b.y, lw, b.h}
	if m.w >= 96 {
		detail = rect{lw + 2, b.y, m.w - lw - 3, b.h}
	}
	return list, detail
}

func (m *Model) updateHome(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "up", "k":
		if m.boardMenu.Move(-1) {
			return m.peekSelected()
		}
	case "down", "j":
		if m.boardMenu.Move(1) {
			return m.peekSelected()
		}
	case "home", "g":
		m.boardMenu.Select(0)
	case "end", "G":
		m.boardMenu.Select(len(m.boards) - 1)
	case "enter", "right", "l":
		if i := m.boardMenu.Selected(); i >= 0 {
			return m.enterBoard(m.boards[i])
		}
	case "n":
		if i := m.boardMenu.Selected(); i >= 0 {
			m.curBoard = m.boards[i]
			m.openCompose(composeThread)
		}
	}
	return nil
}

// peekSelected loads the highlighted board's threads for the detail pane.
func (m *Model) peekSelected() tea.Cmd {
	i := m.boardMenu.Selected()
	if i < 0 || i >= len(m.boards) {
		return nil
	}
	return m.loadThreads(m.boards[i].ID)
}

func (m Model) viewHome() string {
	list, detail := m.homeLayout()
	b := m.body()
	acc := m.accent()

	lp := kit.Panel{Title: "◇ Boards", Note: kit.Plural(int64(len(m.boards)), "board", "boards"),
		Width: list.w, Height: list.h, Stops: acc, Focused: true, Phase: m.panelPhase(), Theme: m.th}
	iw, ih := lp.Inner()
	menu := m.boardMenu
	m.menuHot(&menu, "board", func(m *Model, i int) tea.Cmd {
		return pickOrOpen(&m.boardMenu, i, m.peekSelected, func() tea.Cmd { return m.enterBoard(m.boards[i]) })
	})
	left := lp.Render("\n" + menu.Render(iw, ih-1, m.th, true))

	if detail.w == 0 {
		return kit.Indent(left, list.x)
	}
	right := m.boardDetail(detail)
	return kit.Block(kit.Indent(kit.SideBySide(1, left, right), list.x), m.w, b.h)
}

// boardDetail is the right pane on the home screen: the highlighted board's
// identity, its numbers, and its most recent threads.
func (m Model) boardDetail(r rect) string {
	i := m.boardMenu.Selected()
	if i < 0 || i >= len(m.boards) {
		return kit.Panel{Width: r.w, Height: r.h, Theme: m.th}.Render("")
	}
	bd := m.boards[i]
	stops := kit.PaletteNamed(bd.Palette).Stops
	p := kit.Panel{Title: "◆ " + bd.Title, Width: r.w, Height: r.h, Stops: stops, Focused: false, Theme: m.th}
	iw, ih := p.Inner()

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(kit.Gradient(strings.ToUpper(bd.Title), lipgloss.NewStyle().Bold(true), stops, 0) + "\n")
	b.WriteString(kit.S(m.th.Subtle).Italic(true).Render(bd.Tagline) + "\n\n")
	b.WriteString(statLine(m.th, stops,
		[2]string{fmt.Sprint(bd.ThreadCount), plural(bd.ThreadCount, "thread", "threads")},
		[2]string{fmt.Sprint(bd.PostCount), plural(bd.PostCount, "post", "posts")},
		[2]string{kit.Ago(bd.LastPostAt, m.now()), "last post"}) + "\n\n")
	b.WriteString(kit.FadeRule(iw, "─", kit.At(stops, 0.5), m.th.BgBottom) + "\n\n")

	threads := m.peekFor(bd.ID)
	if len(threads) == 0 {
		b.WriteString(kit.S(m.th.Muted).Render("Nothing here yet — be the first.") + "\n")
		b.WriteString(kit.S(m.th.Faint).Render("press ") + kit.S(kit.At(stops, 0.3)).Bold(true).Render("n") + kit.S(m.th.Faint).Render(" to start a thread"))
	}
	for j, t := range threads {
		if j >= max(1, (ih-10)/2) {
			break
		}
		authorStops := kit.PaletteNamed(t.AuthorPalette).Stops
		who := m.authorName("peek:"+strconv.FormatInt(t.ID, 10), t.AuthorID, t.AuthorHandle, authorStops) + m.friendFlair(t.AuthorID) +
			staffFlair(t.AuthorRole, kit.At(authorStops, 0.7)) + postFlair(t.Flair, authorStops)
		style := kit.S(m.th.Text)
		if t.OpeningUnavailable {
			style = m.placeholderStyle()
		}
		key := "peek:" + strconv.FormatInt(t.ID, 10)
		title := style.Render(clip(t.Title, iw-4))
		if m.lit(key) {
			title = kit.Flow(clip(t.Title, iw-4), lipgloss.NewStyle(), stops, true, m.phase(), m.th)
		}
		t, bd := t, bd
		title = m.hot(key, func(m *Model) tea.Cmd {
			m.curBoard = bd
			m.setAccent(kit.PaletteNamed(bd.Palette).Stops)
			return m.enterThread(t, 0)
		}, title)
		b.WriteString(kit.S(kit.At(stops, float64(j)/5)).Render("▍") + title + "\n")
		line := kit.S(m.th.Faint).Render("▍") + who + kit.S(m.th.Muted).Render(fmt.Sprintf(" · %s · %s", kit.Plural(t.ReplyCount, "reply", "replies"), kit.Ago(t.LastPostAt, m.now())))
		if t.Pinned {
			line += " " + pinnedChip(m.th)
		}
		b.WriteString(line + "\n")
	}

	// The community card sits at the bottom of the pane.
	content := kit.Block(b.String(), iw, ih-4)
	community := statLine(m.th, m.th.Brand.Stops,
		[2]string{fmt.Sprint(m.stats.Accounts), plural(m.stats.Accounts, "user", "users")},
		[2]string{fmt.Sprint(m.stats.Threads), plural(m.stats.Threads, "thread", "threads")},
		[2]string{fmt.Sprint(m.stats.Posts), plural(m.stats.Posts, "post", "posts")})
	content += "\n" + kit.FadeRule(iw, "┄", m.th.Line, m.th.BgBottom) + "\n" + kit.S(m.th.Muted).Render("across the bulletin") + "\n" + community
	return p.Render(content)
}

func (m Model) peekFor(boardID int64) []board.Thread {
	if m.curBoard.ID == boardID && m.screen == scrBoard {
		return m.threads
	}
	return m.peek[boardID]
}

func plural(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// statLine renders "12 threads   40 posts" with gradient numbers.
func statLine(th kit.Theme, stops []color.Color, pairs ...[2]string) string {
	var parts []string
	for i, p := range pairs {
		c := kit.At(stops, float64(i)/float64(max(1, len(pairs)-1)))
		parts = append(parts, kit.S(c).Bold(true).Render(p[0])+" "+kit.S(th.Muted).Render(p[1]))
	}
	return strings.Join(parts, "    ")
}
