package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestComposeDraftsStayWithTheirThreads(t *testing.T) {
	m, svc := lobbyThread(t, 80, 24)
	first := m.curThread
	secondID, err := svc.CreateThread(m.d.Context, *m.acct, first.BoardID, "Another thread", "Second opening")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Thread(m.d.Context, secondID)
	if err != nil {
		t.Fatal(err)
	}
	m = drive(t, m, key("r"))
	m.compose.body.SetValue("For the first thread")
	m.overlayT = 1
	m = clickOn(t, m, "compose:hint:esc")
	if m.overlay != ovNone || m.flash != "Draft saved" {
		t.Fatalf("save and close: overlay=%v, feedback=%q", m.overlay, m.flash)
	}
	m = drive(t, m, m.enterThread(second, 0)(), key("r"))
	if m.compose.body.Value() != "" {
		t.Fatal("the second thread inherited the first draft")
	}
	m.compose.body.SetValue("For the second thread")
	m = drive(t, m, key("esc"))
	m = drive(t, m, m.enterThread(first, 0)(), key("r"))
	if got := m.compose.body.Value(); got != "For the first thread" {
		t.Fatalf("restored first draft = %q", got)
	}
	m.curThread = second // Simulate the underlying selection changing before submit.
	if text := ansi.Strip(m.viewCompose()); !strings.Contains(text, first.Title) || strings.Contains(text, second.Title) {
		t.Fatalf("composer shows the wrong destination:\n%s", text)
	}
	m = drive(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	posts, err := svc.Posts(m.d.Context, first.ID, m.viewerID())
	if err != nil || len(posts) != 4 || posts[3].Body != "For the first thread" {
		t.Fatalf("first thread posts = %+v, %v", posts, err)
	}
	posts, err = svc.Posts(m.d.Context, second.ID, m.viewerID())
	if err != nil || len(posts) != 1 {
		t.Fatalf("second thread posts = %+v, %v", posts, err)
	}
	m = drive(t, m, key("r"))
	if got := m.compose.body.Value(); got != "" {
		t.Fatalf("posted draft reopened with %q", got)
	}
	m = drive(t, m, key("esc"))
	m = drive(t, m, m.enterThread(second, 0)(), key("r"))
	if got := m.compose.body.Value(); got != "For the second thread" {
		t.Fatalf("restored second draft = %q", got)
	}
	m.overlayT = 1
	m = clickOn(t, m, "compose:hint:ctrl+x")
	m = drive(t, m, key("esc"))
	if m.overlay != ovCompose || m.compose.body.Value() != "For the second thread" {
		t.Fatal("canceling discard lost the saved draft")
	}
	m = clickOn(t, m, "compose:hint:ctrl+x")
	m = clickOn(t, m, "compose:hint:y")
	m = drive(t, m, key("r"))
	if got := m.compose.body.Value(); got != "" {
		t.Fatalf("discarded draft reopened with %q", got)
	}
}

func TestNewThreadDraftUsesSelectedBoard(t *testing.T) {
	m, svc := lobbyThread(t, 80, 24)
	m = drive(t, m, key("esc"), key("esc"))
	first, second := m.boards[0], m.boards[1]
	m.boardMenu.Select(0)
	m = drive(t, m, key("n"))
	m.compose.title.SetValue("Draft title")
	m.compose.body.SetValue("Draft body")
	m = drive(t, m, key("esc"), key("down"), key("n"))
	if m.compose.destination.boardID != second.ID || m.compose.title.Value() != "" || m.compose.body.Value() != "" {
		t.Fatalf("opening %q used an unrelated board or draft", second.Title)
	}
	m = drive(t, m, key("esc"), key("up"), key("n"))
	if m.compose.destination.boardID != first.ID || m.compose.title.Value() != "Draft title" || m.compose.body.Value() != "Draft body" {
		t.Fatalf("didn't restore the draft for %q", first.Title)
	}
	m.curBoard = second
	m = drive(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	thread, err := svc.Thread(m.d.Context, m.curThread.ID)
	if err != nil || thread.BoardID != first.ID || thread.Title != "Draft title" || m.curBoard.ID != first.ID {
		t.Fatalf("posted thread = %+v, view board = %d, error = %v", thread, m.curBoard.ID, err)
	}
}

func TestFirstPostComposerShowsPublicCue(t *testing.T) {
	f := newChatFixture(t)
	m := drive(t, modelFor(f.svc, f.alice, 80, 24), key("n"))
	m.overlayT = 1
	if text := screenText(m); !strings.Contains(text, "Posts are public to every arcade player.") {
		t.Fatalf("first post cue:\n%s", text)
	}
	m = drive(t, m, tea.PasteMsg{Content: "Hello"}, key("tab"), tea.PasteMsg{Content: "First post"}, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.acct.PostCount != 1 || m.overlay != ovNone {
		t.Fatalf("first post result: count=%d, overlay=%v", m.acct.PostCount, m.overlay)
	}
}
