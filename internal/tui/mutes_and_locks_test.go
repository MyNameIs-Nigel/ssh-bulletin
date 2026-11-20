package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
)

func TestMuteFromPostCollapsesAndKeepsReport(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	chat := f.say(t, f.bob, "old chat by Bob")
	if _, err := f.svc.CreateThread(ctx, f.bob, f.board.ID, "A private mute test", "This body should collapse"); err != nil {
		t.Fatal(err)
	}
	m := drive(t, modelFor(f.svc, f.alice, 80, 24), key("enter"), key("enter"), key("o"))
	if m.overlay != ovOptions || m.opts.menu.Items[2].Title != "Mute player" {
		t.Fatalf("post options: %+v", m.opts.menu.Items)
	}
	m.overlayT = 1
	m = clickOn(t, m, "option:2")
	m.overlayT = 0
	if !m.muted[f.bob.ID] || len(m.chat.msgs) != 0 {
		t.Fatalf("mute failed, chat: %+v", m.chat.msgs)
	}
	if screen := screenText(m); !strings.Contains(screen, "Muted player · reveal post") || strings.Contains(screen, "This body should collapse") || strings.Contains(screen, "A private mute test") {
		t.Fatalf("collapsed thread:\n%s", screen)
	}
	m = clickOn(t, m, fmt.Sprintf("post:%d:reveal", m.posts[0].ID))
	if screen := screenText(m); !strings.Contains(screen, "This body should collapse") || !strings.Contains(screen, "A private mute test") {
		t.Fatalf("revealed thread:\n%s", screen)
	}
	m = drive(t, m, key("z"), key("!"))
	if m.modForm.content != (board.ModerationTarget{Kind: board.PostTarget, ID: m.posts[0].ID}) {
		t.Fatalf("collapsed post lost report action: %+v", m.modForm)
	}
	m = drive(t, m, key("esc"))
	toasts := m.toasts.Len()
	m = drive(t, m, eventMsgFor(board.Event{Kind: board.EventThread, AccountID: f.bob.ID, Actor: f.bob.Handle, Title: "Another muted title"}))
	if m.toasts.Len() != toasts {
		t.Fatal("activity from a muted player produced a toast")
	}
	m = drive(t, m, m.openUserProfile(f.bob.ID)(), key("o"))
	if len(m.opts.menu.Items) != 2 || m.opts.menu.Items[0].Title != "Report profile" || m.opts.menu.Items[1].Title != "Unmute player" {
		t.Fatalf("muted profile options: %+v", m.opts.menu.Items)
	}
	m = drive(t, m, key("down"), key("enter"))
	if m.muted[f.bob.ID] || len(m.chat.msgs) != 1 || m.chat.msgs[0].ID != chat.ID {
		t.Fatalf("unmute did not restore chat: %+v", m.chat.msgs)
	}
}

func TestThreadLockFormRefreshesThreadAndProtectsDraft(t *testing.T) {
	f := newChatFixture(t)
	if _, err := f.svc.CreateThread(context.Background(), f.bob, f.board.ID, "A discussion", "Existing text"); err != nil {
		t.Fatal(err)
	}
	staff := drive(t, modelFor(f.svc, f.owner, 80, 24), key("enter"), key("o"))
	if staff.overlay != ovOptions || staff.opts.menu.Items[3].Title != "Lock thread" {
		t.Fatalf("staff board options: %+v", staff.opts.menu.Items)
	}
	member := drive(t, modelFor(f.svc, f.alice, 80, 24), key("enter"), key("enter"), key("r"), tea.PasteMsg{Content: "A stale draft"})
	staff.overlayT = 1
	staff = clickOn(t, staff, "option:3")
	staff = drive(t, staff, tea.PasteMsg{Content: "Pause the discussion"}, key("enter"), key("enter"))
	staff.overlayT = 0
	if text := screenText(staff); !strings.Contains(text, "Pause the discussion") || !strings.Contains(text, "Existing text") {
		t.Fatalf("locked discussion:\n%s", text)
	}
	member = drive(t, member, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if member.overlay != ovCompose || member.compose.err != board.ErrThreadLocked.Error() || member.compose.body.Value() != "A stale draft" {
		t.Fatalf("locked submission: overlay = %v, error = %q, draft = %q", member.overlay, member.compose.err, member.compose.body.Value())
	}
}
