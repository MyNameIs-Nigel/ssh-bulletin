package board

import (
	"context"
	"errors"
	"testing"
)

func TestAuthorDeletionPreservesThreadAndChecksOwnership(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	alice := registerTestKey(t, svc, 1, "Alice")
	bob := registerTestKey(t, svc, 2, "Bob")
	boards, err := svc.Boards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tid, err := svc.CreateThread(ctx, alice, boards[0].ID, "Original title", "Original opening")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.Thread(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := svc.Reply(ctx, bob, thread, "Original reply")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeletePost(ctx, alice.ID, pid); !errors.Is(err, ErrForbidden) {
		t.Fatalf("deleting another author's reply: %v", err)
	}
	if err := svc.DeletePost(ctx, bob.ID, pid); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeletePost(ctx, bob.ID, pid); !errors.Is(err, ErrReviewChanged) {
		t.Fatalf("repeated deletion: %v", err)
	}
	posts, err := svc.Posts(ctx, tid, alice.ID)
	if err != nil || len(posts) != 2 || posts[1].ID != pid || posts[1].Placeholder() != "< deleted >" || posts[1].Body != "" {
		t.Fatalf("reply placeholder: %+v, %v", posts, err)
	}
	if err := svc.Report(ctx, alice.ID, pid, "Spam"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("report on deleted reply: %v", err)
	}
	if _, err := svc.ToggleKudos(ctx, alice, posts[1]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("kudos on deleted reply: %v", err)
	}
	if err := svc.DeletePost(ctx, alice.ID, posts[0].ID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Thread(ctx, tid)
	if err != nil || got.Title != "< deleted >" || !got.OpeningUnavailable || got.Removed || got.ReplyCount != 0 {
		t.Fatalf("thread after opening deletion: %+v, %v", got, err)
	}
	if _, err := svc.Reply(ctx, bob, thread, "Still talking"); err != nil {
		t.Fatalf("reply to thread with a deleted opening: %v", err)
	}
	got, err = svc.Thread(ctx, tid)
	if err != nil || got.ReplyCount != 1 {
		t.Fatalf("reply count after deletion: %+v, %v", got, err)
	}
	profile, err := svc.Profile(ctx, bob.ID)
	if err != nil || profile.PostCount != 1 || len(profile.Recent) != 1 || profile.Recent[0].Title != "< deleted >" {
		t.Fatalf("profile after deletion: %+v, %v", profile, err)
	}
}
