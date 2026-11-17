package tui

import (
	"errors"
	"fmt"
	"image/color"

	tea "charm.land/bubbletea/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

// Update routes messages: global plumbing first, then the open overlay, then
// the current screen.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = max(msg.Width, 1), max(msg.Height, 1)
		m.compose.resize(m.w, m.h)
		return m, nil

	case kit.FrameMsg:
		if msg.Gen != m.gen {
			return m, nil // a superseded loop
		}
		m.step()
		if m.animating() {
			return m, kit.Frame(m.gen)
		}
		m.ticking = false
		return m, nil

	case clockMsg:
		if m.d.IdleTimeout > 0 && m.now().Sub(m.lastInput) > m.d.IdleTimeout {
			return m, tea.Sequence(tea.Printf("Disconnected after %s idle. Your posts are saved.", m.d.IdleTimeout), tea.Quit)
		}
		return m, clockTick()

	case eventsClosedMsg:
		return m, nil

	case eventMsg:
		cmds = append(cmds, waitEvent(m.d.Events), m.onEvent(board.Event(msg)))

	case homeMsg:
		if msg.err != nil {
			m.setFlash("couldn't load boards: " + msg.err.Error())
			break
		}
		m.boards, m.stats = msg.boards, msg.stats
		m.boardMenu.SetItems(boardItems(m.boards, m.now()))
		cmds = append(cmds, m.peekSelected())

	case threadsMsg:
		if msg.err != nil {
			break
		}
		m.peek[msg.boardID] = msg.threads
		if m.screen != scrBoard || msg.boardID != m.curBoard.ID {
			break
		}
		m.loading = false
		m.threads = msg.threads
		m.threadMenu.SetItems(threadItems(m.threads, m.now()))
		cmds = append(cmds, m.wantPreview())

	case previewMsg:
		m.previews[msg.threadID] = msg.posts

	case postsMsg:
		m.loading = false
		if msg.err != nil {
			m.setFlash("couldn't open thread")
			break
		}
		m.curThread, m.posts = msg.thread, msg.posts
		if msg.focus != 0 {
			for i, p := range m.posts {
				if p.ID == msg.focus {
					m.postCursor = i
				}
			}
		}
		m.postCursor = max(0, min(m.postCursor, len(m.posts)-1))

	case registeredMsg:
		if msg.err != nil {
			m.form.err = friendly(msg.err)
			break
		}
		first := m.acct == nil
		acct := msg.acct
		m.acct = &acct
		m.form = newIdentityForm(m.th, "", m.acct)
		if first {
			m.toasts.Push(m.now(), "Welcome, "+acct.Handle, "Your key is your account now.", kit.PaletteNamed(acct.Palette).Stops)
			m.goHome()
		} else {
			m.toasts.Push(m.now(), "Profile saved", "", kit.PaletteNamed(acct.Palette).Stops)
			m.setAccent(kit.PaletteNamed(acct.Palette).Stops)
		}
		cmds = append(cmds, loadHome(m.d))

	case postedMsg:
		if msg.err != nil {
			m.compose.err = friendly(msg.err)
			break
		}
		m.closeOverlay()
		m.compose.reset()
		m.loading = true
		cmds = append(cmds, loadPosts(m.d, msg.threadID, m.viewerID(), msg.postID), loadHome(m.d))
		if m.screen != scrThread {
			m.enterThread(board.Thread{ID: msg.threadID, BoardID: m.curBoard.ID})
		}
		m.toasts.Push(m.now(), "Posted", "", m.myStops())

	case kudosMsg:
		if msg.err != nil {
			m.setFlash(friendly(msg.err))
		}

	case tea.PasteMsg:
		m.lastInput = m.now()
		cmds = append(cmds, m.routeInput(msg))

	case tea.MouseWheelMsg:
		m.lastInput = m.now()
		cmds = append(cmds, m.onWheel(msg))

	case tea.MouseClickMsg:
		m.lastInput = m.now()
		cmds = append(cmds, m.onClick(msg))

	case tea.KeyPressMsg:
		m.lastInput = m.now()
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		cmds = append(cmds, m.routeInput(msg))
	}
	m.syncMenus()
	cmds = append(cmds, m.ensureTicking())
	return m, tea.Batch(cmds...)
}

// syncMenus keeps each menu's scroll window in step with its cursor, using
// the same geometry View will draw with, so mouse hit-testing agrees with
// what is on screen.
func (m *Model) syncMenus() {
	list, _ := m.homeLayout()
	m.boardMenu.Scroll(list.h - 3)
	list, _ = m.boardLayout()
	m.threadMenu.Scroll(list.h - 3)
}

