package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestHelpIsReachableAt80By24WithKeyboardAndMouse(t *testing.T) {
	f := newChatFixture(t)
	m := drive(t, modelFor(f.svc, f.owner, 80, 24), key("?"))
	m.overlayT = 1
	if text := screenText(m); m.overlay != ovHelp || !strings.Contains(text, "NAVIGATION") || !strings.Contains(text, "Terminal RTT") {
		t.Fatalf("initial Help:\n%s", text)
	}
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if text := screenText(m); m.overlay != ovHelp || !strings.Contains(text, "FORMATTING") || !strings.Contains(text, "PUBLIC VISIBILITY") || !strings.Contains(text, "Contact staff") {
		t.Fatalf("scrolled Help:\n%s", text)
	}
	m = drive(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 40, Y: 12})
	before := m.helpTop
	m = clickOn(t, m, "help:hint:↓")
	if m.helpTop != before+1 {
		t.Fatalf("mouse scroll position %d, want %d", m.helpTop, before+1)
	}
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyHome})
	m = drive(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 40, Y: 12})
	if m.helpTop != 3 {
		t.Fatalf("wheel down position %d", m.helpTop)
	}
	m = clickOn(t, m, "help:hint:esc")
	if m.overlay != ovNone || m.screen != scrHome {
		t.Fatalf("Help close left screen=%v overlay=%v", m.screen, m.overlay)
	}
}
