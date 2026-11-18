package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

const (
	threadPadX       = 2
	postHeaderOffset = 9 // selection rail, sigil and gutters
)

// threadRect is the reading column: centered, capped for comfortable lines.
func (m Model) threadRect() rect {
	b := m.body()
	w := min(m.w-2, 108)
	return rect{(m.w - w) / 2, b.y, w, b.h}
}

func (m *Model) updateThread(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "up", "k":
		if m.postCursor > 0 {
			m.postCursor--
		}
	case "down", "j":
		if m.postCursor < len(m.posts)-1 {
			m.postCursor++
		}
	case "home", "g":
		m.postCursor, m.postTop = 0, 0
	case "end", "G":
		m.postCursor = max(0, len(m.posts)-1)
	case "pgdown", "space", "ctrl+d":
		m.postTop += m.threadRect().h / 2
	case "pgup", "ctrl+u":
		m.postTop = max(0, m.postTop-m.threadRect().h/2)
	case "r", "enter":
		if len(m.posts) > 0 {
			m.openCompose(composeReply)
		}
	case "!", "shift+1":
		if m.postCursor < len(m.posts) && m.acct != nil && m.posts[m.postCursor].Placeholder() == "" {
			m.startModerationForm(board.Reported, m.posts[m.postCursor].ID, m.curThread.Title, m.postCursor == 0)
		}
	case "o":
		m.openOptions()
	case "f", "+":
		if m.postCursor < len(m.posts) && m.acct != nil && m.posts[m.postCursor].Placeholder() == "" {
			p := &m.posts[m.postCursor]
			// Optimistic: flip locally now, the broker event reconciles.
			p.KudosByViewer = !p.KudosByViewer
			if p.KudosByViewer {
				p.Kudos++
			} else {
				p.Kudos--
			}
			post, acct, d := *p, *m.acct, m.d
			return func() tea.Msg {
				_, err := d.Service.ToggleKudos(d.Context, acct, post)
				return kudosMsg{err: err}
			}
		}
	}
	m.followCursor()
	return nil
}

// threadLines renders the whole thread into lines, and records where each
// post card starts so the cursor can be kept in view.
func (m Model) threadLines(iw int) (lines []string, starts []int) {
	stops := kit.PaletteNamed(m.curBoard.Palette).Stops
	if m.curBoard.ID == 0 || m.curBoard.ID != m.curThread.BoardID {
		for _, b := range m.boards {
			if b.ID == m.curThread.BoardID {
				stops = kit.PaletteNamed(b.Palette).Stops
			}
		}
	}
	title := kit.Gradient(m.curThread.Title, lipgloss.NewStyle().Bold(true), stops, 0)
	if m.curThread.OpeningUnavailable {
		title = m.placeholderStyle().Render(m.curThread.Title)
	}
	head := kit.Wrap(title, iw)
	lines = append(lines, "")
	lines = append(lines, strings.Split(head, "\n")...)
	meta := kit.S(m.th.Muted).Render(fmt.Sprintf("%s · started %s", kit.Plural(m.curThread.ReplyCount, "reply", "replies"), kit.Ago(m.curThread.CreatedAt, m.now())))
	if m.curThread.Pinned {
		// Old threads stay on top because staff pinned them, not because
		// they are current: say so where the age is shown.
		meta = pinnedChip(m.th) + " " + kit.S(m.th.Muted).Render("stays on top") + kit.S(m.th.Faint).Render(" · ") + meta
	}
	lines = append(lines, meta)
	lines = append(lines, kit.FadeRule(iw, "─", kit.At(stops, 0.5), m.th.BgBottom), "")

	for i, p := range m.posts {
		starts = append(starts, len(lines))
		lines = append(lines, m.postCard(p, i, iw, i == m.postCursor)...)
		lines = append(lines, "")
	}
	return lines, starts
}