// routeInput sends keys and pastes to the overlay or the current screen.
func (m *Model) routeInput(msg tea.Msg) tea.Cmd {
	if m.overlay != ovNone {
		return m.updateOverlay(msg)
	}
	switch m.screen {
	case scrSplash:
		if _, ok := msg.(tea.KeyPressMsg); ok {
			m.leaveSplash()
		}
		return nil
	case scrOnboard:
		return m.updateOnboard(msg)
	case scrProfile:
		return m.updateProfile(msg)
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if cmd, handled := m.globalKey(key); handled {
		return cmd
	}
	switch m.screen {
	case scrHome:
		return m.updateHome(key)
	case scrBoard:
		return m.updateBoard(key)
	case scrThread:
		return m.updateThread(key)
	}
	return nil
}

// globalKey handles bindings shared by every browsing screen.
func (m *Model) globalKey(k tea.KeyPressMsg) (tea.Cmd, bool) {
	switch k.String() {
	case "q":
		if m.screen == scrHome {
			return tea.Quit, true
		}
		return m.back(), true
	case "ctrl+k", "/", ":":
		m.openPalette()
		return nil, true
	case "?":
		m.openOverlay(ovHelp)
		return nil, true
	case "p":
		m.openProfile()
		return nil, true
	case "esc", "backspace", "left", "h":
		if m.screen == scrHome {
			return nil, false
		}
		return m.back(), true
	}
	return nil, false
}

// step advances every animation by one frame.
func (m *Model) step() {
	now := m.now()
	dt := now.Sub(m.lastFrame).Seconds()
	if dt <= 0 || dt > 0.25 {
		dt = 1.0 / kit.FrameRate
	}
	m.lastFrame = now
	m.frame++
	if m.accentT < 1 {
		m.accentT = min(1, m.accentT+dt/0.45)
	}
	switch {
	case m.overlay != ovNone && m.overlayT < 1:
		m.overlayT = min(1, m.overlayT+dt/0.18)
	case m.overlay == ovNone && m.overlayT > 0:
		m.overlayT = max(0, m.overlayT-dt/0.14)
	}
	m.boardMenu.Step(dt)
	m.threadMenu.Step(dt)
	m.toasts.Step(now, dt)
}

// onEvent reacts to another session's activity: refresh what is on screen
// and, if it came from someone else, say so.
func (m *Model) onEvent(ev board.Event) tea.Cmd {
	var cmds []tea.Cmd
	mine := m.acct != nil && ev.Actor == m.acct.Handle
	switch ev.Kind {
	case board.EventThread, board.EventReply:
		cmds = append(cmds, loadHome(m.d))
		if m.screen == scrBoard && ev.BoardID == m.curBoard.ID {
			cmds = append(cmds, loadThreads(m.d, m.curBoard.ID))
		}
		if m.screen == scrThread && ev.ThreadID == m.curThread.ID && !mine {
			cmds = append(cmds, loadPosts(m.d, m.curThread.ID, m.viewerID(), 0))
		}
		if !mine && m.screen != scrSplash && m.screen != scrOnboard {
			verb := "replied in"
			if ev.Kind == board.EventThread {
				verb = "started"
			}
			m.toasts.Push(m.now(), ev.Actor+" "+verb, ev.Title, m.stopsForBoard(ev.BoardID))
		}
	case board.EventKudos:
		if m.screen == scrThread && ev.ThreadID == m.curThread.ID {
			cmds = append(cmds, loadPosts(m.d, m.curThread.ID, m.viewerID(), 0))
		}
	case board.EventProfile:
		cmds = append(cmds, loadHome(m.d))
		if m.screen == scrThread {
			cmds = append(cmds, loadPosts(m.d, m.curThread.ID, m.viewerID(), 0))
		}
		if m.screen == scrBoard {
			cmds = append(cmds, loadThreads(m.d, m.curBoard.ID))
		}
	}
	return tea.Batch(cmds...)
}

func (m Model) stopsForBoard(id int64) []color.Color {
	for _, b := range m.boards {
		if b.ID == id {
			return kit.PaletteNamed(b.Palette).Stops
		}
	}
	return m.th.Brand.Stops
}

// ---- navigation --------------------------------------------------------

func (m *Model) leaveSplash() {
	if m.acct == nil {
		m.screen = scrOnboard
		m.setAccent(kit.Midnight.Brand.Stops)
		return
	}
	m.goHome()
}

func (m *Model) goHome() {
	m.screen = scrHome
	m.setAccent(m.th.Brand.Stops)
	m.boardMenu.Snap()
	m.touchFocus()
}

func (m *Model) enterBoard(b board.Board) tea.Cmd {
	m.curBoard = b
	m.screen = scrBoard
	m.threads = nil
	m.threadMenu = kit.NewMenu(nil)
	m.loading = true
	m.setAccent(kit.PaletteNamed(b.Palette).Stops)
	m.touchFocus()
	return loadThreads(m.d, b.ID)
}

func (m *Model) enterThread(t board.Thread) tea.Cmd {
	m.curThread = t
	m.screen = scrThread
	m.posts = nil
	m.postCursor, m.postTop = 0, 0
	m.loading = true
	m.touchFocus()
	return loadPosts(m.d, t.ID, m.viewerID(), 0)
}

func (m *Model) openProfile() {
	if m.acct == nil {
		return
	}
	m.form = newIdentityForm(m.th, "", m.acct)
	m.screen = scrProfile
	m.setAccent(kit.PaletteNamed(m.acct.Palette).Stops)
	m.touchFocus()
}

func (m *Model) back() tea.Cmd {
	switch m.screen {
	case scrThread:
		m.screen = scrBoard
		m.setAccent(kit.PaletteNamed(m.curBoard.Palette).Stops)
		m.touchFocus()
		return loadThreads(m.d, m.curBoard.ID)
	case scrBoard, scrProfile:
		m.goHome()
	}
	return nil
}

func (m *Model) setFlash(s string) {
	m.flash = s
	m.flashAt = m.now()
}

// friendly turns errors into short sentences for the UI.
func friendly(err error) string {
	for _, known := range []error{board.ErrHandleInvalid, board.ErrHandleTaken, board.ErrEmptyTitle,
		board.ErrEmptyBody, board.ErrTooLong, board.ErrNoAccount, board.ErrNotFound} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return fmt.Sprintf("something went wrong (%s)", "try again")
}
