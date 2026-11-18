package board

import (
	"context"
	"errors"
	"testing"

	"github.com/mynameis-nigel/ssh-bulletin/internal/identity"
)

func TestPinningIsStaffOnlyAndSortsFirst(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	if _, _, err := svc.Connect(ctx, identity.FingerprintFromBytes([32]byte{1})); err != nil {
		t.Fatal(err)
	}
	owner := registerTestKey(t, svc, 1, "Owner")
	admin := registerTestKey(t, svc, 2, "Helper")
	member := registerTestKey(t, svc, 3, "Member")
	if _, err := svc.SetAdmin(ctx, owner.ID, admin.Handle, admin.Fingerprint, true); err != nil {
		t.Fatal(err)
	}
	boards, _ := svc.Boards(ctx)
	old, err := svc.CreateThread(ctx, member, boards[0].ID, "Old but important", "first")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := svc.CreateThread(ctx, member, boards[0].ID, "Fresh chatter", "second")
	if err != nil {
		t.Fatal(err)
	}
	order := func() []int64 {
		ts, err := svc.Threads(ctx, boards[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		for _, th := range ts {
			ids = append(ids, th.ID)
		}
		return ids
	}
	if got := order(); got[0] != fresh {
		t.Fatalf("before pinning the newest thread leads: %v", got)
	}

	if err := svc.SetPinned(ctx, member.ID, old, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member pin: %v", err)
	}
	if err := svc.SetPinned(ctx, 0, old, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("anonymous pin: %v", err)
	}
	if th, _ := svc.Thread(ctx, old); th.Pinned {
		t.Fatal("a refused pin must not change the thread")
	}

	events, unsub := svc.Broker().Subscribe()
	defer unsub()
	if err := svc.SetPinned(ctx, admin.ID, old, true); err != nil {
		t.Fatalf("admin pin: %v", err)
	}
	if ev := <-events; ev.Kind != EventModeration || ev.ThreadID != old || ev.BoardID != boards[0].ID {
		t.Fatalf("event = %+v", ev)
	}
	if got := order(); got[0] != old {
		t.Fatalf("pinned thread should lead: %v", got)
	}
	// Pinning states are explicit, so repeating one is harmless.
	if err := svc.SetPinned(ctx, owner.ID, old, true); err != nil {
		t.Fatalf("owner re-pin: %v", err)
	}
	if th, _ := svc.Thread(ctx, old); !th.Pinned {
		t.Fatal("re-pinning must keep it pinned")
	}
	if err := svc.SetPinned(ctx, member.ID, old, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member unpin: %v", err)
	}
	if err := svc.SetPinned(ctx, admin.ID, old, false); err != nil {
		t.Fatalf("admin unpin: %v", err)
	}
	if got := order(); got[0] != fresh {
		t.Fatalf("after unpinning activity order returns: %v", got)
	}

	if err := svc.SetPinned(ctx, admin.ID, 9999, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing thread: %v", err)
	}
	if err := svc.Moderate(ctx, admin.ID, firstPost(t, svc, old), Removed, "spam"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPinned(ctx, admin.ID, old, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed thread: %v", err)
	}
}

func TestPinLimitIsTwoPerBoard(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	if _, _, err := svc.Connect(ctx, identity.FingerprintFromBytes([32]byte{1})); err != nil {
		t.Fatal(err)
	}
	owner := registerTestKey(t, svc, 1, "Owner")
	boards, _ := svc.Boards(ctx)
	var ids [4]int64
	for i := range ids {
		var err error
		ids[i], err = svc.CreateThread(ctx, owner, boards[i%2].ID, "T"+string(rune('A'+i)), "body")
		if err != nil {
			t.Fatal(err)
		}
	}
	// ids[0], ids[2] share boards[0]; ids[1], ids[3] share boards[1].
	third, err := svc.CreateThread(ctx, owner, boards[0].ID, "TE", "body")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{ids[0], ids[2]} {
		if err := svc.SetPinned(ctx, owner.ID, id, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.SetPinned(ctx, owner.ID, third, true); !errors.Is(err, ErrPinLimit) {
		t.Fatalf("third pin on a board: %v", err)
	}
	if th, _ := svc.Thread(ctx, third); th.Pinned {
		t.Fatal("a refused pin must not change the thread")
	}
	if err := svc.SetPinned(ctx, owner.ID, ids[0], true); err != nil {
		t.Fatalf("re-pinning a pinned thread at the cap: %v", err)
	}
	// The cap is per board.
	if err := svc.SetPinned(ctx, owner.ID, ids[1], true); err != nil {
		t.Fatalf("another board's pin: %v", err)
	}
	// Unpinning frees a slot.
	if err := svc.SetPinned(ctx, owner.ID, ids[2], false); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPinned(ctx, owner.ID, third, true); err != nil {
		t.Fatalf("pin after freeing a slot: %v", err)
	}
	// Removing a pinned thread releases its pin, so restoring it cannot
	// bring a board back over the cap.
	opening := firstPost(t, svc, ids[0])
	if err := svc.Moderate(ctx, owner.ID, opening, Removed, "spam"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPinned(ctx, owner.ID, ids[2], true); err != nil {
		t.Fatalf("removed thread should not hold a slot: %v", err)
	}
	if err := svc.Moderate(ctx, owner.ID, opening, Restored, "ok"); err != nil {
		t.Fatal(err)
	}
	if th, _ := svc.Thread(ctx, ids[0]); th.Pinned {
		t.Fatal("a restored thread comes back unpinned")
	}
	ts, _ := svc.Threads(ctx, boards[0].ID)
	pinned := 0
	for _, th := range ts {
		if th.Pinned {
			pinned++
		}
	}
	if pinned > MaxPinned {
		t.Fatalf("board has %d pinned threads", pinned)
	}
}

func firstPost(t *testing.T, svc *Service, thread int64) int64 {
	t.Helper()
	posts, err := svc.Posts(context.Background(), thread, 0)
	if err != nil || len(posts) == 0 {
		t.Fatalf("posts: %v %v", posts, err)
	}
	return posts[0].ID
}
