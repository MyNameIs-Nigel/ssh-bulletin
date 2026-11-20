package board

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mynameis-nigel/ssh-bulletin/internal/identity"
)

func moderationAccounts(t *testing.T, svc *Service) (owner, player, reporter Account, boards []Board) {
	t.Helper()
	ctx := context.Background()
	if err := svc.ConfigureOwner(ctx, identity.FingerprintFromBytes([32]byte{1})); err != nil {
		t.Fatal(err)
	}
	owner = registerTestKey(t, svc, 1, "Owner")
	player = registerTestKey(t, svc, 2, "Player")
	reporter = registerTestKey(t, svc, 3, "Reporter")
	boards, err := svc.Boards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return owner, player, reporter, boards
}

func TestReportEvidenceAndProfileDecisions(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	owner, player, reporter, boards := moderationAccounts(t, svc)
	send := func(a Account, b Board, text string) ChatMessage {
		t.Helper()
		msg, err := svc.SendChat(ctx, a, b.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		return msg
	}
	send(reporter, boards[0], "before line")
	incident := send(player, boards[0], "reported line")
	send(reporter, boards[1], "different board")
	send(reporter, boards[0], "after line")
	chat := ModerationTarget{Kind: ChatTarget, ID: incident.ID}
	if err := svc.ReportContent(ctx, reporter.ID, chat, "chat reason"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReportContent(ctx, reporter.ID, chat, "again"); !errors.Is(err, ErrAlreadyReported) {
		t.Fatalf("duplicate chat report: %v", err)
	}
	if err := svc.DeleteChat(ctx, player.ID, incident.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ModerationPage(ctx, player.ID, QueueView, 0); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member read queue: %v", err)
	}
	queue, err := svc.ModerationPage(ctx, owner.ID, QueueView, 0)
	if err != nil || len(queue) != 1 || queue[0].Target != chat || queue[0].Reason != "chat reason" {
		t.Fatalf("chat report = %+v, %v", queue, err)
	}
	for _, text := range []string{"before line", "[reported message]", "reported line", "after line"} {
		if !strings.Contains(queue[0].Body, text) {
			t.Fatalf("chat report missing %q: %s", text, queue[0].Body)
		}
	}
	if strings.Contains(queue[0].Body, "different board") {
		t.Fatal("report captured a different board's chat")
	}
	if err := svc.ReviewReport(ctx, player.ID, queue[0].ID, Dismissed, "bypass"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member review: %v", err)
	}
	if err := svc.ReviewReport(ctx, owner.ID, queue[0].ID, Dismissed, "Author deleted the message"); err != nil {
		t.Fatal(err)
	}
	second := send(player, boards[0], "remove from review")
	if err := svc.ReportContent(ctx, reporter.ID, ModerationTarget{Kind: ChatTarget, ID: second.ID}, "remove this"); err != nil {
		t.Fatal(err)
	}
	queue, err = svc.ModerationPage(ctx, owner.ID, QueueView, 0)
	if err != nil || len(queue) != 1 {
		t.Fatalf("queued chat removal report: %+v %v", queue, err)
	}
	if err := svc.ReviewReport(ctx, owner.ID, queue[0].ID, Removed, "Confirmed abuse"); err != nil {
		t.Fatal(err)
	}
	if msgs, _, err := svc.ChatHistory(ctx, boards[0].ID, 0, 20); err != nil || len(msgs) != 2 {
		t.Fatalf("public chat after removal: %+v %v", msgs, err)
	}
	if _, err := svc.UpdateProfile(ctx, player, player.Handle, player.Palette, "reported bio", ""); err != nil {
		t.Fatal(err)
	}
	profile := ModerationTarget{Kind: ProfileTarget, ID: player.ID}
	var id int64
	reportProfile := func() {
		t.Helper()
		if err := svc.ReportContent(ctx, reporter.ID, profile, "profile report"); err != nil {
			t.Fatal(err)
		}
		rows, err := svc.ModerationPage(ctx, owner.ID, QueueView, 0)
		if err != nil || len(rows) != 1 || rows[0].Target != profile {
			t.Fatalf("profile queue = %+v, %v", rows, err)
		}
		id = rows[0].ID
	}
	reportProfile()
	if _, err := svc.UpdateProfile(ctx, player, player.Handle, player.Palette, "replacement bio", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReviewReport(ctx, owner.ID, id, BioCleared, "Stale review"); !errors.Is(err, ErrReviewChanged) {
		t.Fatalf("changed bio decision: %v", err)
	}
	if err := svc.ReviewReport(ctx, owner.ID, id, Dismissed, "Bio changed"); err != nil {
		t.Fatal(err)
	}
	reportProfile()
	if err := svc.ReviewReport(ctx, owner.ID, id, BioCleared, "Confirmed bio abuse"); err != nil {
		t.Fatal(err)
	}
	if a, err := svc.Account(ctx, player.ID); err != nil || a.Bio != "" {
		t.Fatalf("cleared profile: %+v, %v", a, err)
	}
	reportProfile()
	if err := svc.ReviewReport(ctx, owner.ID, id, HandleChangeRequired, "Inappropriate handle"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendChat(ctx, player, boards[0].ID, "with old handle"); !errors.Is(err, ErrHandleChangeRequired) {
		t.Fatalf("chat without rename: %v", err)
	}
	if _, err := svc.UpdateProfile(ctx, player, strings.ToUpper(player.Handle), player.Palette, "", ""); !errors.Is(err, ErrHandleChangeRequired) {
		t.Fatalf("case-only change cleared restriction: %v", err)
	}
	if a, err := svc.UpdateProfile(ctx, player, "NewPlayer", player.Palette, "", ""); err != nil || a.HandleChangeRequired {
		t.Fatalf("rename: %+v, %v", a, err)
	}
	history, err := svc.ModerationPage(ctx, owner.ID, HistoryView, 0)
	if err != nil || len(history) != 10 || history[0].Action != HandleChangeRequired || history[2].Action != BioCleared || history[2].Body != "replacement bio" {
		t.Fatalf("review history: %+v, %v", history, err)
	}
}

func TestRestrictionsBlockExistingSessionWritesAndAllowReview(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	now := svc.now()
	svc.now = func() time.Time { return now }
	owner, player, reporter, boards := moderationAccounts(t, svc)
	boardID := boards[0].ID
	tid, err := svc.CreateThread(ctx, player, boardID, "Existing thread", "A post")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.Thread(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	posts, err := svc.Posts(ctx, tid, player.ID)
	if err != nil || len(posts) != 1 {
		t.Fatalf("original post: %+v %v", posts, err)
	}
	post := posts[0]
	chat, err := svc.SendChat(ctx, player, boardID, "A message")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRestriction(ctx, reporter.ID, player.ID, TimedOut, "member action", time.Minute); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member imposed restriction: %v", err)
	}
	if err := svc.SetRestriction(ctx, owner.ID, player.ID, TimedOut, "Cooldown", time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, op := range []struct {
		name  string
		write func() error
	}{
		{"new thread", func() error { _, err := svc.CreateThread(ctx, player, boardID, "New", "Body"); return err }},
		{"reply", func() error { _, err := svc.Reply(ctx, player, thread, "reply"); return err }},
		{"chat", func() error { _, err := svc.SendChat(ctx, player, boardID, "blocked"); return err }},
		{"kudos", func() error { _, err := svc.ToggleKudos(ctx, player, post); return err }},
		{"profile", func() error { _, err := svc.UpdateProfile(ctx, player, "Changed", player.Palette, "", ""); return err }},
		{"friend", func() error { return svc.SetFriend(ctx, player.ID, reporter.ID, true) }},
		{"report", func() error { return svc.Report(ctx, player.ID, post.ID, "another report") }},
		{"delete post", func() error { return svc.DeletePost(ctx, player.ID, post.ID) }},
		{"delete chat", func() error { return svc.DeleteChat(ctx, player.ID, chat.ID) }},
	} {
		err := op.write()
		var r *AccountRestriction
		if !errors.As(err, &r) || r.Kind != TimedOut || r.Reason != "Cooldown" || r.ExpiresAt != now.Add(time.Minute).Unix() {
			t.Errorf("%s allowed or returned the wrong restriction: %v", op.name, err)
		}
	}
	if err := svc.ContactStaff(ctx, player.ID, "Please review my timeout"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ContactStaff(ctx, player.ID, "Duplicate"); !errors.Is(err, ErrContactLimit) {
		t.Fatalf("staff contact rate limit: %v", err)
	}
	queue, err := svc.ModerationPage(ctx, owner.ID, QueueView, 0)
	if err != nil || len(queue) != 1 || queue[0].Target.Kind != ContactTarget || queue[0].Body != "Please review my timeout" {
		t.Fatalf("appeal queue: %+v %v", queue, err)
	}
	now = now.Add(time.Minute)
	if _, err := svc.Reply(ctx, player, thread, "Accepted at expiry"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRestriction(ctx, owner.ID, player.ID, Suspended, "Repeated abuse", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRestriction(ctx, owner.ID, player.ID, RestrictionLifted, "Appeal accepted", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendChat(ctx, player, boardID, "After lift"); err != nil {
		t.Fatal(err)
	}
	history, err := svc.ModerationPage(ctx, owner.ID, HistoryView, 0)
	if err != nil || len(history) != 4 || history[0].Action != RestrictionLifted || history[0].ExpiresAt != now.Add(time.Hour).Unix() || history[0].Actor != owner.Handle || history[0].Reason != "Appeal accepted" {
		t.Fatalf("restriction history: %+v %v", history, err)
	}
}
