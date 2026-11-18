package tui

import (
	"errors"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

// optionsState is the staff-only "thread options" menu. It acts on a
// snapshot of the thread taken when it opened, so the entries cannot drift
// onto a different thread if the list reorders underneath it.
type optionsState struct {
	thread board.Thread
	menu   kit.Menu
	busy   bool
}

type pinResultMsg struct {
	title  string
	pinned bool
	err    error
}

// optionsTarget is the thread the options menu would act on right now.
func (m Model) optionsTarget() (board.Thread, bool) {
	switch m.screen {
	case scrBoard:
		if i := m.threadMenu.Selected(); i >= 0 && i < len(m.threads) {
			return m.threads[i], true
		}
	case scrThread:
		if m.curThread.ID != 0 {
			return m.curThread, true
		}
	}
	return board.Thread{}, false
}

// openOptions shows what staff can do to the highlighted or open thread.
func (m *Model) openOptions() {
	t, ok := m.optionsTarget()
	if !ok || !m.canModerate() {
		return
	}
	m.opts = optionsState{thread: t, menu: kit.NewMenu(nil)}
	m.opts.menu.Rows = 1
	m.opts.menu.SetItems(m.optionItems())
	m.compose.lastShown = ovOptions
	m.openOverlay(ovOptions)
}

func (m Model) optionItems() []kit.Item {
	stops := kit.PaletteNamed(m.curBoard.Palette).Stops
	label, desc := "Pin to top of board", "keeps it above newer threads"
	if m.opts.thread.Pinned {
		label, desc = "Unpin from board", "returns it to activity order"
	}
	return []kit.Item{{Title: label, Meta: desc, Stops: stops}}
}

func (m *Model) updateOptions(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok || m.opts.busy {
		return nil
	}
	switch k.String() {
	case "esc", "q", "o":
		m.closeOverlay()
	case "up", "k":
		m.opts.menu.Move(-1)
	case "down", "j":
		m.opts.menu.Move(1)
	case "enter":
		if m.opts.menu.Selected() == 0 {
			return m.togglePin()
		}
	}
	return nil
}

// togglePin sends the opposite of the snapshot's state as an explicit target.
func (m *Model) togglePin() tea.Cmd {
	if m.acct == nil {
		return nil
	}
	m.opts.busy = true
	d, actor, t := m.d, m.acct.ID, m.opts.thread
	pin := !t.Pinned
	return func() tea.Msg {
		return pinResultMsg{title: t.Title, pinned: pin, err: d.Service.SetPinned(d.Context, actor, t.ID, pin)}
	}
}

func (m *Model) receivePin(msg pinResultMsg) tea.Cmd {
	m.opts.busy = false
	if m.overlay == ovOptions {
		m.closeOverlay()
	}
	switch {
	case errors.Is(msg.err, board.ErrForbidden):
		m.setFlash(friendly(msg.err))
		return m.refreshRole()
	case msg.err != nil:
		m.setFlash(friendly(msg.err))
		return nil
	}
	head := "Pinned to the top"
	if !msg.pinned {
		head = "Unpinned"
	}
	m.toasts.Push(m.now(), head, msg.title, m.myStops())
	return nil
}

func (m Model) viewOptions() string {
	acc := m.accent()
	w := min(m.w-10, 56)
	menu := m.opts.menu
	body := "\n" + kit.S(m.th.Muted).Render("thread  ") + kit.S(m.th.Text).Bold(true).Render(clip(m.opts.thread.Title, w-16)) + "\n" +
		kit.Rule(w-4, "─", acc, 0) + "\n" + menu.Render(w-4, 1, m.th, true) + "\n" +
		kit.Hints([]kit.Hint{{"⏎", "choose"}, {"esc", "close"}}, m.th, acc, w-4)
	p := kit.Panel{Title: "⚙ thread options", Width: w, Height: lipgloss.Height(body) + 2, Stops: acc, Focused: true,
		Phase: m.panelPhase(), Fill: m.th.Overlay, Theme: m.th}
	return p.Render(body)
}
