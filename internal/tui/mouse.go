package tui

import (
	tea "charm.land/bubbletea/v2"
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
	}
	return nil
}

// onClick selects a list row; clicking the selected row opens it.
func (m *Model) onClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft || m.overlay != ovNone {
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
		list, _ := m.boardLayout()
		if i, ok := m.rowHit(list, msg.X, msg.Y, m.threadMenu.ItemAtRow); ok {
			if i == m.threadMenu.Cursor {
				return m.enterThread(m.threads[i])
			}
			m.threadMenu.Select(i)
			return m.wantPreview()
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
