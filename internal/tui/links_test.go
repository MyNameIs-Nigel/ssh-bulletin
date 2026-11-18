package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
)

// lobbyThread opens the seeded first thread, whose opening post links two
// users, two boards and an external site (in that order), and whose last
// reply links the thread's second post.
func lobbyThread(t *testing.T, w, h int) (Model, *board.Service) {
	t.Helper()
	svc, _ := testService(t)
	acct, _, _ := svc.Lookup(context.Background(), "SHA256:demo-0")
	m := New(Deps{Service: svc, Fingerprint: acct.Fingerprint, Account: &acct, Width: w, Height: h,
		Now: func() time.Time { return fixedNow }, NoSplash: true})
	m = drive(t, m, tea.WindowSizeMsg{Width: w, Height: h}, key("enter"), key("enter"))
	if m.screen != scrThread || len(m.posts) != 3 {
		t.Fatalf("screen=%v posts=%d", m.screen, len(m.posts))
	}
	return m, svc
}

// at renders m and returns where hot spot key was drawn.
func at(t *testing.T, m Model, key string) (x, y int) {
	t.Helper()
	m.View()
	x, y, ok := m.ui.hot.Find(key)
	if !ok {
		t.Fatalf("%q was not drawn; drawn: %v", key, m.ui.hot.Keys())
	}
	return x, y
}

func clickOn(t *testing.T, m Model, key string) Model {
	t.Helper()
	x, y := at(t, m, key)
	return drive(t, m, tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
}

func shift(k string) tea.Msg {
	if k == "tab" {
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	}
	return key(k)
}

func TestPostsRenderMarkupAndLinks(t *testing.T) {
	m, _ := lobbyThread(t, 120, 40)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"your own palette —", "• say hi to @ferrofluid", "charm.land↗", "#1/2 hello from a 24-bit terminal"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q in:\n%s", want, view)
		}
	}
	for _, raw := range []string{"**", "https://"} {
		if strings.Contains(view, raw) {
			t.Errorf("markup %q leaked into the view", raw)
		}
	}
	links := m.bodyLinks(m.posts[0].Body)
	if len(links) != 5 || links[0].Handle != "ferrofluid" || links[2].Slug != "arcade" || links[4].Ref.URL != "https://charm.land" {
		t.Fatalf("links: %+v", links)
	}
	if strings.Contains(m.View().Content, "bulletin-hot:") {
		t.Fatal("a hot-spot mark reached the terminal")
	}
}

func TestUnresolvedReferencesStayPlainText(t *testing.T) {
	m, svc := lobbyThread(t, 120, 40)
	if _, err := svc.Reply(context.Background(), *m.acct, m.curThread, "ping @nobody in #nowhere about #4242"); err != nil {
		t.Fatal(err)
	}
	m = drive(t, m, m.loadPosts(m.curThread.ID, 0)())
	last := m.posts[len(m.posts)-1]
	if n := len(m.bodyLinks(last.Body)); n != 0 {
		t.Fatalf("%d links to things that do not exist", n)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "ping @nobody in #nowhere about #4242") {
		t.Fatal("unresolved references were not shown as typed")
	}
}

func TestKeyboardWalksAndFollowsLinks(t *testing.T) {
	m, _ := lobbyThread(t, 120, 40)
	m = drive(t, m, key("tab"))
	if m.focusKey() != linkKey(postScope(m.posts[0].ID), 0) {
		t.Fatalf("focus = %q", m.focusKey())
	}
	if hints := m.hints(); hints[0][1] != "open link" {
		t.Fatalf("hints while a link is focused: %v", hints)
	}
	user := drive(t, m, key("enter"))
	if user.screen != scrUserProfile || user.userProfile.data.Handle != "ferrofluid" {
		t.Fatalf("following @ferrofluid: screen=%v profile=%q", user.screen, user.userProfile.data.Handle)
	}

	brd := drive(t, m, key("tab"), key("tab"), key("enter"))
	if brd.screen != scrBoard || brd.curBoard.Slug != "arcade" {
		t.Fatalf("following #arcade: screen=%v board=%q", brd.screen, brd.curBoard.Slug)
	}

	// shift+tab from the first link wraps to the last: the external one.
	ext := drive(t, m, shift("tab"))
	if ext.linkSel != 4 {
		t.Fatalf("shift+tab wrapped to %d", ext.linkSel)
	}
	ext = drive(t, ext, key("enter"))
	if ext.overlay != ovLink || ext.link.url != "https://charm.land" || ext.link.host != "charm.land" {
		t.Fatalf("external link went straight through: overlay=%v link=%+v", ext.overlay, ext.link)
	}
	ext.overlayT = 1 // the open animation has played
	if !strings.Contains(ansi.Strip(ext.View().Content), "This link leaves the bulletin") {
		t.Fatal("no warning shown")
	}
	next, cmd := ext.Update(key("c"))
	ext = next.(Model)
	if cmd == nil || !ext.link.copied {
		t.Fatal("c did not copy the link")
	}
	ext = drive(t, ext, key("esc"))
	if ext.overlay != ovNone || ext.screen != scrThread || ext.linkSel != 4 {
		t.Fatalf("closing the warning: overlay=%v screen=%v link=%d", ext.overlay, ext.screen, ext.linkSel)
	}
	// esc puts the link down before it leaves the thread; moving does too.
	if ext = drive(t, ext, key("esc")); ext.screen != scrThread || ext.linkSel != -1 {
		t.Fatalf("esc with a link focused: screen=%v link=%d", ext.screen, ext.linkSel)
	}
	if moved := drive(t, m, key("down")); moved.linkSel != -1 {
		t.Fatal("moving to another post kept a link of the last one focused")
	}
	// enter without a focused link still replies.
	if r := drive(t, m, key("esc"), key("enter")); r.overlay != ovCompose {
		t.Fatalf("enter without a link: overlay=%v", r.overlay)
	}
}

