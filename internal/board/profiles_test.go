package board

import (
	"context"
	"errors"
	"testing"
)

func TestDirectoryMatchesUsernamesAndKeepsFriendsPrivate(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	alice := registerTestKey(t, svc, 1, "Alice")
	prefix := registerTestKey(t, svc, 2, "Alice_2")
	suffix := registerTestKey(t, svc, 3, "Z-Alice")
	bob := registerTestKey(t, svc, 4, "Bob")
	if _, err := svc.UpdateProfile(ctx, bob, bob.Handle, bob.Palette, "Alice's friend", ""); err != nil {
		t.Fatal(err)
	}
	entries, _, err := svc.SearchProfiles(ctx, bob.ID, " @aLiCe ", false)
	if err != nil || len(entries) != 3 || entries[0].ID != alice.ID || entries[1].ID != prefix.ID || entries[2].ID != suffix.ID {
		t.Fatalf("directory matches: %+v, %v", entries, err)
	}
	entries, _, err = svc.SearchProfiles(ctx, bob.ID, "_", false)
	if err != nil || len(entries) != 1 || entries[0].ID != prefix.ID {
		t.Fatalf("literal underscore match: %+v, %v", entries, err)
	}
	if err := svc.SetFriend(ctx, bob.ID, bob.ID, true); !errors.Is(err, ErrSelfFriend) {
		t.Fatalf("self friendship: %v", err)
	}
	for range 2 {
		if err := svc.SetFriend(ctx, bob.ID, alice.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	entries, _, err = svc.SearchProfiles(ctx, bob.ID, "", true)
	if err != nil || len(entries) != 1 || entries[0].ID != alice.ID {
		t.Fatalf("Bob's friends: %+v, %v", entries, err)
	}
	entries, _, err = svc.SearchProfiles(ctx, alice.ID, "", true)
	if err != nil || len(entries) != 0 {
		t.Fatalf("Alice's friends: %+v, %v", entries, err)
	}
	if err := svc.SetFriend(ctx, bob.ID, alice.ID, false); err != nil {
		t.Fatal(err)
	}
	if ids, err := svc.FriendIDs(ctx, bob.ID); err != nil || len(ids) != 0 {
		t.Fatalf("Bob's remaining friends: %+v, %v", ids, err)
	}
}

func TestPostFlairSnapshotsAndVisibleActivity(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	alice := registerTestKey(t, svc, 1, "Alice")
	if err := svc.ConfigureOwner(ctx, alice.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateProfile(ctx, alice, alice.Handle, alice.Palette, "", "SU"); !errors.Is(err, ErrFlairInvalid) {
		t.Fatalf("staff label accepted as a personal flair: %v", err)
	}
	setFlair := func(flair string) {
		t.Helper()
		if _, err := svc.UpdateProfile(ctx, alice, alice.Handle, alice.Palette, "", flair); err != nil {
			t.Fatal(err)
		}
	}
	setFlair("Arcade")
	boards, err := svc.Boards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tid, err := svc.CreateThread(ctx, alice, boards[0].ID, "A post", "Opening")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.Thread(ctx, tid)
	if err != nil || thread.Flair != "Arcade" {
		t.Fatalf("opening flair: %q, %v", thread.Flair, err)
	}
	setFlair("Builder")
	pid, err := svc.Reply(ctx, alice, thread, "A reply")
	if err != nil {
		t.Fatal(err)
	}
	setFlair("")
	posts, err := svc.Posts(ctx, tid, alice.ID)
	if err != nil || len(posts) != 2 || posts[0].Flair != "Arcade" || posts[1].Flair != "Builder" {
		t.Fatalf("post flairs: %+v, %v", posts, err)
	}
	profile, err := svc.Profile(ctx, alice.ID)
	if err != nil || profile.Flair != "" || len(profile.Recent) != 2 || !profile.Recent[0].IsReply || profile.Recent[0].PostID != pid {
		t.Fatalf("recent profile activity: %+v, %v", profile, err)
	}
	if err := svc.Moderate(ctx, alice.ID, posts[0].ID, Removed, "Remove thread"); err != nil {
		t.Fatal(err)
	}
	profile, err = svc.Profile(ctx, alice.ID)
	if err != nil || profile.PostCount != 1 || len(profile.Recent) != 1 || profile.Recent[0].PostID != pid || profile.Recent[0].Title != "< removed >" {
		t.Fatalf("profile includes removed content: %+v, %v", profile, err)
	}
}
