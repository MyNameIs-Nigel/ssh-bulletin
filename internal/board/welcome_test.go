package board

import (
	"context"
	"strings"
	"testing"
)

func TestWelcomeSeedsRealPinnedThreadAndPreservesExistingPins(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	owner, player, _, boards := moderationAccounts(t, svc)
	a := registerTestKey(t, svc, 4, "Bulletin") // existing handle must keep its identity
	for _, title := range []string{"First sticky", "Second sticky"} {
		id, err := svc.CreateThread(ctx, player, boards[0].ID, title, "Existing content")
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.SetPinned(ctx, owner.ID, id, true); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := svc.SeedWelcome(ctx); err != nil {
			t.Fatal(err)
		}
	}
	threads, err := svc.Threads(ctx, boards[0].ID)
	if err != nil || len(threads) != 3 || threads[0].Title != welcomeTitle || !threads[0].Pinned || !threads[1].Pinned || !threads[2].Pinned || threads[0].AuthorID == a.ID {
		t.Fatalf("welcome and existing pins: %+v, %v", threads, err)
	}
	posts, err := svc.Posts(ctx, threads[0].ID, player.ID)
	if err != nil || len(posts) != 1 || !strings.Contains(posts[0].Body, "Community rules") || !strings.Contains(posts[0].Body, "Contact staff") {
		t.Fatalf("welcome post: %+v, %v", posts, err)
	}
	if _, err := svc.Reply(ctx, player, threads[0], "Hi, I play Moon Miner."); err != nil {
		t.Fatal(err)
	}
}
