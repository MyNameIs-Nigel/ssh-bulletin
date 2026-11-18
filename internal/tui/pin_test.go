package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/identity"
)

func pinFixture(t *testing.T) (svc *board.Service, owner, member board.Account, b board.Board) {
	t.Helper()
	ctx := context.Background()
	svc, _ = testService(t)
	ownerKey := identity.FingerprintFromBytes([32]byte{1})
	if err := svc.ConfigureOwner(ctx, ownerKey); err != nil {
		t.Fatal(err)
	}
	var err error
	if owner, err = svc.Register(ctx, ownerKey, "", "Owner", "reef"); err != nil {
		t.Fatal(err)
	}
	if member, err = svc.Register(ctx, identity.FingerprintFromBytes([32]byte{2}), "", "Member", "reef"); err != nil {
		t.Fatal(err)
	}
	boards, err := svc.Boards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Weekend plans", "Show your terminal"} {
		if _, err := svc.CreateThread(ctx, member, boards[0].ID, title, "Chat."); err != nil {
			t.Fatal(err)
		}
	}
	return svc, owner, member, boards[0]
}

func modelFor(svc *board.Service, a board.Account, w, h int) Model {
	return New(Deps{Service: svc, Fingerprint: a.Fingerprint, Account: &a, Width: w, Height: h,
		Now: func() time.Time { return fixedNow }, NoSplash: true})
}

func TestOnlyStaffGetThreadOptions(t *testing.T) {
	svc, _, member, _ := pinFixture(t)
	m := drive(t, modelFor(svc, member, 100, 30), key("enter"), key("o"))
	if m.screen != scrBoard || m.overlay != ovNone {
		t.Fatalf("a member must not get an options menu: screen=%v overlay=%v", m.screen, m.overlay)
	}
	if strings.Contains(ansi.Strip(m.View().Content), "options") {
		t.Fatal("a member should not see the options hint")
	}
}

func TestAdminPinsThroughOptionsMenuAndCursorFollows(t *testing.T) {
	ctx := context.Background()
	svc, owner, _, b := pinFixture(t)
	m := drive(t, modelFor(svc, owner, 100, 30), key("enter"), key("down"), key("down"))
	if m.screen != scrBoard || len(m.threads) < 3 {
		t.Fatalf("screen=%v threads=%d", m.screen, len(m.threads))
	}
	target := m.threads[2]
	if target.Pinned {
		t.Fatal("fixture thread should start unpinned")
	}
	m = drive(t, m, key("o"))
	if m.overlay != ovOptions {
		t.Fatalf("o should open the options menu, overlay=%v", m.overlay)
	}
	m.overlayT = 1
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "Pin to top of board") {
		t.Fatalf("options menu should offer pinning:\n%s", v)
	}
	m = drive(t, m, key("enter"),
		eventMsgFor(board.Event{Kind: board.EventModeration, BoardID: b.ID, ThreadID: target.ID}))
	if m.overlay != ovNone {
		t.Fatalf("overlay should close after choosing, got %v", m.overlay)
	}
	if got, _ := svc.Thread(ctx, target.ID); !got.Pinned {
		t.Fatal("thread was not pinned in the store")
	}
	if m.threads[0].ID != target.ID || !m.threads[0].Pinned {
		t.Fatalf("pinned thread should lead the list: %+v", m.threads[0])
	}
	if i := m.threadMenu.Selected(); i != 0 {
		t.Fatalf("cursor should follow the thread to the top, is on row %d", i)
	}
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "PINNED") {
		t.Fatalf("board view should show PINNED:\n%s", v)
	}

	// Choosing again acts on the same thread and releases it.
	m = drive(t, m, key("o"))
	m.overlayT = 1
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "Unpin from board") {
		t.Fatalf("options menu should now offer unpinning:\n%s", v)
	}
	m = drive(t, m, key("enter"),
		eventMsgFor(board.Event{Kind: board.EventModeration, BoardID: b.ID, ThreadID: target.ID}))
	if got, _ := svc.Thread(ctx, target.ID); got.Pinned {
		t.Fatal("thread was not unpinned")
	}
}

func TestPinnedIndicatorsSurviveSmallTerminals(t *testing.T) {
	ctx := context.Background()
	svc, owner, _, b := pinFixture(t)
	long := strings.Repeat("An extremely long and wordy title ", 3)[:board.MaxTitleLen]
	tid, err := svc.CreateThread(ctx, owner, b.ID, long, "Read this before posting.")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPinned(ctx, owner.ID, tid, true); err != nil {
		t.Fatal(err)
	}
	for _, sz := range [][2]int{{64, 20}, {80, 24}, {100, 30}, {160, 50}} {
		for _, keys := range [][]string{{"enter"}, {"enter", "enter"}, {}} {
			msgs := []tea.Msg{tea.WindowSizeMsg{Width: sz[0], Height: sz[1]}}
			for _, k := range keys {
				msgs = append(msgs, key(k))
			}
			m := drive(t, modelFor(svc, owner, sz[0], sz[1]), msgs...)
			out := m.View().Content
			if lipgloss.Height(out) != sz[1] {
				t.Errorf("%v %v: height %d", sz, keys, lipgloss.Height(out))
			}
			for i, l := range strings.Split(out, "\n") {
				if lipgloss.Width(l) != sz[0] {
					t.Errorf("%v %v: row %d width %d", sz, keys, i, lipgloss.Width(l))
					break
				}
			}
			plain := ansi.Strip(out)
			if len(keys) > 0 && !strings.Contains(plain, "PINNED") {
				t.Errorf("%v %v: PINNED indicator missing:\n%s", sz, keys, plain)
			}
		}
	}
}

func TestThirdPinIsRefusedWithAMessage(t *testing.T) {
	ctx := context.Background()
	svc, owner, _, b := pinFixture(t)
	ts, err := svc.Threads(ctx, b.ID)
	if err != nil || len(ts) < 3 {
		t.Fatalf("threads: %d %v", len(ts), err)
	}
	for _, th := range ts[:2] {
		if err := svc.SetPinned(ctx, owner.ID, th.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	m := drive(t, modelFor(svc, owner, 100, 30), key("enter"), key("down"), key("down"), key("o"), key("enter"))
	if m.overlay != ovNone {
		t.Fatalf("the menu should close after a refusal, overlay=%v", m.overlay)
	}
	if !strings.Contains(m.flash, "already has 2 pinned") {
		t.Fatalf("flash = %q", m.flash)
	}
	if got, _ := svc.Thread(ctx, ts[2].ID); got.Pinned {
		t.Fatal("third thread must stay unpinned")
	}
}
