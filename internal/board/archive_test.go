package board

import (
	"context"
	"errors"
	"testing"
)

func TestSearchTracksEditsModerationAndMutes(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	owner, author, viewer, boards := moderationAccounts(t, svc)
	id, err := svc.CreateThread(ctx, author, boards[0].ID, "Circuit handbook", "original phrase")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.Thread(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := svc.Reply(ctx, owner, thread, "Vintage capacitors and practical supplies")
	if err != nil {
		t.Fatal(err)
	}
	posts, err := svc.Posts(ctx, id, author.ID)
	if err != nil || len(posts) != 2 {
		t.Fatalf("initial posts: %+v %v", posts, err)
	}
	opening, reply := posts[0], posts[1]
	search := func(query string, wantPost int64) {
		t.Helper()
		rows, more, err := svc.SearchThreads(ctx, viewer.ID, query, 0)
		if err != nil || more || (wantPost == 0 && len(rows) != 0) || (wantPost != 0 && (len(rows) != 1 || rows[0].ThreadID != id || rows[0].PostID != wantPost)) {
			t.Fatalf("search %q = %+v, more=%t, err=%v, want #%d", query, rows, more, err, wantPost)
		}
	}
	search(`circuit`, opening.ID)
	search(`"vintage" capacitors`, pid)
	search(`NEAR(vintage) "`, 0)
	if _, err := svc.EditPost(ctx, owner.ID, opening, "Staff overwrite", "Wrong author"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("staff edited another player's post: %v", err)
	}
	if _, err := svc.EditPost(ctx, author.ID, opening, "", "Cleared title"); !errors.Is(err, ErrEmptyTitle) {
		t.Fatalf("empty edited title: %v", err)
	}
	if changed, err := svc.EditPost(ctx, author.ID, opening, "Revised field guide", "public corrections"); err != nil || !changed {
		t.Fatalf("opening edit: changed=%t, %v", changed, err)
	}
	if _, err := svc.EditPost(ctx, author.ID, opening, "Stale revision", "Old buffer"); !errors.Is(err, ErrEditChanged) {
		t.Fatalf("stale edit: %v", err)
	}
	if changed, err := svc.EditPost(ctx, owner.ID, reply, "Cannot rename thread", "Replacing capacitors"); err != nil || !changed {
		t.Fatalf("reply edit: changed=%t, %v", changed, err)
	}
	posts, err = svc.Posts(ctx, id, author.ID)
	if err != nil || posts[0].EditedAt == 0 || posts[1].EditedAt == 0 || posts[0].Body != "public corrections" || posts[1].Body != "Replacing capacitors" {
		t.Fatalf("edited posts: %+v %v", posts, err)
	}
	updated, err := svc.Thread(ctx, id)
	if err != nil || updated.Title != "Revised field guide" || updated.LastPostAt != thread.LastPostAt {
		t.Fatalf("edited thread: %+v %v", updated, err)
	}
	search("circuit", 0)
	search("revision", 0)
	search("field guide", opening.ID)
	search("replacing capacitors", reply.ID)
	history, err := svc.ModerationPage(ctx, owner.ID, HistoryView, 0)
	if err != nil || len(history) != 2 || history[1].Action != "edited" || history[1].Title != "Circuit handbook" || history[1].Body != "original phrase" {
		t.Fatalf("previous versions: %+v %v", history, err)
	}
	if err := svc.SetMuted(ctx, viewer.ID, owner.ID, true); err != nil {
		t.Fatal(err)
	}
	search("capacitors", 0)
	if err := svc.SetMuted(ctx, viewer.ID, owner.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := svc.Moderate(ctx, owner.ID, opening.ID, Removed, "Review root"); err != nil {
		t.Fatal(err)
	}
	search("field guide", 0)
	search("capacitors", 0)
	if _, err := svc.EditPost(ctx, author.ID, posts[0], "Overridden removal", "hidden"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed post edit: %v", err)
	}
	if err := svc.Moderate(ctx, owner.ID, opening.ID, Restored, "Reviewed"); err != nil {
		t.Fatal(err)
	}
	search("capacitors", reply.ID)
	if err := svc.DeletePost(ctx, owner.ID, reply.ID); err != nil {
		t.Fatal(err)
	}
	search("capacitors", 0)
}

func TestDraftOrderDiscardAndAtomicSubmission(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	owner, author, other, boards := moderationAccounts(t, svc)
	tid, err := svc.CreateThread(ctx, owner, boards[0].ID, "Conversation", "Opening")
	if err != nil {
		t.Fatal(err)
	}
	opening, err := svc.Posts(ctx, tid, author.ID)
	if err != nil || len(opening) != 1 {
		t.Fatalf("thread post: %+v %v", opening, err)
	}
	replyKey := DraftKey{BoardID: boards[0].ID, ThreadID: tid}
	boardKey := DraftKey{BoardID: boards[1].ID}
	old := Draft{Body: "Earlier reply", Revision: svc.NextDraftRevision(0)}
	current := Draft{Body: "  Newest reply\n\n", ReplyTo: opening[0].ID, Revision: svc.NextDraftRevision(old.Revision)}
	if err := svc.SaveDraft(ctx, author.ID, replyKey, current); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveDraft(ctx, author.ID, replyKey, old); !errors.Is(err, ErrDraftChanged) {
		t.Fatalf("out-of-order draft save: %v", err)
	}
	boardDraft := Draft{Title: "Board draft", Body: "Different destination", Revision: svc.NextDraftRevision(0)}
	if err := svc.SaveDraft(ctx, author.ID, boardKey, boardDraft); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Draft(ctx, other.ID, replyKey); err != nil || got != (Draft{}) {
		t.Fatalf("other account draft: %+v %v", got, err)
	}
	if got, err := svc.Draft(ctx, author.ID, replyKey); err != nil || got != current {
		t.Fatalf("restored draft: %+v %v", got, err)
	}
	if err := svc.SetThreadLock(ctx, owner.ID, tid, true, "Pause replies"); err != nil {
		t.Fatal(err)
	}
	submit := current
	submit.Revision = svc.NextDraftRevision(current.Revision)
	if _, _, err := svc.SubmitDraft(ctx, author, replyKey, submit); !errors.Is(err, ErrThreadLocked) {
		t.Fatalf("locked submission: %v", err)
	}
	if got, err := svc.Draft(ctx, author.ID, replyKey); err != nil || got != current {
		t.Fatalf("failed submission lost draft: %+v %v", got, err)
	}
	if err := svc.SetThreadLock(ctx, owner.ID, tid, false, "Replies resumed"); err != nil {
		t.Fatal(err)
	}
	if threadID, postID, err := svc.SubmitDraft(ctx, author, replyKey, submit); err != nil || threadID != tid || postID == 0 {
		t.Fatalf("submitted draft: thread=%d post=%d err=%v", threadID, postID, err)
	}
	if err := svc.SaveDraft(ctx, author.ID, replyKey, current); !errors.Is(err, ErrDraftChanged) {
		t.Fatalf("late save restored submitted draft: %v", err)
	}
	if got, err := svc.Draft(ctx, author.ID, replyKey); err != nil || got.Body != "" || got.ReplyTo != 0 {
		t.Fatalf("submitted draft still stored as content: %+v %v", got, err)
	}
	if got, err := svc.Draft(ctx, author.ID, boardKey); err != nil || got != boardDraft {
		t.Fatalf("different board draft changed: %+v %v", got, err)
	}
	if err := svc.SaveDraft(ctx, author.ID, boardKey, Draft{Revision: svc.NextDraftRevision(boardDraft.Revision)}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveDraft(ctx, author.ID, boardKey, boardDraft); !errors.Is(err, ErrDraftChanged) {
		t.Fatalf("late save restored discarded draft: %v", err)
	}
}
