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
)

func newTestModel(t *testing.T, acct *board.Account, w, h int) (Model, *board.Service) {
	svc, _ := testService(t)
	m := New(Deps{Service: svc, Fingerprint: "SHA256:new", Account: acct, Width: w, Height: h,
		Now: func() time.Time { return fixedNow }, NoSplash: true})
	return drive(t, m, tea.WindowSizeMsg{Width: w, Height: h}), svc
}

// Every screen fills the terminal exactly, at every supported size.
func TestEveryFrameIsExactlyTheTerminal(t *testing.T) {
	svc, _ := testService(t)
	acct, _, _ := svc.Lookup(context.Background(), "SHA256:demo-0")
	for _, sz := range [][2]int{{64, 20}, {80, 24}, {100, 30}, {160, 50}, {40, 10}} {
		for _, keys := range [][]string{nil, {"enter"}, {"enter", "enter"}, {"n"}, {"/"}, {"?"}, {"p"}, {"enter", "tab"}, {"enter", "tab", "enter"}} {
			m := New(Deps{Service: svc, Fingerprint: "SHA256:demo-0", Account: &acct, Width: sz[0], Height: sz[1],
				Now: func() time.Time { return fixedNow }, NoSplash: true})
			msgs := []tea.Msg{tea.WindowSizeMsg{Width: sz[0], Height: sz[1]}}
			for _, k := range keys {
				msgs = append(msgs, key(k))
			}
			m = drive(t, m, msgs...)
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
		}
	}
}

