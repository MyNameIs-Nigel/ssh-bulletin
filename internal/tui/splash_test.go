package tui

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

func splashModel(t *testing.T, acct *board.Account, w, h int, at time.Time) Model {
	t.Helper()
	svc, _ := testService(t)
	m := New(Deps{Service: svc, Fingerprint: "SHA256:demo-0", Account: acct, Width: w, Height: h,
		Now: func() time.Time { return at }})
	return drive(t, m, tea.WindowSizeMsg{Width: w, Height: h})
}

// Nobody is carried past the splash on a timer, returning players included:
// only a key leaves it.
func TestSplashWaitsForAKey(t *testing.T) {
	svc, _ := testService(t)
	acct, _, _ := svc.Lookup(context.Background(), "SHA256:demo-0")
	now := fixedNow
	m := New(Deps{Service: svc, Fingerprint: acct.Fingerprint, Account: &acct, Width: 100, Height: 40,
		Now: func() time.Time { return now }})
	m = drive(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	for _, d := range []time.Duration{3 * time.Second, 30 * time.Second, 5 * time.Minute} {
		now = fixedNow.Add(d)
		next, _ := m.Update(kit.FrameMsg{Gen: m.gen})
		m = next.(Model)
		if m.screen != scrSplash {
			t.Fatalf("after %v the splash advanced on its own (screen %v)", d, m.screen)
		}
	}
	if out := ansi.Strip(m.View().Content); !strings.Contains(out, "press any key to begin") || !strings.Contains(out, "welcome back") {
		t.Fatalf("returning player's splash lacks the key prompt:\n%s", out)
	}
	m = drive(t, m, key("x"))
	if m.screen != scrHome {
		t.Fatalf("a key press left the splash for %v, want home", m.screen)
	}
}

// The prompt is mandatory now, so it must be on screen at every supported
// size, with or without the bridge.
func TestSplashFitsTheTerminal(t *testing.T) {
	svc, _ := testService(t)
	acct, _, _ := svc.Lookup(context.Background(), "SHA256:demo-0")
	for _, who := range []*board.Account{nil, &acct} {
		for _, sz := range [][2]int{{64, 20}, {80, 24}, {80, 30}, {100, 30}, {120, 40}, {150, 45}} {
			m := splashModel(t, who, sz[0], sz[1], fixedNow)
			rows := strings.Split(ansi.Strip(m.View().Content), "\n")
			if len(rows) != sz[1] {
				t.Errorf("%v: %d rows", sz, len(rows))
			}
			found := false
			for _, r := range rows {
				found = found || strings.Contains(r, "press any key to begin")
			}
			if !found {
				t.Errorf("%v (returning=%v): prompt is off screen:\n%s", sz, who != nil, strings.Join(rows, "\n"))
			}
		}
	}
	// The bridge itself appears wherever it fits, signature included.
	m := splashModel(t, nil, 100, 45, fixedNow)
	if out := ansi.Strip(m.View().Content); !strings.Contains(out, "[]") || !strings.Contains(out, bridgeTag) {
		t.Errorf("full-size splash is missing the bridge or its signature:\n%s", out)
	}
}

// Water flows and birds fly, but the piers, the signature and the tapering
// outline of the water stay where they are.
func TestBridgeMovesButTheStructureStays(t *testing.T) {
	art := theBridge()
	rows := art.fit(len(art.rows))
	if len(rows) != len(art.rows) {
		t.Fatalf("fit(full) kept %d of %d rows", len(rows), len(art.rows))
	}
	const w = 100
	frame := func(sec float64) []string {
		return strings.Split(ansi.Strip(art.render(rows, w, sec, kit.Midnight)), "\n")
	}
	a, b := frame(0), frame(1.7)
	moved := false
	for i := art.waterStart; i < len(a); i++ {
		if a[i] != b[i] {
			moved = true
		}
		ra, rb := []rune(a[i]), []rune(b[i])
		for _, c := range art.feet[i] {
			x := (w-art.w)/2 + c
			if ra[x] != rb[x] {
				t.Errorf("row %d: pier foot at col %d moved", i, c)
			}
		}
		for x := range ra {
			if (ra[x] == ' ') != (rb[x] == ' ') {
				t.Errorf("row %d: outline changed at col %d", i, x)
			}
		}
		first := strings.IndexFunc(a[i], func(r rune) bool { return r != ' ' })
		firstB := strings.IndexFunc(b[i], func(r rune) bool { return r != ' ' })
		if i != art.tagRow && first != firstB {
			t.Errorf("row %d: water's left edge moved %d → %d", i, first, firstB)
		}
	}
	if !moved {
		t.Error("the water did not move")
	}
	tagAt := strings.Index(a[art.tagRow], bridgeTag)
	if tagAt < 0 || strings.Index(b[art.tagRow], bridgeTag) != tagAt {
		t.Errorf("signature moved or vanished: %d → %d", tagAt, strings.Index(b[art.tagRow], bridgeTag))
	}
	sky := func(f []string) string { return strings.Join(f[:4], "\n") }
	if sky(a) == sky(b) {
		t.Error("the birds did not move")
	}
	if frame(0.0)[0] != frame(0.0)[0] || strings.Join(frame(5), "") != strings.Join(frame(5), "") {
		t.Error("render is not a pure function of time")
	}
}

// The signature stands in for a link until the real one is wired up, so it
// must read as one: bold and underlined, and the style must survive the
// scene's compositing.
func TestSignatureLooksLikeALink(t *testing.T) {
	m := splashModel(t, nil, 100, 45, fixedNow)
	var line string
	for _, l := range strings.Split(m.View().Content, "\n") {
		if strings.Contains(ansi.Strip(l), bridgeTag) {
			line = l
		}
	}
	if line == "" {
		t.Fatal("no signature on screen")
	}
	i := strings.Index(line, bridgeTag)
	if i < 0 {
		t.Fatalf("signature is split by escapes: %q", line)
	}
	pre := line[:i]
	sgr := pre[strings.LastIndex(pre, "\x1b["):]
	if !regexp.MustCompile(`[\[;]1[;m]`).MatchString(sgr) || !regexp.MustCompile(`[\[;]4[;m]`).MatchString(sgr) {
		t.Errorf("signature is not bold+underlined: %q", sgr)
	}
}

// The scene must not freeze while it waits for a key: a frozen splash that
// then jumps on the next event reads as a stutter.
func TestSplashKeepsAnimatingUntilAKey(t *testing.T) {
	now := fixedNow
	m := splashModel(t, nil, 100, 40, now)
	m.now = func() time.Time { return now }
	m.accentT = 1
	m.focusAt = now.Add(-time.Hour)
	for _, d := range []time.Duration{time.Second, 8 * time.Second, time.Minute, 10 * time.Minute} {
		now = fixedNow.Add(d)
		if !m.animating() {
			t.Fatalf("splash stopped animating %v in, before any key", d)
		}
	}
}

// An empty board shows the cat. Three seconds after it appears the cat shuts
// its eyes for 350ms, then does so every five seconds; the frame loop wakes
// only for the blink.
func TestEmptyBoardCatBlinks(t *testing.T) {
	svc, _ := testService(t)
	acct, _, _ := svc.Lookup(context.Background(), "SHA256:demo-0")
	now := fixedNow
	m := New(Deps{Service: svc, Fingerprint: acct.Fingerprint, Account: &acct, Width: 100, Height: 30,
		Now: func() time.Time { return now }, NoSplash: true})
	m = drive(t, m, tea.WindowSizeMsg{Width: 100, Height: 30}, key("enter"))
	// The board is opened a minute into the session: blinks count from
	// arrival, not from connecting.
	const arrive = time.Minute
	now = fixedNow.Add(arrive)
	next, cmd := m.Update(threadsMsg{boardID: m.curBoard.ID})
	m = next.(Model)
	if cmd == nil || !m.onEmptyBoard() || m.emptyAt != now {
		t.Fatalf("empty board not registered: cmd=%v empty=%v at=%v", cmd != nil, m.onEmptyBoard(), m.emptyAt)
	}
	m.accentT = 1
	m.focusAt = now.Add(-time.Hour)
	m.boardMenu.Snap()
	m.threadMenu.Snap()

	view := func(at time.Duration) string {
		now = fixedNow.Add(arrive + at)
		return ansi.Strip(m.View().Content)
	}
	for _, c := range []struct {
		at    time.Duration
		blink bool
	}{
		{0, false}, {time.Second, false}, {2900 * time.Millisecond, false},
		{3 * time.Second, true}, {3200 * time.Millisecond, true}, {3340 * time.Millisecond, true}, {3400 * time.Millisecond, false},
		{5 * time.Second, false}, {7900 * time.Millisecond, false},
		{8 * time.Second, true}, {8300 * time.Millisecond, true}, {8400 * time.Millisecond, false},
		{13100 * time.Millisecond, true},
	} {
		out := view(c.at)
		for _, want := range []string{"The board is empty!", `/\_/\`, "> ^ <"} {
			if !strings.Contains(out, want) {
				t.Fatalf("at %v: missing %q:\n%s", c.at, want, out)
			}
		}
		if strings.Contains(out, "A quiet board") {
			t.Errorf("at %v: old empty-state heading is still there", c.at)
		}
		open, shut := strings.Contains(out, "( o.o )"), strings.Contains(out, "( -.- )")
		if c.blink && !(shut && !open) || !c.blink && !(open && !shut) {
			t.Errorf("at %v: blink=%v but eyes open=%v shut=%v", c.at, c.blink, open, shut)
		}
		if got := m.animating(); got != c.blink {
			t.Errorf("at %v: animating=%v, want %v (only the blink needs frames)", c.at, got, c.blink)
		}
	}
	// The timer aims at the next blink, not at a fixed interval.
	for at, want := range map[time.Duration]time.Duration{
		0: 3 * time.Second, time.Second: 2 * time.Second, 4 * time.Second: 4 * time.Second, 6 * time.Second: 2 * time.Second,
	} {
		now = fixedNow.Add(arrive + at)
		if got := m.untilBlink(); got != want {
			t.Errorf("at %v: next blink in %v, want %v", at, got, want)
		}
	}
}

// A stale timer (from an earlier visit) must not keep a loop alive, and a
// board that gains a thread stops blinking.
func TestBlinkTimersAreRetired(t *testing.T) {
	svc, _ := testService(t)
	acct, _, _ := svc.Lookup(context.Background(), "SHA256:demo-0")
	m := New(Deps{Service: svc, Fingerprint: acct.Fingerprint, Account: &acct, Width: 100, Height: 30,
		Now: func() time.Time { return fixedNow }, NoSplash: true})
	m = drive(t, m, tea.WindowSizeMsg{Width: 100, Height: 30}, key("enter"))
	next, _ := m.Update(threadsMsg{boardID: m.curBoard.ID})
	m = next.(Model)
	gen := m.blinkGen
	if gen == 0 {
		t.Fatal("no blink timer started for an empty board")
	}
	// A refresh that is still empty keeps the same timer.
	next, cmd := m.Update(threadsMsg{boardID: m.curBoard.ID})
	m = next.(Model)
	if m.blinkGen != gen {
		t.Errorf("an empty refresh replaced the timer: %d → %d", gen, m.blinkGen)
	}
	_ = cmd
	// The live timer reschedules; a stale one does not.
	if _, c := m.Update(blinkMsg{gen: gen}); c == nil {
		t.Error("the live timer did not reschedule")
	}
	if _, c := m.Update(blinkMsg{gen: gen - 1}); c != nil {
		t.Error("a stale timer rescheduled itself")
	}
	// Leaving for another visit restarts the clock with a new timer.
	m.screen = scrHome
	m.emptyAt = time.Time{}
	if _, c := m.Update(blinkMsg{gen: gen}); c != nil {
		t.Error("a timer outlived the empty board")
	}
}
