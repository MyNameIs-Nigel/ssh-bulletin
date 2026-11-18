package tui

import (
	"errors"
	"fmt"
	"image/color"
	"maps"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

// Update routes messages: global plumbing first, then the open overlay, then
// the current screen.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if mm, ok := msg.(tea.MouseMotionMsg); ok {
		m.lastInput = m.now()
		if !m.onMotion(mm) {
			// Nothing lit or unlit: the last frame still stands.
			m.reuse = true
			return m, m.ensureTicking()
		}
	}
	m.reuse = false
	m.stamp++
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = max(msg.Width, 1), max(msg.Height, 1)
		m.compose.resize(m.w, m.h)
		m.syncMenus()
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
		cmds = append(cmds, clockTick(), m.refreshRole(), m.loadModeration(), m.refreshFriends())

	case eventsClosedMsg:
		return m, nil

	case blinkMsg:
		if msg.gen == m.blinkGen && !m.emptyAt.IsZero() {
			cmds = append(cmds, m.blinkTick())
		}

	case eventMsg:
		cmds = append(cmds, waitEvent(m.d.Events), m.onEvent(board.Event(msg)))

	case homeMsg:
		if msg.version != m.contentVersion {
			break
		}
		if msg.err != nil {
			m.setFlash("couldn't load boards: " + msg.err.Error())
			break
		}
		m.boards, m.stats = msg.boards, msg.stats
		m.boardMenu.SetItems(boardItems(m.boards, m.now()))
		cmds = append(cmds, m.peekSelected())

	case threadsMsg:
		if msg.version != m.contentVersion {
			break
		}
		if msg.err != nil {
			break
		}
		m.peek[msg.boardID] = msg.threads
		if m.screen != scrBoard || msg.boardID != m.curBoard.ID {
			break
		}
		m.loading = false
		// The menu keeps its cursor by index, but a pin or new reply reorders
		// the list: follow the thread the cursor was on.
		var keep int64
		if i := m.threadMenu.Selected(); i >= 0 && i < len(m.threads) {
			keep = m.threads[i].ID
		}
		m.threads = msg.threads
		m.threadMenu.SetItems(m.threadItems())
		for i, t := range m.threads {
			if keep != 0 && t.ID == keep {
				m.threadMenu.Select(i)
			}
		}
		cmds = append(cmds, m.wantPreview())
		switch {
		case len(m.threads) > 0:
			m.emptyAt = time.Time{}
		case m.emptyAt.IsZero():
			// The cat blinks for the first time blinkFirst after arriving.
			m.emptyAt = m.now()
			m.blinkGen++
			cmds = append(cmds, m.blinkTick())
		}

	case previewMsg:
		if msg.version == m.contentVersion {
			m.previews[msg.threadID] = msg.posts
			m.mergeRefs(msg.refs)
		}

	case postsMsg:
		if msg.version != m.contentVersion || m.screen != scrThread || msg.threadID != m.curThread.ID {
			break
		}
		m.loading = false
		if errors.Is(msg.err, board.ErrNotFound) {
			m.posts = nil
			m.closeOverlay()
			m.compose.reset()
			m.setFlash("This thread is unavailable")
			cmds = append(cmds, m.back())
			break
		}
		if msg.err != nil {
			m.setFlash("couldn't open thread")
			break
		}
		m.mergeRefs(msg.refs)
		m.curThread, m.posts = msg.thread, msg.posts
		if msg.focus != 0 {
			for i, p := range m.posts {
				if p.ID == msg.focus {
					m.postCursor = i
				}
			}
		}
		m.postCursor = max(0, min(m.postCursor, len(m.posts)-1))
		if msg.focus != 0 {
			m.followCursor()
		}

	case directoryMsg:
		if m.screen == scrDirectory && msg.request == m.socialRequest {
			m.directory.loading = false
			m.directory.entries, m.directory.more = msg.entries, msg.more
			if msg.err != nil {
				m.directory.err = "Couldn't load usernames. Try searching again."
			}
			m.syncDirectoryItems()
		}

	case userProfileMsg:
		if m.screen == scrUserProfile && msg.request == m.socialRequest {
			if msg.err != nil {
				m.userProfile.err = friendly(msg.err)
				break
			}
			if m.userProfile.data.Palette != msg.profile.Palette {
				m.setAccent(kit.PaletteNamed(msg.profile.Palette).Stops)
			}
			m.mergeRefs(msg.refs)
			m.userProfile.data = msg.profile
			m.userProfile.recent = min(m.userProfile.recent, max(0, len(msg.profile.Recent)-1))
		}

	case friendsMsg:
		if msg.request == m.friendsRequest && msg.err == nil && !maps.Equal(m.friends, msg.ids) {
			m.friends = msg.ids
			m.threadMenu.SetItems(m.threadItems())
			m.syncDirectoryItems()
			if m.screen == scrDirectory && m.directory.friendsOnly {
				cmds = append(cmds, m.searchDirectory(false))
			}
		}

	case friendSavedMsg:
		m.userProfile.saving = false
		if msg.err != nil {
			m.userProfile.err = friendly(msg.err)
			break
		}
		friends := make(map[int64]bool, len(m.friends)+1)
		maps.Copy(friends, m.friends)
		if msg.on {
			friends[m.userProfile.id] = true
		} else {
			delete(friends, m.userProfile.id)
		}
		m.friends = friends
		m.threadMenu.SetItems(m.threadItems())
		m.syncDirectoryItems()
		cmds = append(cmds, m.refreshFriends())

	case registeredMsg:
		m.form.busy = false
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
			cmds = append(cmds, m.refreshFriends())
		} else {
			m.toasts.Push(m.now(), "Profile saved", "", kit.PaletteNamed(acct.Palette).Stops)
			m.setAccent(kit.PaletteNamed(acct.Palette).Stops)
		}
		cmds = append(cmds, m.loadHome())

	case postedMsg:
		m.compose.busy = false
		if msg.err != nil {
			m.compose.err = friendly(msg.err)
			break
		}
		m.closeOverlay()
		m.compose.reset()
		if m.screen != scrThread {
			cmds = append(cmds, m.enterThread(board.Thread{ID: msg.threadID, BoardID: m.curBoard.ID}, msg.postID))
		} else {
			cmds = append(cmds, m.loadPosts(msg.threadID, msg.postID))
		}
		cmds = append(cmds, m.loadHome())
		m.toasts.Push(m.now(), "Posted", "", m.myStops())

	case kudosMsg:
		if msg.err != nil {
			m.setFlash(friendly(msg.err))
		}

	case deleteResultMsg:
		cmds = append(cmds, m.receiveDelete(msg))

	case pinResultMsg:
		cmds = append(cmds, m.receivePin(msg))

	case chatLoadedMsg:
		cmds = append(cmds, m.receiveChat(msg))

	case chatSentMsg:
		cmds = append(cmds, m.receiveChatSent(msg))

	case chatWithdrawnMsg:
		cmds = append(cmds, m.receiveChatWithdrawn(msg))

	case moderationPageMsg:
		cmds = append(cmds, m.receiveModerationPage(msg))

	case moderationResultMsg:
		if m.overlay != ovModeration {
			break
		}
		m.modForm.busy = false
		if errors.Is(msg.err, board.ErrForbidden) {
			m.leaveModeration()
			cmds = append(cmds, m.refreshRole())
			m.setFlash(friendly(msg.err))
		} else if msg.err != nil {
			m.modForm.err = friendly(msg.err)
		} else {
			m.closeOverlay()
			m.mod.detail = nil
			m.toasts.Push(m.now(), msg.message, "", m.myStops())
			if m.screen == scrThread && m.modForm.action == board.Removed {
				cmds = append(cmds, m.onEvent(board.Event{Kind: board.EventModeration, ThreadID: m.curThread.ID, PostID: m.modForm.postID}))
			} else {
				cmds = append(cmds, m.loadModeration())
			}
		}

	case roleMsg:
		if m.acct != nil && msg.err == nil && msg.request == m.roleRequest {
			a := *m.acct
			wasStaff := a.Role.CanModerate()
			a.Role = msg.role
			m.acct = &a
			if wasStaff != m.canModerate() && m.overlay == ovOptions && !m.opts.busy {
				m.closeOverlay()
			}
			if wasStaff && !m.canModerate() {
				m.leaveModeration()
			}
		}

	case tea.PasteMsg:
		m.lastInput = m.now()
		cmds = append(cmds, m.routeInput(msg))

	case tea.MouseWheelMsg:
		m.lastInput = m.now()
		m.hover = "" // the content moves under the pointer
		cmds = append(cmds, m.onWheel(msg))

	case tea.MouseClickMsg:
		m.lastInput = m.now()
		cmds = append(cmds, m.onClick(msg))

	case tea.KeyPressMsg:
		m.lastInput = m.now()
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		// The keyboard takes over: the pointer's highlight would only
		// compete with the keyboard's until the mouse moves again.
		screen, overlay := m.screen, m.overlay
		cmds = append(cmds, m.routeInput(msg))
		if m.screen != screen || m.overlay != overlay {
			m.hover = ""
		}
	}
	m.syncMenus()
	m.trackLit()
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
	m.directory.menu.Scroll(m.socialRect().h - 2 - directoryHeaderRows)
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
	case scrDirectory:
		return m.updateDirectory(msg)
	case scrChat:
		// The input has the keyboard: q, / and friends are text here.
		return m.updateChat(msg)
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if m.screen == scrUserProfile {
		return m.updateUserProfile(key)
	}
	if m.screen == scrModeration && m.mod.detail != nil {
		return m.updateModeration(key)
	}
	if m.screen == scrThread && m.linkSel >= 0 && (key.String() == "esc" || key.String() == "backspace") {
		m.linkSel = -1 // put the link down before leaving the thread
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
	case scrModeration:
		return m.updateModeration(key)
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
	case "m":
		return m.openModeration(), true
	case "?":
		m.openOverlay(ovHelp)
		return nil, true
	case "p":
		m.openProfile()
		return nil, true
	case "u":
		if m.screen == scrModeration {
			return nil, false // restore in the moderation queue
		}
		return m.openDirectory(), true
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
		cmds = append(cmds, m.loadHome())
		if m.screen == scrBoard && ev.BoardID == m.curBoard.ID {
			cmds = append(cmds, m.loadThreads(m.curBoard.ID))
		}
		if m.screen == scrThread && ev.ThreadID == m.curThread.ID && !mine {
			cmds = append(cmds, m.loadPosts(m.curThread.ID, 0))
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
			cmds = append(cmds, m.loadPosts(m.curThread.ID, 0))
		}
	case board.EventRole:
		if ev.AccountID == m.viewerID() {
			cmds = append(cmds, m.refreshRole())
		}
		if m.overlay == ovPalette {
			m.closeOverlay()
		}
		cmds = append(cmds, m.loadModeration(), m.loadHome())
		if m.screen == scrThread {
			cmds = append(cmds, m.loadPosts(m.curThread.ID, 0))
		}
		if m.screen == scrBoard {
			cmds = append(cmds, m.loadThreads(m.curBoard.ID))
		}
		if m.screen == scrBoard || m.screen == scrChat {
			cmds = append(cmds, m.reloadChat())
		}
	case board.EventReview:
		cmds = append(cmds, m.loadModeration())
	case board.EventChat:
		// Chat never toasts: it would bury every other notice. The board
		// section and chat screen show it as it arrives.
		if ev.BoardID == m.chat.boardID && (m.screen == scrBoard || m.screen == scrChat) {
			cmds = append(cmds, m.refreshChat())
		}
	case board.EventChatRemoved:
		if m.overlay == ovOptions && m.opts.chat.ID == ev.MessageID && !m.opts.busy {
			m.closeOverlay()
		}
		if ev.BoardID == m.chat.boardID {
			m.dropChat(ev.MessageID)
			cmds = append(cmds, m.clampChat())
		}
	case board.EventModeration:
		if m.overlay == ovOptions && m.opts.thread.ID == ev.ThreadID && !m.opts.busy {
			m.closeOverlay()
		}
		m.contentVersion++
		delete(m.previews, ev.ThreadID)
		clear(m.peek)
		cmds = append(cmds, m.loadModeration(), m.loadHome())
		if m.screen == scrThread {
			cmds = append(cmds, m.loadPosts(m.curThread.ID, 0))
		}
		if m.screen == scrBoard {
			cmds = append(cmds, m.loadThreads(m.curBoard.ID))
		}
	case board.EventProfile:
		cmds = append(cmds, m.loadHome())
		if m.screen == scrThread {
			cmds = append(cmds, m.loadPosts(m.curThread.ID, 0))
		}
		if m.screen == scrBoard {
			cmds = append(cmds, m.loadThreads(m.curBoard.ID))
		}
		if m.screen == scrBoard || m.screen == scrChat {
			cmds = append(cmds, m.reloadChat())
		}
	}
	if m.screen == scrUserProfile {
		switch ev.Kind {
		case board.EventProfile, board.EventRole, board.EventThread, board.EventReply:
			if ev.AccountID == m.userProfile.id {
				cmds = append(cmds, m.loadUserProfile())
			}
		case board.EventModeration:
			cmds = append(cmds, m.loadUserProfile())
		}
	} else if m.screen == scrDirectory && (ev.Kind == board.EventProfile || ev.Kind == board.EventRole) {
		cmds = append(cmds, m.searchDirectory(false))
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
	if m.screen == scrModeration {
		m.mod = moderationState{}
		m.modRequest++
	}
	m.screen = scrHome
	m.setAccent(m.th.Brand.Stops)
	m.boardMenu.Snap()
	m.touchFocus()
}