func TestOnboardingCreatesAccount(t *testing.T) {
	m, svc := newTestModel(t, nil, 100, 30)
	if m.screen != scrOnboard {
		t.Fatalf("screen = %v", m.screen)
	}
	msgs := []tea.Msg{}
	for _, r := range "newbie" {
		msgs = append(msgs, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	msgs = append(msgs, key("tab"), key("right"), key("enter"))
	m = drive(t, m, msgs...)
	if m.acct == nil || m.acct.Handle != "newbie" || m.acct.Palette != "sunset" {
		t.Fatalf("account = %+v", m.acct)
	}
	if m.screen != scrHome {
		t.Fatalf("after onboarding screen = %v", m.screen)
	}
	if _, ok, _ := svc.Lookup(context.Background(), "SHA256:new"); !ok {
		t.Fatal("account not persisted")
	}
}

func TestComposeReplyAndKudos(t *testing.T) {
	svc, _ := testService(t)
	acct, _, _ := svc.Lookup(context.Background(), "SHA256:demo-0")
	m := New(Deps{Service: svc, Fingerprint: acct.Fingerprint, Account: &acct, Width: 120, Height: 40,
		Now: func() time.Time { return fixedNow }, NoSplash: true})
	m = drive(t, m, tea.WindowSizeMsg{Width: 120, Height: 40}, key("enter"), key("enter"))
	if m.screen != scrThread || len(m.posts) == 0 {
		t.Fatalf("screen=%v posts=%d", m.screen, len(m.posts))
	}
	before := len(m.posts)
	msgs := []tea.Msg{key("r")}
	for _, r := range "a reply" {
		msgs = append(msgs, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	msgs = append(msgs, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = drive(t, m, msgs...)
	if m.overlay != ovNone || len(m.posts) != before+1 || m.posts[len(m.posts)-1].Body != "a reply" {
		t.Fatalf("overlay=%v posts=%d", m.overlay, len(m.posts))
	}
	m = drive(t, m, key("f"))
	if !m.posts[m.postCursor].KudosByViewer {
		t.Fatal("kudos not applied")
	}
}

func TestPaletteJumpsToBoard(t *testing.T) {
	svc, _ := testService(t)
	acct, _, _ := svc.Lookup(context.Background(), "SHA256:demo-0")
	m := New(Deps{Service: svc, Account: &acct, Width: 100, Height: 30, Now: func() time.Time { return fixedNow }, NoSplash: true})
	msgs := []tea.Msg{tea.WindowSizeMsg{Width: 100, Height: 30}, key("/")}
	for _, r := range "wkshp" {
		msgs = append(msgs, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	msgs = append(msgs, key("enter"))
	m = drive(t, m, msgs...)
	if m.screen != scrBoard || m.curBoard.Slug != "workshop" {
		t.Fatalf("screen=%v board=%q", m.screen, m.curBoard.Slug)
	}
}

func TestLiveEventFromOthersToasts(t *testing.T) {
	svc, _ := testService(t)
	acct, _, _ := svc.Lookup(context.Background(), "SHA256:demo-0")
	m := New(Deps{Service: svc, Account: &acct, Width: 100, Height: 30, Now: func() time.Time { return fixedNow }, NoSplash: true})
	m = drive(t, m, tea.WindowSizeMsg{Width: 100, Height: 30},
		eventMsgFor(board.Event{Kind: board.EventThread, Actor: "someone", Title: "hi"}),
		eventMsgFor(board.Event{Kind: board.EventThread, Actor: acct.Handle, Title: "mine"}))
	if m.toasts.Len() != 1 {
		t.Fatalf("toasts = %d, want 1 (own posts don't toast)", m.toasts.Len())
	}
}

// The animation loop must go quiet: an idle session costs no bandwidth.
func TestAnimationLoopStops(t *testing.T) {
	m, _ := newTestModel(t, nil, 100, 30)
	m.screen = scrHome
	now := fixedNow
	m.now = func() time.Time { return now }
	m.accentT = 1
	m.focusAt = now.Add(-time.Hour)
	m.boardMenu.Snap()
	m.threadMenu.Snap()
	if m.animating() {
		t.Fatal("a resting home screen is still animating")
	}
}

func eventMsgFor(e board.Event) tea.Msg { return liveEvent(e) }

// Onboarding animates after input, then settles: an abandoned onboarding
// screen must not stream frames until the idle timeout. (The splash is
// exempt: it keeps moving until a key is pressed.)
func TestAmbientScreensSettle(t *testing.T) {
	for _, scr := range []screen{scrOnboard} {
		m, _ := newTestModel(t, nil, 100, 30)
		m.screen = scr
		now := fixedNow
		m.now = func() time.Time { return now }
		m.accentT = 1
		m.focusAt = now.Add(-time.Hour)
		m.boardMenu.Snap()
		m.threadMenu.Snap()
		m.lastInput = now.Add(-time.Second)
		if !m.animating() {
			t.Errorf("screen %v: not animating right after input", scr)
		}
		m.lastInput = now.Add(-time.Minute)
		if m.animating() {
			t.Errorf("screen %v: still animating a minute after the last input", scr)
		}
	}
}

// A panel's top corner must sit in the same column as its sides. Indenting a
// joined block with a leading " " shifts only its first row, which is how the
// top edges once drifted one column right of the borders below them.
func TestPanelEdgesAlign(t *testing.T) {
	svc, _ := testService(t)
	acct, _, _ := svc.Lookup(context.Background(), "SHA256:demo-0")
	for _, keys := range [][]string{nil, {"enter"}} {
		m := New(Deps{Service: svc, Fingerprint: "SHA256:demo-0", Account: &acct, Width: 111, Height: 32,
			Now: func() time.Time { return fixedNow }, NoSplash: true})
		msgs := []tea.Msg{tea.WindowSizeMsg{Width: 111, Height: 32}}
		for _, k := range keys {
			msgs = append(msgs, key(k))
		}
		m = drive(t, m, msgs...)
		rows := strings.Split(ansi.Strip(m.View().Content), "\n")
		top, side := []rune(rows[2]), []rune(rows[3])
		for x, r := range top {
			if r != '╭' && r != '╮' {
				continue
			}
			if x >= len(side) || side[x] != '│' {
				t.Errorf("%v: corner %q at column %d has no side border under it: %q", keys, r, x, string(side))
			}
		}
	}
}
