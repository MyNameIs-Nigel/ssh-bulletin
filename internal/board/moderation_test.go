package board

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mynameis-nigel/ssh-bulletin/internal/identity"
)

func registerTestKey(t *testing.T, svc *Service, key byte, handle string) Account {
	t.Helper()
	fp := identity.FingerprintFromBytes([32]byte{key})
	a, err := svc.Register(context.Background(), fp, "", handle, "reef")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestModerationRolesAndVisibility(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	initial := identity.FingerprintFromBytes([32]byte{1})
	if _, ok, err := svc.Connect(ctx, initial); ok || err != nil {
		t.Fatalf("first connection: registered=%v err=%v", ok, err)
	}
	alice := registerTestKey(t, svc, 2, "Alice")
	owner := registerTestKey(t, svc, 1, "Owner")
	mod := registerTestKey(t, svc, 3, "Moderator")
	if owner.Role != RoleOwner || alice.Role != "" {
		t.Fatalf("roles: owner=%q, first to register=%q", owner.Role, alice.Role)
	}
	if _, err := svc.UpdateProfile(ctx, mod, "ALICE", mod.Palette, "", mod.Flair); !errors.Is(err, ErrHandleTaken) {
		t.Fatalf("rename collision: %v", err)
	}
	if _, err := svc.SetAdmin(ctx, alice.ID, mod.Handle, mod.Fingerprint, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member grant: %v", err)
	}
	if _, err := svc.SetAdmin(ctx, owner.ID, mod.Handle, alice.Fingerprint, true); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("mismatched key grant: %v", err)
	}
	if _, err := svc.SetAdmin(ctx, owner.ID, mod.Handle, mod.Fingerprint[:19], true); !errors.Is(err, ErrInvalidFingerprint) {
		t.Fatalf("partial fingerprint grant: %v", err)
	}
	mod, err := svc.SetAdmin(ctx, owner.ID, "moderator", mod.Fingerprint, true)
	if err != nil || mod.Role != RoleAdmin {
		t.Fatalf("admin grant: role=%q err=%v", mod.Role, err)
	}
	mod, err = svc.UpdateProfile(ctx, mod, "NewMod", mod.Palette, "", mod.Flair)
	if err != nil || mod.Role != RoleAdmin {
		t.Fatalf("admin rename: role=%q err=%v", mod.Role, err)
	}
	if _, err := svc.SetAdmin(ctx, mod.ID, alice.Handle, alice.Fingerprint, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin grant by admin: %v", err)
	}
	if _, err := svc.Admins(ctx, mod.ID, 0); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin list opened by admin: %v", err)
	}
	admins, err := svc.Admins(ctx, owner.ID, 0)
	if err != nil || len(admins) != 1 || admins[0].ID != mod.ID {
		t.Fatalf("owner admin list: %+v %v", admins, err)
	}

	boards, err := svc.Boards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tid, err := svc.CreateThread(ctx, owner, boards[0].ID, "Title", "opening")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.Thread(ctx, tid)
	if err != nil || thread.AuthorRole != RoleOwner {
		t.Fatalf("thread: %+v %v", thread, err)
	}
	rid, err := svc.Reply(ctx, mod, thread, "reply")
	if err != nil {
		t.Fatal(err)
	}
	posts, err := svc.Posts(ctx, tid, alice.ID)
	if err != nil || len(posts) != 2 || posts[1].AuthorRole != RoleAdmin {
		t.Fatalf("public posts: %+v %v", posts, err)
	}
	opening, reply := posts[0], posts[1]

	if err := svc.Report(ctx, alice.ID, rid, "   "); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("empty reason: %v", err)
	}
	if err := svc.Report(ctx, alice.ID, rid, "Spam\nexample"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Report(ctx, alice.ID, rid, "Spam again"); !errors.Is(err, ErrAlreadyReported) {
		t.Fatalf("duplicate report: %v", err)
	}
	if err := svc.Report(ctx, owner.ID, rid, "Second report"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ModerationPage(ctx, alice.ID, QueueView, 0); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member read review queue: %v", err)
	}
	if err := svc.Moderate(ctx, alice.ID, rid, Removed, "Spam"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member remove: %v", err)
	}
	queue, err := svc.ModerationPage(ctx, mod.ID, QueueView, 0)
	if err != nil || len(queue) != 2 || queue[0].Reason != "Spam example" || queue[0].Body != "reply" {
		t.Fatalf("queue: %+v %v", queue, err)
	}
	if err := svc.Moderate(ctx, mod.ID, rid, Dismissed, "Verified"); err != nil {
		t.Fatal(err)
	}
	queue, err = svc.ModerationPage(ctx, mod.ID, QueueView, 0)
	if err != nil || len(queue) != 0 {
		t.Fatalf("dismiss resolves all reports: %+v %v", queue, err)
	}

	if err := svc.Moderate(ctx, mod.ID, rid, Removed, "Repeated spam"); err != nil {
		t.Fatal(err)
	}
	posts, err = svc.Posts(ctx, tid, alice.ID)
	if err != nil || len(posts) != 2 || posts[0].ID != opening.ID || posts[1].Placeholder() != "< removed >" || posts[1].Body != "" {
		t.Fatalf("public posts after removal: %+v %v", posts, err)
	}
	if _, err := svc.ToggleKudos(ctx, alice, reply); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale kudos on removed reply: %v", err)
	}
	if err := svc.Moderate(ctx, mod.ID, opening.ID, Removed, "Opening post"); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Thread(ctx, tid); err != nil || !got.Removed || got.Title != "< removed >" || got.ReplyCount != 0 {
		t.Fatalf("removed thread placeholder: %+v, %v", got, err)
	}
	posts, err = svc.Posts(ctx, tid, alice.ID)
	if err != nil || len(posts) != 2 || posts[0].Placeholder() != "< removed >" || posts[0].Body != "" {
		t.Fatalf("removed opening post placeholder: %+v, %v", posts, err)
	}
	if _, err := svc.Reply(ctx, alice, thread, "stale reply"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reply to removed thread: %v", err)
	}
	list, err := svc.Threads(ctx, thread.BoardID)
	if err != nil || len(list) != 1 || list[0].Title != "< removed >" {
		t.Fatalf("board listing should show the placeholder: %+v %v", list, err)
	}
	stats, err := svc.Stats(ctx)
	if err != nil || stats.Threads != 1 || stats.Posts != 0 {
		t.Fatalf("visible totals: %+v %v", stats, err)
	}
	removed, err := svc.ModerationPage(ctx, mod.ID, RemovedView, 0)
	if err != nil || len(removed) != 2 || !removed[0].Opening {
		t.Fatalf("removed: %+v %v", removed, err)
	}
	if err := svc.Moderate(ctx, owner.ID, opening.ID, Restored, "Reopened"); err != nil {
		t.Fatal(err)
	}
	posts, err = svc.Posts(ctx, tid, alice.ID)
	if err != nil || len(posts) != 2 || posts[0].Body != "opening" || posts[1].Placeholder() != "< removed >" || posts[1].Body != "" {
		t.Fatalf("restore must keep previously removed replies as placeholders: %+v %v", posts, err)
	}
	history, err := svc.ModerationPage(ctx, mod.ID, HistoryView, 0)
	if err != nil || len(history) != 6 || history[0].Action != Restored || history[0].Actor != owner.Handle {
		t.Fatalf("audit history: %+v %v", history, err)
	}

	if _, err := svc.SetAdmin(ctx, owner.ID, mod.Handle, mod.Fingerprint, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ModerationPage(ctx, mod.ID, HistoryView, 0); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked admin read: %v", err)
	}
	if err := svc.Moderate(ctx, mod.ID, rid, Restored, "Stale session"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked admin write: %v", err)
	}
}