func (m *Model) enterBoard(b board.Board) tea.Cmd {
	m.curBoard = b
	m.screen = scrBoard
	m.chatFocus = false
	m.emptyAt = time.Time{}
	m.threads = nil
	m.threadMenu = kit.NewMenu(nil)
	m.loading = true
	m.setAccent(kit.PaletteNamed(b.Palette).Stops)
	m.touchFocus()
	return tea.Batch(m.loadThreads(b.ID), m.resetChat(b.ID))
}

func (m *Model) enterThread(t board.Thread, focus int64) tea.Cmd {
	m.curThread = t
	m.screen = scrThread
	m.posts = nil
	m.postCursor, m.postTop = 0, 0
	m.loading = true
	m.touchFocus()
	return m.loadPosts(t.ID, focus)
}

func (m *Model) openProfile() {
	if m.acct == nil || m.screen == scrProfile {
		return
	}
	m.form = newIdentityForm(m.th, "", m.acct)
	m.editFrom = m.screen
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
		return tea.Batch(m.loadThreads(m.curBoard.ID), m.refreshChat())
	case scrChat:
		return m.leaveChat()
	case scrProfile:
		return m.returnFromSocial(m.editFrom)
	case scrBoard, scrModeration:
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
		board.ErrEmptyBody, board.ErrTooLong, board.ErrNoAccount, board.ErrNotFound,
		board.ErrForbidden, board.ErrInvalidFingerprint, board.ErrKeyMismatch, board.ErrOwnerRole,
		board.ErrReasonRequired, board.ErrAlreadyReported, board.ErrReviewChanged, board.ErrPostLimit,
		board.ErrNewcomerLimit, board.ErrReportLimit, board.ErrPinLimit, board.ErrFlairInvalid, board.ErrSelfFriend, board.ErrChatLimit} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return fmt.Sprintf("something went wrong (%s)", "try again")
}
