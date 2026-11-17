package board

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return NewService(st, NewBroker(), func() time.Time { return time.Unix(1_800_000_000, 0) })
}

func TestRegisterAndPostFlow(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	a, err := svc.Register(ctx, "SHA256:a", "", "Alice", "reef")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Register(ctx, "SHA256:b", "", "alice", "reef"); !errors.Is(err, ErrHandleTaken) {
		t.Fatalf("case-insensitive handle clash: %v", err)
	}
	if _, err := svc.Register(ctx, "SHA256:b", "", "x", "reef"); !errors.Is(err, ErrHandleInvalid) {
		t.Fatalf("short handle: %v", err)
	}
	boards, _ := svc.Boards(ctx)
	if len(boards) != 5 {
		t.Fatalf("seeded boards = %d", len(boards))
	}
	events, unsub := svc.Broker().Subscribe()
	defer unsub()

	tid, err := svc.CreateThread(ctx, a, boards[0].ID, "  Hello\x1b[31m  ", "first\x07 post")
	if err != nil {
		t.Fatal(err)
	}
	if ev := <-events; ev.Kind != EventThread || ev.ThreadID != tid || ev.Actor != "Alice" {
		t.Fatalf("event = %+v", ev)
	}
	th, _ := svc.Thread(ctx, tid)
	if th.Title != "Hello[31m" {
		t.Fatalf("title not cleaned: %q", th.Title)
	}
	if _, err := svc.Reply(ctx, a, th, "   "); !errors.Is(err, ErrEmptyBody) {
		t.Fatalf("empty reply: %v", err)
	}
	if _, err := svc.Reply(ctx, Account{}, th, "x"); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("anonymous reply: %v", err)
	}
	pid, err := svc.Reply(ctx, a, th, "second")
	if err != nil {
		t.Fatal(err)
	}
	posts, _ := svc.Posts(ctx, tid, a.ID)
	if len(posts) != 2 || posts[1].ID != pid {
		t.Fatalf("posts = %+v", posts)
	}
	on, _ := svc.ToggleKudos(ctx, a, posts[1])
	posts, _ = svc.Posts(ctx, tid, a.ID)
	if !on || posts[1].Kudos != 1 || !posts[1].KudosByViewer {
		t.Fatalf("kudos on: %+v", posts[1])
	}
	on, _ = svc.ToggleKudos(ctx, a, posts[1])
	posts, _ = svc.Posts(ctx, tid, a.ID)
	if on || posts[1].Kudos != 0 {
		t.Fatalf("kudos off: %+v", posts[1])
	}
	th, _ = svc.Thread(ctx, tid)
	if th.ReplyCount != 1 {
		t.Fatalf("reply count = %d", th.ReplyCount)
	}
}

func TestBrokerDropsInsteadOfBlocking(t *testing.T) {
	b := NewBroker()
	_, unsub := b.Subscribe()
	for range 100 { // never read: must not block
		b.Publish(Event{Kind: EventReply})
	}
	unsub()
	unsub() // idempotent
	if b.Subscribers() != 0 {
		t.Fatal("unsubscribe leaked")
	}
}
