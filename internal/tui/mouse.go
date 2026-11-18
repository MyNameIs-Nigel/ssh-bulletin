package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

// onWheel scrolls whatever list is under the pointer.
func (m *Model) onWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if m.overlay != ovNone {
		return nil
	}
	d := 1
	if msg.Button == tea.MouseWheelUp {
		d = -1
	}
	switch m.screen {
	case scrHome:
		if m.boardMenu.Move(d) {
			return m.peekSelected()
		}
	case scrBoard:
		if m.threadMenu.Move(d) {
			return m.wantPreview()
		}
	case scrThread:
		m.postTop = max(0, m.postTop+3*d)
	case scrChat:
		return m.scrollChat(-3 * d)
	case scrDirectory:
		m.directory.menu.Move(d)
		m.directory.menu.Snap()
	}
	return nil
}

// onClick selects a list row; clicking the selected row opens it.
func (m *Model) onClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft || m.overlay != ovNone || m.w < minW || m.h < minH {
		return nil
	}
	switch m.screen {
	case scrSplash:
		m.leaveSplash()
	case scrHome:
		list, _ := m.homeLayout()
		if i, ok := m.rowHit(list, msg.X, msg.Y, m.boardMenu.ItemAtRow); ok {
			if i == m.boardMenu.Cursor {
				return m.enterBoard(m.boards[i])
			}
			m.boardMenu.Select(i)
			return m.peekSelected()
		}
	case scrBoard:
		if m.chatSectionRect().contains(msg.X, msg.Y) {
			if m.chatFocus {
				return m.openChat()
			}
			m.focusChatSection(true)
			return nil
		}
		list, _ := m.boardLayout()
		if list.contains(msg.X, msg.Y) {
			m.focusChatSection(false)
		}
		if i, ok := m.rowHit(list, msg.X, msg.Y, m.threadMenu.ItemAtRow); ok {
			if i == m.threadMenu.Cursor {
				return m.enterThread(m.threads[i], 0)
			}
			m.threadMenu.Select(i)
			return m.wantPreview()
		}
	case scrThread:
		r := m.threadRect()
		p := kit.Panel{Width: r.w, Height: r.h, PadX: threadPadX}
		iw, ih := p.Inner()
		row := msg.Y - r.y - 1
		nameX := r.x + 1 + threadPadX + postHeaderOffset
		if row < 0 || row >= ih || msg.X < nameX || msg.X >= nameX+iw-postHeaderOffset {
			break
		}
		lines, starts := m.threadLines(iw)
		for i, start := range starts {
			if start == row+m.threadTop(len(lines), ih) && msg.X < nameX+lipgloss.Width(m.posts[i].AuthorHandle) {
				m.postCursor = i
				return m.openUserProfile(m.posts[i].AuthorID)
			}
		}
	case scrDirectory:
		r := m.socialRect()
		if msg.X >= r.x+2 && msg.X < r.x+r.w-2 && msg.Y < r.y+r.h-1 {
			if i := m.directory.menu.ItemAtRow(msg.Y - r.y - 1 - directoryHeaderRows); i >= 0 {
				m.directory.menu.Select(i)
				return m.openUserProfile(m.directory.entries[i].ID)
			}
		}
	}
	return nil
}

// rowHit maps a click inside a list panel to an item index. Menus start one
// row below the panel's top border plus one blank row.
func (m *Model) rowHit(panel rect, x, y int, at func(int) int) (int, bool) {
	if !panel.contains(x, y) {
		return 0, false
	}
	_, oy := menuOrigin(panel)
	i := at(y - oy)
	return i, i >= 0
}
