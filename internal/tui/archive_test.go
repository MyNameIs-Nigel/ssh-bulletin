package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSearchOpensMatchingReplyAndReturnsToResults(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	id, err := f.svc.CreateThread(ctx, f.owner, f.board.ID, "Solar panels", "A thread for workshop notes")
	if err != nil {
		t.Fatal(err)
	}
	th, err := f.svc.Thread(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := f.svc.Reply(ctx, f.bob, th, "The rare polysilicon cells arrived")
	if err != nil {
		t.Fatal(err)
	}
	m := drive(t, modelFor(f.svc, f.alice, 80, 24), key("s"), tea.PasteMsg{Content: "polysilicon cells"}, key("enter"))
	if m.screen != scrSearch || len(m.search.results) != 1 || !strings.Contains(screenText(m), "Solar panels") || !strings.Contains(screenText(m), "polysilicon cells") {
		t.Fatalf("search results:\n%s", screenText(m))
	}
	m = clickOn(t, m, "search:0")
	if m.screen != scrThread || m.curThread.ID != id || m.posts[m.postCursor].ID != pid {
		t.Fatalf("opened search target: screen=%v, thread=%d, cursor=%d", m.screen, m.curThread.ID, m.postCursor)
	}
	m = drive(t, m, key("esc"))
	if m.screen != scrSearch || m.search.query != "polysilicon cells" || len(m.search.results) != 1 {
		t.Fatalf("return to search: %+v", m.search)
	}
}

func TestEditOpeningPostFromOptionsKeepsReplyDraft(t *testing.T) {
	f := newChatFixture(t)
	if _, err := f.svc.CreateThread(context.Background(), f.alice, f.board.ID, "Original thread", "Original opening"); err != nil {
		t.Fatal(err)
	}
	m := drive(t, modelFor(f.svc, f.alice, 80, 24), key("enter"), key("enter"), key("r"), tea.PasteMsg{Content: "An unrelated draft"}, key("esc"), key("o"))
	m.overlayT = 1
	editItem := -1
	for i, item := range m.opts.menu.Items {
		if item.Title == "Edit opening post" {
			editItem = i
			break
		}
	}
	if editItem < 0 {
		t.Fatal("author didn't get an Edit action")
	}
	m = clickOn(t, m, fmt.Sprintf("option:%d", editItem))
	if m.compose.edit.ID != m.posts[0].ID || m.compose.title.Value() != "Original thread" || m.compose.body.Value() != "Original opening" {
		t.Fatalf("edit form did not load the current author content: %+v", m.compose.edit)
	}
	m.compose.title.SetValue("Corrected thread")
	m.compose.body.SetValue("Corrected opening")
	m = drive(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m.overlayT = 0
	if text := screenText(m); m.overlay != ovNone || !strings.Contains(text, "Corrected thread") || !strings.Contains(text, "Corrected opening") || !strings.Contains(text, "edited") {
		t.Fatalf("edit result:\n%s", text)
	}
	m = drive(t, m, key("r"))
	if m.compose.body.Value() != "An unrelated draft" {
		t.Fatalf("edit overwrote the reply draft: %q", m.compose.body.Value())
	}
}

func TestDraftRestoresAfterReconnectWithoutEsc(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	if _, err := f.svc.CreateThread(ctx, f.bob, f.board.ID, "A destination", "Opening text"); err != nil {
		t.Fatal(err)
	}
	first := drive(t, modelFor(f.svc, f.alice, 80, 24), key("enter"), key("enter"), key("o"), key("down"), key("enter"))
	if first.compose.replyTo == 0 {
		t.Fatal("direct draft didn't capture the intended post")
	}
	// Treat the session as gone after allocating the save for the last input.
	session, disconnect := context.WithCancel(first.d.Context)
	defer disconnect()
	first.d.Context = session
	next, cmd := first.Update(tea.PasteMsg{Content: "This survives disconnect"})
	disconnect()
	first = next.(Model)
	// Complete the command without the old SSH session's context.
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				run(sub)
			}
		case draftSavedMsg:
			if msg.err != nil {
				t.Fatalf("save after disconnect: %v", msg.err)
			}
		}
	}
	run(cmd)
	second := drive(t, modelFor(f.svc, f.alice, 80, 24), key("enter"), key("enter"), key("r"))
	if second.compose.body.Value() != first.compose.body.Value() || second.compose.replyTo != first.compose.replyTo {
		t.Fatalf("resumed body=%q, recipient=%d", second.compose.body.Value(), second.compose.replyTo)
	}
	second = drive(t, second, tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}, key("y"))
	if second.overlay != ovNone || second.flash != "Draft discarded" {
		t.Fatalf("discard: overlay=%v flash=%q", second.overlay, second.flash)
	}
	third := drive(t, modelFor(f.svc, f.alice, 80, 24), key("enter"), key("enter"), key("r"))
	if third.compose.body.Value() != "" || third.compose.replyTo != 0 {
		t.Fatalf("discarded draft reappeared: %q", third.compose.body.Value())
	}
}
