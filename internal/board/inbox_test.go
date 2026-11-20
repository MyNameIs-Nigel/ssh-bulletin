package board

import (
	"context"
	"errors"
	"testing"
)

func TestInboxGroupsAndPersistsReadStateWithMutes(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	owner, player, reporter, boards := moderationAccounts(t, svc)
	id, err := svc.CreateThread(ctx, owner, boards[0].ID, "A thread", "Hi @Reporter @REPORTER @Owner `@Player`")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.Thread(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	wantUnread := func(a Account, want int) {
		t.Helper()
		if n, err := svc.InboxUnread(ctx, a.ID); err != nil || n != want {
			t.Fatalf("%s unread = %d, %v, want %d", a.Handle, n, err, want)
		}
	}
	wantUnread(reporter, 1)
	wantUnread(owner, 0)
	wantUnread(player, 0)
	first, err := svc.Reply(ctx, player, thread, "My reply")
	if err != nil {
		t.Fatal(err)
	}
	direct, err := svc.ReplyTo(ctx, reporter, thread, first, "Thanks @Player @Owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReplyTo(ctx, reporter, thread, 9999, "Unknown target"); !errors.Is(err, ErrReplyTarget) {
		t.Fatalf("invalid direct reply: %v", err)
	}
	groups, more, err := svc.InboxPage(ctx, owner.ID, 0, 0)
	if err != nil || more || len(groups) != 1 || groups[0].ThreadID != id || groups[0].PostID != direct || groups[0].Unread != 2 || groups[0].Reasons != ThreadReply|Mention {
		t.Fatalf("owner groups: %+v, more=%t, %v", groups, more, err)
	}
	entries, _, err := svc.InboxPage(ctx, player.ID, id, 0)
	if err != nil || len(entries) != 1 || entries[0].PostID != direct || entries[0].Reasons != DirectReply|Mention {
		t.Fatalf("direct recipient entries: %+v, %v", entries, err)
	}
	if err := svc.ReadInboxPost(ctx, owner.ID, direct); err != nil {
		t.Fatal(err)
	}
	reconnected := NewService(svc.st, NewBroker(), svc.now)
	entries, _, err = reconnected.InboxPage(ctx, owner.ID, id, 0)
	if err != nil || len(entries) != 2 || entries[0].Unread != 0 || entries[1].Unread != 1 || entries[1].PostID != first {
		t.Fatalf("read state after reconnect: %+v %v", entries, err)
	}
	wantUnread(player, 1)
	if err := svc.SetMuted(ctx, owner.ID, player.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reply(ctx, player, thread, "Muted reply and @Owner mention"); err != nil {
		t.Fatal(err)
	}
	wantUnread(owner, 0)
	if err := svc.SetMuted(ctx, owner.ID, player.ID, false); err != nil {
		t.Fatal(err)
	}
	wantUnread(owner, 1)
	if err := svc.SetThreadMuted(ctx, owner.ID, thread.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reply(ctx, reporter, thread, "Muted thread @Owner and @Player"); err != nil {
		t.Fatal(err)
	}
	wantUnread(owner, 0)
	wantUnread(player, 2)
	if on, err := reconnected.ThreadMuted(ctx, owner.ID, id); err != nil || !on {
		t.Fatalf("thread mute after reconnect = %t, %v", on, err)
	}
	if err := svc.SetThreadMuted(ctx, owner.ID, id, false); err != nil {
		t.Fatal(err)
	}
	wantUnread(owner, 1)
	if err := svc.DeletePost(ctx, player.ID, first); err != nil {
		t.Fatal(err)
	}
	wantUnread(owner, 0)
}
