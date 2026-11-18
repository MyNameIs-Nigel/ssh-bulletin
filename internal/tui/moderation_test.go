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