func TestPostLinksJumpToThePost(t *testing.T) {
	m, _ := lobbyThread(t, 120, 40)
	m = drive(t, m, key("down"), key("down"), key("tab"), key("enter"))
	if m.screen != scrThread || m.postCursor != 1 {
		t.Fatalf("#1/2 landed on screen=%v cursor=%d", m.screen, m.postCursor)
	}
}

func TestHoverLightsAndClickFollows(t *testing.T) {
	m, _ := lobbyThread(t, 120, 40)
	key1 := linkKey(postScope(m.posts[0].ID), 1) // @oldmoon
	x, y := at(t, m, key1)
	m = drive(t, m, tea.MouseMotionMsg{X: x, Y: y})
	if m.hover != key1 || !m.lit(key1) || !m.animating() {
		t.Fatalf("hover=%q lit=%v animating=%v", m.hover, m.lit(key1), m.animating())
	}
	lit := m.View().Content

	// Moving within the same link changes nothing: the frame is reused.
	m = drive(t, m, tea.MouseMotionMsg{X: x + 1, Y: y})
	if !m.reuse || m.View().Content != lit {
		t.Fatal("motion inside one hot spot drew a new frame")
	}
	// The flow settles once input stops.
	idle := m
	idle.lastInput = fixedNow.Add(-hoverFlow - time.Second)
	idle.accentT, idle.focusAt = 1, fixedNow.Add(-time.Hour)
	if idle.animating() {
		t.Fatal("a resting hover keeps animating")
	}

	m = drive(t, m, tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
	if m.screen != scrUserProfile || m.userProfile.data.Handle != "oldmoon" {
		t.Fatalf("click: screen=%v profile=%q", m.screen, m.userProfile.data.Handle)
	}
	if m.hover != "" {
		t.Fatal("hover survived the screen change")
	}
}

func TestMotionOffEverythingUnlights(t *testing.T) {
	m, _ := lobbyThread(t, 120, 40)
	x, y := at(t, m, linkKey(postScope(m.posts[0].ID), 0))
	m = drive(t, m, tea.MouseMotionMsg{X: x, Y: y}, tea.MouseMotionMsg{X: 0, Y: m.h - 2})
	if m.hover != "" || m.reuse {
		t.Fatalf("hover=%q reuse=%v", m.hover, m.reuse)
	}
}

func TestFooterHintsBreadcrumbsAndNamesAreClickable(t *testing.T) {
	m, _ := lobbyThread(t, 120, 40)
	if c := clickOn(t, m, "foot:hint:r"); c.overlay != ovCompose {
		t.Fatalf("clicking the reply hint: overlay=%v", c.overlay)
	}
	if c := clickOn(t, m, "crumb:0:boards"); c.screen != scrHome {
		t.Fatalf("clicking the boards crumb: screen=%v", c.screen)
	}
	if c := clickOn(t, m, "crumb:1:"+m.curBoard.Title); c.screen != scrBoard {
		t.Fatalf("clicking the board crumb: screen=%v", c.screen)
	}
	if c := clickOn(t, m, "me:author"); c.screen != scrUserProfile || c.userProfile.id != m.acct.ID {
		t.Fatal("clicking your own name did not open your profile")
	}
	if c := clickOn(t, m, postScope(m.posts[1].ID)+":author"); c.screen != scrUserProfile || c.userProfile.id != m.posts[1].AuthorID {
		t.Fatal("clicking an author did not open their profile")
	}
}

func TestListRowsHoverWithoutMovingAndClickToOpen(t *testing.T) {
	svc, _ := testService(t)
	acct, _, _ := svc.Lookup(context.Background(), "SHA256:demo-0")
	m := drive(t, New(Deps{Service: svc, Account: &acct, Width: 120, Height: 40, Now: func() time.Time { return fixedNow }, NoSplash: true}),
		tea.WindowSizeMsg{Width: 120, Height: 40})
	x, y := at(t, m, "board:2")
	m = drive(t, m, tea.MouseMotionMsg{X: x + 3, Y: y})
	if m.hover != "board:2" || m.boardMenu.Cursor != 0 {
		t.Fatalf("hover=%q cursor=%d", m.hover, m.boardMenu.Cursor)
	}
	m = drive(t, m, tea.MouseClickMsg{Button: tea.MouseLeft, X: x + 3, Y: y})
	if m.screen != scrHome || m.boardMenu.Cursor != 2 {
		t.Fatal("the first click should select the row")
	}
	m = clickOn(t, m, "board:2")
	if m.screen != scrBoard || m.curBoard.ID != m.boards[2].ID {
		t.Fatal("clicking the selected row should open it")
	}
}

// Under a modal only the modal is interactive.
func TestModalBlocksTheFrameBehindIt(t *testing.T) {
	m, _ := lobbyThread(t, 120, 40)
	m.openLink("https://charm.land")
	m.View()
	if keys := m.ui.hot.Keys(); len(keys) != 0 {
		t.Fatalf("before the modal is drawn, %v are clickable behind it", keys)
	}
	m.overlayT = 1
	m.View()
	for _, k := range m.ui.hot.Keys() {
		if !strings.HasPrefix(k, "modal:") {
			t.Fatalf("%q is clickable behind the modal", k)
		}
	}
	if c := clickOn(t, m, "modal:hint:esc"); c.overlay != ovNone {
		t.Fatal("the modal's own buttons do not work")
	}
}

func TestLookalikeHostsAreFlagged(t *testing.T) {
	m, _ := lobbyThread(t, 120, 40)
	m.openLink("https://exаmple.com/login") // Cyrillic а
	if !m.link.odd {
		t.Fatal("a mixed-alphabet host was not flagged")
	}
	m.openLink("https://good@evil.example/x")
	if m.link.host != "evil.example" {
		t.Fatalf("host shown as %q", m.link.host)
	}
}

func TestChatTabWalksMessageLinks(t *testing.T) {
	svc, _ := testService(t)
	acct, _, _ := svc.Lookup(context.Background(), "SHA256:demo-0")
	m := New(Deps{Service: svc, Account: &acct, Width: 120, Height: 40, Now: func() time.Time { return fixedNow }, NoSplash: true})
	// Arcade Talk's chat: oldmoon's message links @quietfox and #2.
	m = drive(t, m, tea.WindowSizeMsg{Width: 120, Height: 40}, key("down"), key("enter"), key("tab"), key("enter"), key("tab"))
	if m.screen != scrChat || m.chat.sel < 0 {
		t.Fatalf("screen=%v sel=%d", m.screen, m.chat.sel)
	}
	m = drive(t, m, key("up"))
	if got := m.chat.msgs[m.chat.sel].AuthorHandle; got != "oldmoon" {
		t.Fatalf("selected %q", got)
	}
	m = drive(t, m, key("tab"))
	if m.chat.link != 0 || m.focusKey() == "" {
		t.Fatalf("link=%d focus=%q", m.chat.link, m.focusKey())
	}
	thread := drive(t, m, key("tab"), key("enter"))
	if thread.screen != scrThread || thread.curThread.ID != 2 {
		t.Fatalf("#2 from chat: screen=%v thread=%d", thread.screen, thread.curThread.ID)
	}
	back := drive(t, m, key("tab"), key("tab"))
	if back.chat.sel != -1 || back.chat.link != -1 {
		t.Fatal("tab past the last link should return to typing")
	}
}

func TestSplashCreditOpensTheWarning(t *testing.T) {
	svc, _ := testService(t)
	m := New(Deps{Service: svc, Width: 120, Height: 40, Now: func() time.Time { return fixedNow }})
	m = drive(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = clickOn(t, m, creditKey)
	if m.screen != scrSplash || m.overlay != ovLink || m.link.url != bridgeSource {
		t.Fatalf("screen=%v overlay=%v url=%q", m.screen, m.overlay, m.link.url)
	}
}
