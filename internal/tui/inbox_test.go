package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

func TestInboxOpenExactPostAndMuteThread(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	id, err := f.svc.CreateThread(ctx, f.alice, f.board.ID, "Inbox test thread", "Opening text")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := f.svc.Thread(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Reply(ctx, f.bob, thread, "An older reply"); err != nil {
		t.Fatal(err)
	}
	post, err := f.svc.Reply(ctx, f.owner, thread, "A new reply to @Alice")
	if err != nil {
		t.Fatal(err)
	}
	m := drive(t, modelFor(f.svc, f.alice, 80, 24))
	if text := screenText(m); !strings.Contains(text, "inbox (2)") {
		t.Fatalf("home unread count:\n%s", text)
	}
	m = clickOn(t, m, "foot:hint:i")
	if m.screen != scrInbox || len(m.inbox.entries) != 1 || m.inbox.entries[0].Unread != 2 {
		t.Fatalf("inbox groups: %+v", m.inbox)
	}
	m = drive(t, m, key("enter"))
	if len(m.inbox.entries) != 2 || m.inbox.entries[0].PostID != post {
		t.Fatalf("thread activity: %+v", m.inbox)
	}
	m = clickOn(t, m, "inbox:0")
	if m.screen != scrThread || m.posts[m.postCursor].ID != post || m.inbox.unread != 1 {
		t.Fatalf("opened notification: screen=%v, cursor=%d, unread=%d", m.screen, m.postCursor, m.inbox.unread)
	}
	m = drive(t, m, key("esc"), key("m"))
	if m.screen != scrInbox || m.inbox.threadID != 0 || len(m.inbox.entries) != 0 || m.inbox.unread != 0 {
		t.Fatalf("muted thread in inbox: %+v", m.inbox)
	}
	reconnected := drive(t, modelFor(f.svc, f.alice, 80, 24), key("enter"), key("enter"), key("o"))
	i := slices.IndexFunc(reconnected.opts.menu.Items, func(it kit.Item) bool { return it.Title == "Unmute thread notifications" })
	if i < 0 {
		t.Fatalf("reopened thread options: %+v", reconnected.opts.menu.Items)
	}
	reconnected.opts.menu.Select(i)
	reconnected = drive(t, reconnected, key("enter"))
	if reconnected.threadMuted || reconnected.inbox.unread != 1 {
		t.Fatalf("unmute left unread=%d, muted=%t", reconnected.inbox.unread, reconnected.threadMuted)
	}
}

func TestInboxDirectReplyKeepsRecipientInDraft(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	id, err := f.svc.CreateThread(ctx, f.owner, f.board.ID, "A discussion", "Opening text")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := f.svc.Thread(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	target, err := f.svc.Reply(ctx, f.bob, thread, "The post to reply to")
	if err != nil {
		t.Fatal(err)
	}
	m := drive(t, modelFor(f.svc, f.alice, 80, 24), key("enter"), key("enter"), key("down"), key("o"), key("down"), key("enter"))
	if m.overlay != ovCompose || m.compose.replyTo != target {
		t.Fatalf("compose target: %d, overlay=%v", m.compose.replyTo, m.overlay)
	}
	m = drive(t, m, tea.PasteMsg{Content: "A direct reply"}, key("esc"), key("r"))
	if m.compose.replyTo != target {
		t.Fatal("resumed draft lost its direct recipient")
	}
	m = drive(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if count, err := f.svc.InboxUnread(ctx, f.bob.ID); err != nil || count != 1 || m.overlay != ovNone {
		t.Fatalf("direct reply delivered %d alerts, overlay=%v, err=%v", count, m.overlay, err)
	}
}