// postCard draws one post: sigil, author in their own gradient, time,
// kudos, then the wrapped body. The selected card wears a glowing rail.
func (m Model) postCard(p board.Post, idx, iw int, selected bool) []string {
	astops := kit.PaletteNamed(p.AuthorPalette).Stops
	sigil := strings.Split(kit.Sigil(p.AuthorKey, astops, 1), "\n")

	name := kit.GradientBold(p.AuthorHandle, astops) + m.friendFlair(p.AuthorID) + staffFlair(p.AuthorRole, kit.At(astops, 0.7))
	if idx == 0 {
		name += " " + kit.Chip("OP", kit.At(astops, 0.7), true)
	}
	name += postFlair(p.Flair, astops)
	when := kit.S(m.th.Muted).Render(" · " + kit.Ago(p.CreatedAt, m.now()))
	bodyW := iw - postHeaderOffset
	head := name + when
	if p.Placeholder() == "" {
		heart := kit.S(m.th.Faint).Render("♡")
		if p.KudosByViewer {
			heart = kit.S(m.th.Bad).Render("♥")
		}
		kudos := heart + kit.S(m.th.Muted).Render(fmt.Sprintf(" %d", p.Kudos))
		head = kit.Fit(head, bodyW-lipgloss.Width(kudos)) + kudos
	}
	ink := m.th.Subtle
	if selected {
		ink = m.th.Text
	}
	body := m.renderPostBody(p, bodyW, kit.S(ink))
	right := append([]string{head}, strings.Split(body, "\n")...)
	n := max(len(right), len(sigil))
	out := make([]string, n)
	for i := range n {
		rail := " "
		if selected {
			rail = kit.S(kit.At(astops, float64(i)/float64(max(1, n-1)))).Render("▌")
		}
		left := "     "
		if i < len(sigil) {
			left = kit.Fit(sigil[i], 5)
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		out[i] = rail + " " + left + "  " + r
	}
	return out
}

func (m Model) placeholderStyle() lipgloss.Style {
	return kit.S(m.th.Muted).Italic(true)
}

func (m Model) renderPostBody(p board.Post, width int, style lipgloss.Style) string {
	if marker := p.Placeholder(); marker != "" {
		return m.placeholderStyle().Render(marker)
	}
	return style.Render(kit.Wrap(p.Body, width))
}

// followCursor scrolls so the selected post's header is on screen.
func (m *Model) followCursor() {
	r := m.threadRect()
	p := kit.Panel{Width: r.w, Height: r.h, PadX: threadPadX}
	iw, ih := p.Inner()
	lines, starts := m.threadLines(iw)
	if m.postCursor >= len(starts) {
		return
	}
	start := starts[m.postCursor]
	end := len(lines)
	if m.postCursor+1 < len(starts) {
		end = starts[m.postCursor+1]
	}
	if start < m.postTop {
		m.postTop = start
	}
	if end > m.postTop+ih {
		m.postTop = min(start, end-ih)
	}
	m.postTop = m.threadTop(len(lines), ih)
}

func (m Model) threadTop(lines, height int) int {
	return max(0, min(m.postTop, max(0, lines-height)))
}

func (m Model) viewThread() string {
	r := m.threadRect()
	acc := m.accent()
	note := ""
	if len(m.posts) > 0 {
		note = fmt.Sprintf("%d / %d", m.postCursor+1, len(m.posts))
	}
	p := kit.Panel{Title: "◆ thread", Note: note, Width: r.w, Height: r.h, Stops: acc, Focused: true, Phase: m.panelPhase(), Theme: m.th, PadX: threadPadX}
	iw, ih := p.Inner()
	if len(m.posts) == 0 {
		return pad(r.x, p.Render("\n"+kit.Spinner(m.frame, acc)+kit.S(m.th.Muted).Render(" opening thread…")))
	}
	lines, _ := m.threadLines(iw)
	top := m.threadTop(len(lines), ih)
	view := lines[top:min(len(lines), top+ih)]
	// Scroll indicator on the right edge of the inner area.
	out := kit.Block(strings.Join(view, "\n"), iw, ih)
	if len(lines) > ih {
		out = withScrollbar(out, top, ih, len(lines), m.th, acc)
	}
	return pad(r.x, p.Render(out))
}

// withScrollbar overlays a slim gradient thumb on the last column.
func withScrollbar(block string, top, height, total int, th kit.Theme, stops []colorT) string {
	rows := strings.Split(block, "\n")
	thumb := max(1, height*height/total)
	pos := (height - thumb) * top / max(1, total-height)
	for i, r := range rows {
		w := lipgloss.Width(r)
		mark := kit.S(th.Faint).Render("│")
		if i >= pos && i < pos+thumb {
			mark = kit.S(kit.At(stops, float64(i)/float64(max(1, height-1)))).Render("┃")
		}
		// Cut the last cell without an ellipsis: rows are padded to the
		// full width, so a "…" would land beside the bar on every row.
		rows[i] = ansi.Truncate(r, w-1, "") + mark
	}
	return strings.Join(rows, "\n")
}

func pad(x int, block string) string {
	sp := strings.Repeat(" ", x)
	return sp + strings.ReplaceAll(block, "\n", "\n"+sp)
}
