package board

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/mynameis-nigel/ssh-bulletin/internal/identity"
	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
)

// newClockSvc is a service whose clock the test advances, so quotas can be
// exercised and stepped past.
func newClockSvc(t *testing.T) (*Service, *time.Time) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Unix(1_800_000_000, 0)
	return NewService(st, NewBroker(), func() time.Time { return now }), &now
}

func TestChatSendCleansAndBroadcasts(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	alice := registerTestKey(t, svc, 1, "Alice")
	boards, _ := svc.Boards(ctx)
	events, unsub := svc.Broker().Subscribe()
	defer unsub()

	msg, err := svc.SendChat(ctx, alice, boards[1].ID, "  hi\x1b[31m there\nfriend‮ ")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Body != "hi[31m there friend" || msg.AuthorHandle != "Alice" || msg.BoardID != boards[1].ID {
		t.Fatalf("message = %+v", msg)
	}
	if ev := <-events; ev.Kind != EventChat || ev.BoardID != boards[1].ID || ev.MessageID != msg.ID || ev.Actor != "Alice" {
		t.Fatalf("event = %+v", ev)
	}
	for _, tc := range []struct {
		acct Account
		body string
		want error
	}{
		{Account{}, "hi", ErrNoAccount},
		{alice, " \t\n", ErrEmptyBody},
	} {
		if _, err := svc.SendChat(ctx, tc.acct, boards[1].ID, tc.body); !errors.Is(err, tc.want) {
			t.Errorf("SendChat(%q) = %v, want %v", tc.body, err, tc.want)
		}
	}
	long := fmt.Sprintf("%0*d", MaxChatLen+1, 0)
	if _, err := svc.SendChat(ctx, alice, boards[1].ID, long); !errors.Is(err, ErrTooLong) {
		t.Errorf("long message: %v", err)
	}
	if _, err := svc.SendChat(ctx, alice, 9999, "hi"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown board: %v", err)
	}
	// Chats are per board.
	if other, _ := svc.ChatSince(ctx, boards[0].ID, 0); len(other) != 0 {
		t.Fatalf("message leaked to another board: %+v", other)
	}
}

func TestChatQuota(t *testing.T) {
	ctx := context.Background()
	svc, now := newClockSvc(t)
	alice := registerTestKey(t, svc, 1, "Alice")
	boards, _ := svc.Boards(ctx)
	*now = now.Add(48 * time.Hour) // past the newcomer window
	for i := range 5 {
		if _, err := svc.SendChat(ctx, alice, boards[0].ID, fmt.Sprint("m", i)); err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
	}
	if _, err := svc.SendChat(ctx, alice, boards[0].ID, "one too many"); !errors.Is(err, ErrChatLimit) {
		t.Fatalf("burst: %v", err)
	}
	*now = now.Add(11 * time.Second)
	if _, err := svc.SendChat(ctx, alice, boards[0].ID, "after the burst window"); err != nil {
		t.Fatalf("after burst window: %v", err)
	}
	// Six sent so far (t=0 and t=11). Bursts at t=22, 33 and 44 bring the
	// rolling minute to 20; at t=55 the burst window is empty but every
	// message is still inside the minute.
	send := func(n int) {
		t.Helper()
		*now = now.Add(11 * time.Second)
		for i := range n {
			if _, err := svc.SendChat(ctx, alice, boards[0].ID, "x"); err != nil {
				t.Fatalf("at %d: %v", i, err)
			}
		}
	}
	send(5)
	send(5)
	send(4)
	*now = now.Add(11 * time.Second)
	if _, err := svc.SendChat(ctx, alice, boards[0].ID, "minute cap"); !errors.Is(err, ErrChatLimit) {
		t.Fatalf("per-minute cap: %v", err)
	}
}

func TestChatHistoryPagesAndCatchUp(t *testing.T) {
	ctx := context.Background()
	svc, now := newClockSvc(t)
	alice := registerTestKey(t, svc, 1, "Alice")
	boards, _ := svc.Boards(ctx)
	b := boards[2].ID
	*now = now.Add(48 * time.Hour)
	var ids []int64
	for i := range 130 {
		*now = now.Add(4 * time.Second) // stays under every quota
		m, err := svc.SendChat(ctx, alice, b, fmt.Sprint("line ", i))
		if err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		ids = append(ids, m.ID)
	}
	page, more, err := svc.ChatHistory(ctx, b, 0, ChatPage)
	if err != nil || !more || len(page) != ChatPage || page[0].Body != "line 30" || page[ChatPage-1].Body != "line 129" {
		t.Fatalf("newest page: more=%v len=%d err=%v first=%q", more, len(page), err, page[0].Body)
	}
	older, more, err := svc.ChatHistory(ctx, b, page[0].ID, ChatPage)
	if err != nil || more || len(older) != 30 || older[0].Body != "line 0" || older[29].Body != "line 29" {
		t.Fatalf("older page: more=%v len=%d err=%v", more, len(older), err)
	}
	since, err := svc.ChatSince(ctx, b, ids[127])
	if err != nil || len(since) != 2 || since[0].ID != ids[128] {
		t.Fatalf("since: %+v %v", since, err)
	}
}

func TestChatDeleteAndRemove(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	ownerKey := identity.FingerprintFromBytes([32]byte{9})
	if err := svc.ConfigureOwner(ctx, ownerKey); err != nil {
		t.Fatal(err)
	}
	owner, err := svc.Register(ctx, ownerKey, "", "Owner", "reef")
	if err != nil {
		t.Fatal(err)
	}
	alice := registerTestKey(t, svc, 1, "Alice")
	bob := registerTestKey(t, svc, 2, "Bob")
	boards, _ := svc.Boards(ctx)
	b := boards[0].ID
	a1, _ := svc.SendChat(ctx, alice, b, "mine")
	a2, _ := svc.SendChat(ctx, alice, b, "rude")
	keep, _ := svc.SendChat(ctx, bob, b, "stays")

	events, unsub := svc.Broker().Subscribe()
	defer unsub()
	if err := svc.DeleteChat(ctx, bob.ID, a1.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("deleting someone else's message: %v", err)
	}
	if err := svc.RemoveChat(ctx, bob.ID, a2.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member removing: %v", err)
	}
	if err := svc.DeleteChat(ctx, alice.ID, a1.ID); err != nil {
		t.Fatal(err)
	}
	if ev := <-events; ev.Kind != EventChatRemoved || ev.MessageID != a1.ID || ev.BoardID != b {
		t.Fatalf("delete event = %+v", ev)
	}
	if err := svc.RemoveChat(ctx, owner.ID, a2.ID); err != nil {
		t.Fatal(err)
	}
	if ev := <-events; ev.Kind != EventChatRemoved || ev.MessageID != a2.ID {
		t.Fatalf("remove event = %+v", ev)
	}
	if err := svc.DeleteChat(ctx, alice.ID, a2.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting a removed message: %v", err)
	}
	if err := svc.RemoveChat(ctx, owner.ID, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removing a missing message: %v", err)
	}
	msgs, _, err := svc.ChatHistory(ctx, b, 0, ChatPage)
	if err != nil || len(msgs) != 1 || msgs[0].ID != keep.ID {
		t.Fatalf("visible after delete/remove: %+v %v", msgs, err)
	}
}