func TestPostingQuotaAcrossSessionsAndKeys(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	now := svc.now()
	svc.now = func() time.Time { return now }
	ownerKey := identity.FingerprintFromBytes([32]byte{1})
	if err := svc.ConfigureOwner(ctx, ownerKey); err != nil {
		t.Fatal(err)
	}
	owner := registerTestKey(t, svc, 1, "Owner")
	other := registerTestKey(t, svc, 2, "Other")
	boards, err := svc.Boards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tid, err := svc.CreateThread(ctx, owner, boards[0].ID, "Rate limit", "Opening post")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.Thread(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for range cap(errs) {
		wg.Go(func() {
			_, err := svc.Reply(ctx, owner, thread, "Concurrent reply")
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	var accepted int
	for err := range errs {
		if err == nil {
			accepted++
		} else if !errors.Is(err, ErrPostLimit) {
			t.Fatal(err)
		}
	}
	if accepted != 4 {
		t.Fatalf("accepted %d concurrent replies, want 4", accepted)
	}
	posts, err := svc.Posts(ctx, tid, owner.ID)
	if err != nil || len(posts) != 5 {
		t.Fatalf("thread posts: %+v %v", posts, err)
	}
	if err := svc.Moderate(ctx, owner.ID, posts[1].ID, Removed, "Count soft-removed posts"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateThread(ctx, owner, boards[0].ID, "Over limit", "Same key"); !errors.Is(err, ErrPostLimit) {
		t.Fatalf("removed posts must still count: %v", err)
	}
	for key := byte(2); key <= 4; key++ {
		if key > 2 {
			other = registerTestKey(t, svc, key, fmt.Sprintf("Other%d", key))
		}
		for range 5 {
			if _, err := svc.Reply(ctx, other, thread, "Quota across keys"); err != nil {
				t.Fatal(err)
			}
		}
	}
	next := registerTestKey(t, svc, 5, "NewKey")
	if _, err := svc.Reply(ctx, next, thread, "One too many"); !errors.Is(err, ErrNewcomerLimit) {
		t.Fatalf("rotated key escaped shared quota: %v", err)
	}
	now = now.Add(time.Minute)
	if _, err := svc.Reply(ctx, owner, thread, "Window elapsed"); err != nil {
		t.Fatalf("quota did not expire: %v", err)
	}
}
