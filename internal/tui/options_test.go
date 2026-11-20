package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
)

func TestReportPostMenuAndShortcutUseTheSameTarget(t *testing.T) {
	m, _ := lobbyThread(t, 80, 24)
	m = drive(t, m, key("down"), key("!"))
	shortcut := m.modForm
	m = drive(t, m, key("esc"), key("o"))
	m.overlayT = 1
	for i, item := range m.opts.menu.Items {
		if item.Title == "Report post" {
			m = clickOn(t, m, fmt.Sprintf("option:%d", i))
			if m.overlay != ovModeration || m.modForm.action != shortcut.action || m.modForm.content != shortcut.content || m.modForm.target != shortcut.target {
				t.Fatalf("menu target %+v differs from shortcut %+v", m.modForm.content, shortcut.content)
			}
			return
		}
	}
	t.Fatal("post options are missing Report post")
}

func TestPostOptionsAndPlaceholders(t *testing.T) {
	ctx := context.Background()
	svc, owner, member, b := pinFixture(t)
	tid, err := svc.CreateThread(ctx, member, b.ID, "A conversation", "Original opening")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.Thread(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reply(ctx, owner, thread, "Staff reply to remove"); err != nil {
		t.Fatal(err)
	}
	openThread := func(a board.Account) Model {
		m := modelFor(svc, a, 64, 20)
		m.curBoard = b
		m = drive(t, m, m.enterThread(thread, 0)())
		return m
	}
	assertOptions := func(m Model, want ...string) {
		t.Helper()
		if m.overlay != ovOptions {
			t.Fatalf("expected options, got overlay %v", m.overlay)
		}
		view := ansi.Strip(m.viewOptions())
		for _, label := range []string{"View author profile", "Delete opening post", "Delete reply", "Remove reply", "Remove opening post", "Pin to top of board"} {
			expected := false
			for _, w := range want {
				expected = expected || label == w
			}
			if strings.Contains(view, label) != expected {
				t.Fatalf("expected %q present=%t in:\n%s", label, expected, view)
			}
		}
	}

	m := drive(t, openThread(member), key("v"), key("x"))
	if m.screen != scrThread || m.overlay != ovNone {
		t.Fatal("old shortcuts should have no effect")
	}
	m = drive(t, m, key("o"))
	assertOptions(m, "View author profile", "Delete opening post")
	if !strings.Contains(ansi.Strip(m.viewOptions()), "Reply linking this post") {
		t.Fatal("a live post should offer a reply that links it")
	}
	m = drive(t, m, key("down"), key("down"), key("enter"), key("esc"))
	if m.opts.confirming || m.posts[0].Deleted {
		t.Fatal("cancel should leave the opening post in place")
	}
	m = drive(t, m, key("enter"), key("y"))
	if !m.posts[0].Deleted || !strings.Contains(ansi.Strip(m.View().Content), "< deleted >") ||
		strings.Contains(ansi.Strip(m.View().Content), "Original opening") {
		t.Fatalf("deleted marker or content is incorrect: %+v", m.posts[0])
	}
	m = drive(t, m, key("o"))
	assertOptions(m, "View author profile")
	m = drive(t, m, key("esc"), key("down"), key("o"))
	assertOptions(m, "View author profile")

	staff := drive(t, openThread(owner), key("down"), key("o"))
	assertOptions(staff, "View author profile", "Delete reply", "Remove reply")
	staff = drive(t, staff, key("down"), key("down"), key("down"), key("enter"), tea.PasteMsg{Content: "Reviewed"},
		key("enter"))
	view := ansi.Strip(staff.View().Content)
	if !staff.posts[1].Removed || !strings.Contains(view, "< removed >") || strings.Contains(view, "Staff reply to remove") {
		t.Fatalf("removed reply is not rendered as a placeholder:\n%s", view)
	}
}
