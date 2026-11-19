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
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

func TestIdleSplashPreservesDraftAndOnlyAnimatesTheCat(t *testing.T) {
	svc, _ := testService(t)
	acct, _, err := svc.Lookup(context.Background(), "SHA256:demo-0")
	if err != nil {
		t.Fatal(err)
	}
	now := fixedNow
	m := New(Deps{Service: svc, Account: &acct, Width: 80, Height: 24,
		Now: func() time.Time { return now }, NoSplash: true})
	m.openOverlay(ovCompose)
	m.compose.body.SetValue("my draft")
	update := func(msg tea.Msg) tea.Cmd {
		next, cmd := m.Update(msg)
		m = next.(Model)
		return cmd
	}

	now = fixedNow.Add(idleSplashAfter - time.Second)
	update(clockMsg{})
	if m.idleSplash {
		t.Fatal("idle splash appeared before five minutes")
	}
	now = fixedNow.Add(idleSplashAfter)
	activeGen := m.gen
	update(clockMsg{})
	if !m.idleSplash || m.animating() || m.ticking {
		t.Fatal("five minutes of inactivity should replace the ordinary animation loop")
	}
	still := m.View().Content
	if !strings.Contains(still, catSource) || !strings.Contains(ansi.Strip(still), " <`-....__.'  fsc") {
		t.Fatal("idle splash is missing the cat or the art source")
	}
	if cmd := update(kit.FrameMsg{Gen: activeGen}); cmd != nil {
		t.Fatal("the old animation loop scheduled another frame")
	}
	update(eventMsg(board.Event{Kind: board.EventThread, Actor: "another user"}))
	update(clockMsg{})
	if m.View().Content != still {
		t.Fatal("background activity changed the idle splash")
	}
	breath := idleBreathMsg{gen: m.gen}
	if cmd := update(breath); cmd == nil {
		t.Fatal("breathing should schedule the next pose")
	}
	inhale := strings.Split(m.View().Content, "\n")
	changed := 0
	for i, line := range strings.Split(still, "\n") {
		if line != inhale[i] {
			changed++
		}
	}
	if changed != 2 {
		t.Fatalf("breathing changed %d rows, want only the two moving cat rows", changed)
	}
	update(breath)
	if m.View().Content != still {
		t.Fatal("exhale did not return to the original pose")
	}

	now = now.Add(idleBreathHold)
	update(key("x"))
	if m.idleSplash || m.lastInput != now || m.overlay != ovCompose || m.compose.body.Value() != "my draft" {
		t.Fatal("the wake key should restore the open draft without editing it")
	}
	if cmd := update(breath); cmd != nil {
		t.Fatal("a pending breath scheduled another idle frame after waking")
	}
}

func TestIdleSplashFitsAndClickRestoresStartup(t *testing.T) {
	now := fixedNow
	m := New(Deps{Width: 80, Height: 24, Now: func() time.Time { return now }})
	now = now.Add(idleSplashAfter)
	next, _ := m.Update(clockMsg{})
	m = next.(Model)
	for _, size := range [][2]int{{80, 24}, {36, 10}, {17, 8}, {17, 7}, {1, 1}} {
		next, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		view := m.View().Content
		if lipgloss.Width(view) != size[0] || lipgloss.Height(view) != size[1] {
			t.Errorf("idle splash did not fit %dx%d", size[0], size[1])
		}
	}
	next, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft})
	m = next.(Model)
	if m.idleSplash || m.screen != scrSplash {
		t.Fatal("a wake click should return to the startup splash")
	}
}
