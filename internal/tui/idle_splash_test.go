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
	if strings.Contains(still, catSource) || !strings.Contains(ansi.Strip(still), " <`-....__.'  fsc") || !strings.Contains(ansi.Strip(still), "_   Zz") {
		t.Fatal("idle splash should show the signed cat and keep the URL behind the warning")
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
	inhaleFrame := m.View().Content
	if !strings.Contains(ansi.Strip(inhaleFrame), "_   zZ") {
		t.Fatal("the sleep cue should switch to zZ on the inhale")
	}
	inhale := strings.Split(inhaleFrame, "\n")
	changed := 0
	for i, line := range strings.Split(still, "\n") {
		if line != inhale[i] {
			changed++
		}
	}
	if changed != 3 {
		t.Fatalf("breathing changed %d rows, want only the cat and its sleep cue", changed)
	}
	update(breath)
	if m.View().Content != still {
		t.Fatal("exhale did not return to the original pose")
	}
	x, y, ok := m.ui.idle.hot.Find(idleCreditKey)
	if !ok {
		t.Fatal("cat signature is missing its click target")
	}
	update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !m.idleSplash || m.idleLink.url != catSource || m.overlay != ovCompose {
		t.Fatal("clicking the signature should open its warning over the idle splash and preserve the draft overlay")
	}
	if out := m.View().Content; !strings.Contains(out, catSource) || !strings.Contains(ansi.Strip(out), "external link") {
		t.Fatal("the warning should show the cat source URL")
	}
	x, y, ok = m.ui.idle.hot.Find("modal:hint:c")
	if !ok || update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}) == nil || !m.idleLink.copied {
		t.Fatal("clicking copy should copy the cat source URL")
	}
	update(key("esc"))
	if !m.idleSplash || m.View().Content != still {
		t.Fatal("escape should close the warning and return to the cat")
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
