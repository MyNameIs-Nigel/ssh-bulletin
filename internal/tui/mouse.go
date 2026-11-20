package tui

import (
	tea "charm.land/bubbletea/v2"
)

// onWheel scrolls whatever list is under the pointer.
func (m *Model) onWheel(msg tea.MouseWheelMsg) tea.Cmd {
	d := 1
	if msg.Button == tea.MouseWheelUp {
		d = -1
	}
	if m.overlay != ovNone {
		if m.overlay == ovHelp {
			m.scrollHelp(3 * d)
		}
		if m.overlay == ovOptions && !m.opts.confirming && !m.opts.busy {
			m.opts.menu.Move(d)
			m.opts.menu.Snap()
		}
		return nil
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
	case scrInbox:
		if !m.inbox.busy && !m.inbox.loading {
			if m.inbox.menu.Move(d) {
				m.inbox.menu.Snap()
			} else if (d > 0 && m.inbox.more) || (d < 0 && m.inbox.page > 0) {
				return m.pageInbox(d)
			}
		}
	}
	return nil
}

// onClick runs the hot spot under the pointer (see hover.go): list rows,
// links, names, breadcrumbs, hints and buttons all register themselves as
// they are drawn. A click anywhere on the splash moves past it.
func (m *Model) onClick(msg tea.MouseClickMsg) tea.Cmd {
	if cmd, ok := m.clickHot(msg); ok {
		return cmd
	}
	if msg.Button == tea.MouseLeft && m.overlay == ovNone && m.screen == scrSplash {
		m.leaveSplash()
	}
	return nil
}
