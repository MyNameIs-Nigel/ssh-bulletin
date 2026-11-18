package tui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

// fixedNow pins the clock so renders are deterministic.
var fixedNow = time.Date(2026, 11, 16, 20, 0, 0, 0, time.UTC)

func testService(t *testing.T) (*board.Service, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	clock := func() time.Time { return fixedNow.Add(-2 * time.Hour) }
	svc := board.NewService(st, board.NewBroker(), clock)
	if err := board.SeedDemo(context.Background(), svc, st); err != nil {
		t.Fatal(err)
	}
	return svc, st
}

// drive runs Init and feeds msgs, resolving commands synchronously (one
// level of batching) so data loads land before the snapshot.
func drive(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	run := func(cmd tea.Cmd) {}
	var apply func(tea.Msg)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- cmd() }()
		select {
		case msg := <-done:
			apply(msg)
		case <-time.After(150 * time.Millisecond):
		}
	}
	apply = func(msg tea.Msg) {
		switch msg := msg.(type) {
		case nil:
		case tea.BatchMsg:
			for _, c := range msg {
				run(c)
			}
		case liveEvent:
			next, cmd := m.Update(eventMsg(msg))
			m = next.(Model)
			run(cmd)
		case eventMsg, clockMsg, blinkMsg, kit.FrameMsg:
		default:
			next, cmd := m.Update(msg)
			m = next.(Model)
			run(cmd)
		}
	}
	run(m.Init())
	for _, msg := range msgs {
		apply(msg)
	}
	return m
}

func key(s string) tea.Msg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// TestRenderGoldens writes ANSI snapshots to $BULLETIN_RENDER_DIR for a
// human (or a browser) to look at. Skipped unless the env var is set.
func TestRenderGoldens(t *testing.T) {
	dir := os.Getenv("BULLETIN_RENDER_DIR")
	if dir == "" {
		t.Skip("set BULLETIN_RENDER_DIR to write renders")
	}
	svc, _ := testService(t)
	acct, _, _ := svc.Lookup(context.Background(), "SHA256:demo-0")
	sizes := [][2]int{{80, 24}, {120, 40}, {150, 45}}
	type shot struct {
		name string
		acct *board.Account
		keys []string
	}
	shots := []shot{
		{"splash", nil, nil},
		{"onboard", nil, []string{"enter"}},
		{"home", &acct, []string{"enter"}},
		{"home-down", &acct, []string{"enter", "down"}},
		{"board", &acct, []string{"enter", "down", "enter"}},
		{"board-chat", &acct, []string{"enter", "down", "enter", "tab"}},
		{"chat", &acct, []string{"enter", "down", "enter", "tab", "enter"}},
		{"thread", &acct, []string{"enter", "down", "enter", "enter"}},
		{"thread-rich", &acct, []string{"enter", "enter", "enter"}},
		{"thread-link", &acct, []string{"enter", "enter", "enter", "tab", "tab", "tab"}},
		{"link-modal", &acct, []string{"enter", "enter", "enter", "tab", "tab", "tab", "tab", "tab", "enter"}},
		{"compose", &acct, []string{"enter", "n"}},
		{"palette", &acct, []string{"enter", "/"}},
		{"help", &acct, []string{"enter", "?"}},
		{"profile", &acct, []string{"enter", "p"}},
	}
	for _, sz := range sizes {
		for _, s := range shots {
			m := New(Deps{Service: svc, Fingerprint: "SHA256:demo-0", Account: s.acct, Width: sz[0], Height: sz[1],
				Now: func() time.Time { return fixedNow }, Suggested: "nigel"})
			msgs := []tea.Msg{tea.WindowSizeMsg{Width: sz[0], Height: sz[1]}}
			for _, k := range s.keys {
				msgs = append(msgs, key(k))
			}
			m = drive(t, m, msgs...)
			if m.overlay != ovNone {
				m.overlayT = 1
			}
			m.accentT = 1
			out := m.View().Content
			name := filepath.Join(dir, s.name+"-"+itoa2(sz[0])+"x"+itoa2(sz[1])+".ans")
			if err := os.WriteFile(name, []byte(out), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Too small.
	m := New(Deps{Service: svc, Fingerprint: "x", Account: &acct, Width: 50, Height: 15, NoSplash: true, Now: func() time.Time { return fixedNow }})
	_ = os.WriteFile(filepath.Join(dir, "tiny-50x15.ans"), []byte(m.View().Content), 0o644)
}

func itoa2(n int) string { return strconv.Itoa(n) }

// liveEvent lets tests inject a broker event; drive() otherwise drops
// eventMsg, because the real listener would block forever.
type liveEvent board.Event
