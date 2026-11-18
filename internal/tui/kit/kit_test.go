package kit

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestFitAndBlockAreExact(t *testing.T) {
	for _, s := range []string{"", "short", strings.Repeat("wide", 30), GradientBold("gradient text", Midnight.Brand.Stops)} {
		if w := lipgloss.Width(Fit(s, 20)); w != 20 {
			t.Errorf("Fit(%q) width %d", s, w)
		}
	}
	b := Block("a\nb\nc\nd", 7, 2)
	if lipgloss.Width(b) != 7 || lipgloss.Height(b) != 2 {
		t.Fatalf("Block = %dx%d", lipgloss.Width(b), lipgloss.Height(b))
	}
}

func TestPanelGeometry(t *testing.T) {
	for _, focused := range []bool{false, true} {
		p := Panel{Title: "◆ title", Note: "3 posts", Width: 30, Height: 8, Stops: Midnight.Brand.Stops, Focused: focused, Phase: 0.3, Theme: Midnight}
		out := p.Render("hello\nworld")
		if lipgloss.Width(out) != 30 || lipgloss.Height(out) != 8 {
			t.Fatalf("panel %dx%d", lipgloss.Width(out), lipgloss.Height(out))
		}
		for i, l := range strings.Split(out, "\n") {
			if lipgloss.Width(l) != 30 {
				t.Errorf("row %d width %d", i, lipgloss.Width(l))
			}
		}
	}
}

func TestSceneFillsEveryCell(t *testing.T) {
	sc := NewScene(40, 10, Midnight)
	sc.Base = "plain text with no colors"
	sc.Overlay(Panel{Width: 12, Height: 4, Theme: Midnight, Fill: Midnight.Overlay}.Render("x"), 5, 3, Midnight.Overlay)
	sc.Dim = 0.5
	out := sc.Render()
	if lipgloss.Height(out) != 10 {
		t.Fatalf("height %d", lipgloss.Height(out))
	}
	for i, l := range strings.Split(out, "\n") {
		if lipgloss.Width(l) != 40 {
			t.Errorf("row %d width %d (cells left to the terminal's colors)", i, lipgloss.Width(l))
		}
	}
	// The overlay must not erase the base frame outside its own rectangle.
	if !strings.Contains(out, "p") || !strings.Contains(out, "lain") {
		t.Fatal("overlay erased the base frame")
	}
}

func TestPixelsAndSigils(t *testing.T) {
	if s := Sigil("SHA256:abc", Midnight.Brand.Stops, 1); lipgloss.Width(s) != 5 || lipgloss.Height(s) != 3 {
		t.Fatalf("sigil %dx%d", lipgloss.Width(s), lipgloss.Height(s))
	}
	if Sigil("a", Midnight.Brand.Stops, 2) == Sigil("b", Midnight.Brand.Stops, 2) {
		t.Fatal("sigils should differ by key")
	}
	if w := Wordmark("BULLETIN", Midnight.Brand.Stops, 0); lipgloss.Height(w) != 5 {
		t.Fatalf("wordmark height %d", lipgloss.Height(w))
	}
}

func TestMenuSpringSettles(t *testing.T) {
	m := NewMenu([]Item{{Title: "a"}, {Title: "b"}, {Title: "c"}})
	m.Move(2)
	for range 200 {
		m.Step(1.0 / FrameRate)
	}
	if m.Animating() {
		t.Fatal("spring never settled: the render loop would never stop")
	}
	if m.ItemAtRow(0) != 0 || m.ItemAtRow(2) != -1 || m.ItemAtRow(3) != 1 {
		t.Fatal("row mapping")
	}
}

func TestPaletteLookup(t *testing.T) {
	if PaletteNamed("nope").Name != "aurora" || PaletteNamed("ember").Name != "ember" {
		t.Fatal("palette lookup")
	}
}

// Unstyled text (e.g. a wrapped continuation line) must render in the theme's
// text color, never the terminal's default foreground.
func TestSceneGivesUnstyledTextTheThemeColor(t *testing.T) {
	sc := NewScene(20, 2, Midnight)
	sc.Base = "plain"
	out := sc.Render()
	r, g, b, _ := Midnight.Text.RGBA()
	want := fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
	if !strings.Contains(out, want) {
		t.Fatalf("unstyled base text did not get Theme.Text (%s): %q", want, out)
	}
}

// The masthead rule must land exactly on its resting look when the sweep
// ends (and start from it), or it visibly jumps when the animation stops.
func TestGlowRuleSettles(t *testing.T) {
	stops := Midnight.Brand.Stops
	rest := GlowRule(80, "━", stops, Midnight.BgTop, 1)
	if GlowRule(80, "━", stops, Midnight.BgTop, 0) != rest {
		t.Fatal("sweep 0 differs from the resting rule")
	}
	if GlowRule(80, "━", stops, Midnight.BgTop, 0.5) == rest {
		t.Fatal("mid-sweep should show a highlight")
	}
	if w := lipgloss.Width(rest); w != 80 {
		t.Fatalf("width %d", w)
	}
}

// Hot marks survive panels, fitting, truncation and the menu's glow, land in
// the hit map where they were drawn, and never reach the terminal.
func TestHotSpotsMapAndVanish(t *testing.T) {
	th := Midnight
	menu := NewMenu([]Item{{Title: "first"}, {Title: "second"}})
	menu.HotKey = func(i int) string { return "row" + string(rune('0'+i)) }
	body := Hot("link", "click me") + " plain " + Hot("cut", "this one is far too long to fit")
	p := Panel{Width: 30, Height: 9, Theme: th}
	sc := NewScene(40, 12, th)
	var hm HotMap
	sc.Hot = &hm
	sc.Base = "\n" + Indent(p.Render(body+"\n"+menu.Render(26, 5, th, true)), 3)
	out := sc.Render()
	if strings.Contains(out, hotScheme) {
		t.Fatal("a hot mark reached the output")
	}
	if x, y, ok := hm.Find("link"); !ok || x != 5 || y != 2 {
		t.Fatalf("link at %d,%d %v", x, y, ok)
	}
	if hm.At(5+len("click me"), 2) != "" || hm.At(4, 2) != "" {
		t.Fatal("link span spills past its text")
	}
	if _, y, ok := hm.Find("cut"); !ok || y != 2 {
		t.Fatal("truncated link lost")
	}
	if hm.At(3+29, 2) != "" {
		t.Fatal("a truncated link leaked into the border")
	}
	_, y0, ok0 := hm.Find("row0")
	_, y1, ok1 := hm.Find("row1")
	if !ok0 || !ok1 || y0 != 3 || y1 != 6 {
		t.Fatalf("menu rows at %d %v, %d %v; keys %v", y0, ok0, y1, ok1, hm.Keys())
	}
	if hm.At(7, 4) != "row0" {
		t.Fatal("the description row of an item is not part of it")
	}
}

// A real link (the external-link modal's) passes through to the terminal.
func TestRealLinksPassThrough(t *testing.T) {
	sc := NewScene(20, 2, Midnight)
	sc.Base = Link("https://example.com", "go")
	if !strings.Contains(sc.Render(), "https://example.com") {
		t.Fatal("real hyperlink was dropped")
	}
}
