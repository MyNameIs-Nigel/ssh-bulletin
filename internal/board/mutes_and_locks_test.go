package board

import (
	"context"
	"errors"
	"testing"
)

func TestMutesArePrivateAndFilterChatHistoryAndCatchup(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	owner, author, viewer, boards := moderationAccounts(t, svc)
	b := boards[0].ID
	first, err := svc.SendChat(ctx, owner, b, "visible before")
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := svc.SendChat(ctx, author, b, "muted message")
	if err != nil {
		t.Fatal(err)
	}
	last, err := svc.SendChat(ctx, owner, b, "visible after")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetMuted(ctx, viewer.ID, viewer.ID, true); !errors.Is(err, ErrSelfMute) {
		t.Fatalf("self mute: %v", err)
	}
	if err := svc.SetMuted(ctx, viewer.ID, author.ID, true); err != nil {
		t.Fatal(err)
	}
	if ids, err := svc.MutedIDs(ctx, owner.ID); err != nil || len(ids) != 0 {
		t.Fatalf("another player's mutes: %v %v", ids, err)
	}
	if ids, err := svc.MutedIDs(ctx, viewer.ID); err != nil || !ids[author.ID] {
		t.Fatalf("saved mute: %v %v", ids, err)
	}
	page, more, err := svc.ChatHistoryFor(ctx, viewer.ID, b, 0, 1)
	if err != nil || !more || len(page) != 1 || page[0].ID != last.ID {
		t.Fatalf("latest filtered page: %+v %t %v", page, more, err)
	}
	page, more, err = svc.ChatHistoryFor(ctx, viewer.ID, b, last.ID, 1)
	if err != nil || more || len(page) != 1 || page[0].ID != first.ID {
		t.Fatalf("older filtered page: %+v %t %v", page, more, err)
	}
	since, err := svc.ChatSinceFor(ctx, viewer.ID, b, first.ID)
	if err != nil || len(since) != 1 || since[0].ID != last.ID {
		t.Fatalf("filtered catchup: %+v %v", since, err)
	}
	if err := svc.ReportContent(ctx, viewer.ID, ModerationTarget{Kind: ChatTarget, ID: hidden.ID}, "Evidence still available to staff"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetMuted(ctx, viewer.ID, author.ID, false); err != nil {
		t.Fatal(err)
	}
	since, err = svc.ChatSinceFor(ctx, viewer.ID, b, first.ID)
	if err != nil || len(since) != 2 || since[0].ID != hidden.ID {
		t.Fatalf("unmuted catchup: %+v %v", since, err)
	}
}

func TestThreadLockGuardsStaleRepliesAndKeepsHistory(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	owner, author, other, boards := moderationAccounts(t, svc)
	id, err := svc.CreateThread(ctx, author, boards[0].ID, "Still readable", "Opening text")
	if err != nil {
		t.Fatal(err)
	}
	stale, err := svc.Thread(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reply(ctx, other, stale, "Existing reply"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetThreadLock(ctx, author.ID, id, true, "Author has no authority"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member lock: %v", err)
	}
	if err := svc.SetThreadLock(ctx, owner.ID, id, true, "  "); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("empty lock reason: %v", err)
	}
	if err := svc.SetThreadLock(ctx, owner.ID, id, true, "Pause discussion"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reply(ctx, other, stale, "Stale composer"); !errors.Is(err, ErrThreadLocked) {
		t.Fatalf("reply from stale composer: %v", err)
	}
	if _, err := svc.Reply(ctx, owner, stale, "Staff reply"); !errors.Is(err, ErrThreadLocked) {
		t.Fatalf("staff reply to locked thread: %v", err)
	}
	locked, err := svc.Thread(ctx, id)
	if err != nil || locked.Title != stale.Title || locked.Removed || !locked.Locked() || locked.LockReason != "Pause discussion" {
		t.Fatalf("locked thread: %+v %v", locked, err)
	}
	if posts, err := svc.Posts(ctx, id, other.ID); err != nil || len(posts) != 2 || posts[0].Body != "Opening text" || posts[1].Body != "Existing reply" {
		t.Fatalf("locked thread posts: %+v %v", posts, err)
	}
	if err := svc.SetThreadLock(ctx, owner.ID, id, true, "Repeated request"); !errors.Is(err, ErrReviewChanged) {
		t.Fatalf("duplicate lock: %v", err)
	}
	if err := svc.SetThreadLock(ctx, owner.ID, id, false, "Discussion reopened"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reply(ctx, other, locked, "New reply after unlock"); err != nil {
		t.Fatal(err)
	}
	history, err := svc.ModerationPage(ctx, owner.ID, HistoryView, 0)
	if err != nil || len(history) != 2 || history[0].Target != (ModerationTarget{Kind: ThreadTarget, ID: id}) || history[0].Action != ThreadUnlocked || history[1].Action != ThreadLocked || history[1].Reason != "Pause discussion" {
		t.Fatalf("lock history: %+v %v", history, err)
	}
}
