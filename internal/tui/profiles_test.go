package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

func TestProfileNavigationAndPrivateFriendFlair(t *testing.T) {
	ctx := context.Background()
	svc, _ := testService(t)
	viewer, _, _ := svc.Lookup(ctx, "SHA256:demo-0")
	friend, _, _ := svc.Lookup(ctx, "SHA256:demo-1")
	makeModel := func(a board.Account) Model {
		return New(Deps{Service: svc, Account: &a, Fingerprint: a.Fingerprint, Width: 64, Height: 20,
			Now: func() time.Time { return fixedNow }, NoSplash: true})
	}
	m := drive(t, makeModel(viewer), key("u"), tea.PasteMsg{Content: "RoFl"}, key("enter"))
	if m.screen != scrUserProfile || m.userProfile.data.ID != friend.ID {
		t.Fatalf("opened wrong profile: %+v", m.userProfile.data)
	}
	m = drive(t, m, key("f"))
	assertFriendBadge := func(m Model, want bool) {
		t.Helper()
		if got := strings.Contains(ansi.Strip(m.View().Content), "▐F▌"); got != want {
			t.Fatalf("friend badge = %t, want %t\n%s", got, want, ansi.Strip(m.View().Content))
		}
	}
	assertFriendBadge(m, true)
	m = drive(t, m, key("esc"), key("tab"))
	if !m.directory.friendsOnly || len(m.directory.entries) != 1 || m.directory.entries[0].ID != friend.ID {
		t.Fatalf("friends view: %+v", m.directory.entries)
	}
	assertFriendBadge(m, true)
	m = drive(t, m, key("esc"), key("enter"), key("enter"), key("down"))
	if m.screen != scrThread || m.posts[m.postCursor].AuthorID != friend.ID {
		t.Fatal("expected the friend's reply to be selected")
	}
	assertFriendBadge(m, true)
	threadPanel := m.threadRect()
	iw, ih := (kit.Panel{Width: threadPanel.w, Height: threadPanel.h, PadX: threadPadX}).Inner()
	lines, starts := m.threadLines(iw)
	click := tea.MouseClickMsg{Button: tea.MouseLeft, X: threadPanel.x + 1 + threadPadX + postHeaderOffset,
		Y: threadPanel.y + 1 + starts[m.postCursor] - m.threadTop(len(lines), ih)}
	m = drive(t, m, click)
	if m.screen != scrUserProfile || m.userProfile.data.ID != friend.ID {
		t.Fatal("clicking the reply author did not open their profile")
	}
	if strings.Contains(ansi.Strip(m.View().Content), friend.Fingerprint) {
		t.Fatal("profile exposed another account's fingerprint to a regular player")
	}
	recent := m.userProfile.data.Recent[0]
	m = drive(t, m, key("enter"))
	if m.screen != scrThread || m.curThread.ID != recent.ThreadID || m.posts[m.postCursor].ID != recent.PostID {
		t.Fatal("recent activity did not jump to the selected post")
	}
	other := drive(t, makeModel(friend), key("enter"), key("enter"), key("down"))
	assertFriendBadge(other, false)

	m = drive(t, m, key("o"), key("enter"), key("f"), key("esc"))
	if m.screen != scrThread || m.curThread.ID != recent.ThreadID || m.posts[m.postCursor].ID != recent.PostID {
		t.Fatal("profile exit did not restore the selected post")
	}
	assertFriendBadge(m, false)
}

func TestProfileEditorSavesFlairAtMinimumTerminalSize(t *testing.T) {
	svc, _ := testService(t)
	acct, _, _ := svc.Lookup(context.Background(), "SHA256:demo-0")
	m := New(Deps{Service: svc, Account: &acct, Fingerprint: acct.Fingerprint, Width: 64, Height: 20,
		Now: func() time.Time { return fixedNow }, NoSplash: true})
	m = drive(t, m, key("p"), key("tab"), key("tab"), key("tab"), key("right"))
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "FLAIR ON NEW POSTS") || !strings.Contains(view, "‹ ▐Arcade▌ ›") {
		t.Fatalf("flair picker clipped or missing:\n%s", view)
	}
	m = drive(t, m, key("enter"))
	if m.acct.Flair != "Arcade" {
		t.Fatalf("selected flair = %q", m.acct.Flair)
	}
}
