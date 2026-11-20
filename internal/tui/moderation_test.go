package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/identity"
)

func TestModerationKeysAndFlairs(t *testing.T) {
	ctx := context.Background()
	svc, _ := testService(t)
	ownerKey := identity.FingerprintFromBytes([32]byte{1})
	modKey := identity.FingerprintFromBytes([32]byte{2})
	if err := svc.ConfigureOwner(ctx, ownerKey); err != nil {
		t.Fatal(err)
	}
	owner, err := svc.Register(ctx, ownerKey, "", "Owner", "reef")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := svc.Register(ctx, modKey, "", "Helper", "reef")
	if err != nil {
		t.Fatal(err)
	}
	boards, err := svc.Boards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tid, err := svc.CreateThread(ctx, owner, boards[0].ID, "A reviewable thread", "Original content for review.")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.Thread(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	makeModel := func(a board.Account) Model {
		return New(Deps{Service: svc, Fingerprint: a.Fingerprint, Account: &a, Width: 64, Height: 20,
			Now: func() time.Time { return fixedNow }, NoSplash: true})
	}

	member := makeModel(mod)
	member.openProfile()
	if view := ansi.Strip(member.View().Content); !strings.Contains(view, modKey) {
		t.Fatalf("profile should show the full fingerprint at the minimum terminal size:\n%s", view)
	}
	member.curBoard, member.curThread, member.screen = boards[0], thread, scrThread
	member.posts, err = svc.Posts(ctx, tid, mod.ID)
	if err != nil || len(member.posts) != 1 {
		t.Fatalf("posts: %+v %v", member.posts, err)
	}
	if view := ansi.Strip(member.View().Content); !strings.Contains(view, "▐SU▌") || !strings.Contains(view, "▐OP▌") {
		t.Fatalf("opening post should show SU and OP flairs:\n%s", view)
	}
	member = drive(t, member, key("m"), key("!"), tea.PasteMsg{Content: "Off-topic"}, key("enter"))
	if member.screen != scrThread || member.overlay != ovNone {
		t.Fatalf("report flow ended at screen %v overlay %v", member.screen, member.overlay)
	}

	ownerUI := drive(t, makeModel(owner), key("m"), key("enter"))
	if ownerUI.mod.detail == nil || ownerUI.mod.detail.Reason != "Off-topic" {
		t.Fatalf("queue detail: %+v", ownerUI.mod.detail)
	}
	ownerUI = drive(t, ownerUI, key("esc"), key("4"), key("a"), tea.PasteMsg{Content: "Helper"}, key("tab"),
		tea.PasteMsg{Content: modKey}, key("enter"))
	if ownerUI.mod.tab != modAdmins || len(ownerUI.mod.admins) != 1 || ownerUI.mod.admins[0].ID != mod.ID {
		t.Fatalf("admin tab: %+v", ownerUI.mod)
	}

	staff := drive(t, member, eventMsgFor(board.Event{Kind: board.EventRole, AccountID: mod.ID}), key("m"), key("4"))
	if staff.screen != scrModeration || staff.mod.tab != modQueue || staff.acct.Role != board.RoleAdmin {
		t.Fatalf("new admin: screen=%v tab=%v role=%q", staff.screen, staff.mod.tab, staff.acct.Role)
	}
	staff = drive(t, staff, key("x"), tea.PasteMsg{Content: "Confirmed spam"}, key("enter"), key("3"))
	if len(staff.mod.entries) != 1 || staff.mod.entries[0].Action != board.Removed {
		t.Fatalf("removed tab: %+v", staff.mod.entries)
	}
	staff = drive(t, staff, key("u"), tea.PasteMsg{Content: "Restored on review"}, key("enter"), key("2"))
	if len(staff.mod.entries) != 3 || staff.mod.entries[0].Action != board.Restored {
		t.Fatalf("history tab: %+v", staff.mod.entries)
	}
}

func TestModerationReportMenusAndRestrictionReview(t *testing.T) {
	f := newChatFixture(t)
	msg := f.say(t, f.bob, "Reported chat line")
	member := drive(t, f.openChatModel(t, f.alice, 80, 24), key("tab"), key("o"))
	member.overlayT = 1
	member = clickOn(t, member, "option:1")
	if member.modForm.content != (board.ModerationTarget{Kind: board.ChatTarget, ID: msg.ID}) {
		t.Fatalf("chat report target: %+v", member.modForm)
	}
	member = drive(t, member, tea.PasteMsg{Content: "From chat options"}, key("enter"))
	member = drive(t, member, member.openUserProfile(f.bob.ID)(), key("o"), key("enter"))
	if member.modForm.content != (board.ModerationTarget{Kind: board.ProfileTarget, ID: f.bob.ID}) {
		t.Fatalf("profile report target: %+v", member.modForm)
	}
	member = drive(t, member, tea.PasteMsg{Content: "From profile options"}, key("enter"))
	owner := drive(t, modelFor(f.svc, f.owner, 80, 24), key("m"))
	if len(owner.mod.entries) != 2 || !strings.Contains(owner.mod.entries[0].Body, "Reported chat line") || owner.mod.entries[1].Reason != "From profile options" {
		t.Fatalf("staff queue: %+v", owner.mod.entries)
	}
	owner = drive(t, owner, owner.openUserProfile(f.bob.ID)(), key("o"), key("down"), key("enter"))
	if owner.modForm.action != board.TimedOut {
		t.Fatalf("restriction action: %+v", owner.modForm)
	}
	owner = drive(t, owner, tea.PasteMsg{Content: "Repeated spam"}, key("tab"), tea.PasteMsg{Content: "30m"}, key("enter"))
	if owner.overlay != ovNone {
		t.Fatalf("restriction submission: %s", owner.modForm.err)
	}
	player := drive(t, modelFor(f.svc, f.bob, 80, 24))
	if player.restriction == nil || !strings.Contains(screenText(player), "Account timed out") {
		t.Fatalf("reconnecting player's restriction: %s", screenText(player))
	}
	player = clickOn(t, player, "foot:contact")
	if !strings.Contains(ansi.Strip(player.viewModerationForm()), "Repeated spam") {
		t.Fatal("Contact staff did not show the restriction reason")
	}
	player = drive(t, player, tea.PasteMsg{Content: "Please review this"}, key("enter"))
	if player.overlay != ovNone {
		t.Fatalf("staff contact submission: %s", player.modForm.err)
	}
	queue, err := f.svc.ModerationPage(context.Background(), f.owner.ID, board.QueueView, 0)
	if err != nil || len(queue) != 3 || queue[2].Target.Kind != board.ContactTarget || queue[2].Body != "Please review this" {
		t.Fatalf("appeal queue: %+v %v", queue, err)
	}
}
