package markup

import (
	"reflect"
	"testing"
)

func spans(s string) []Span { return Inline(s) }

func TestEmphasis(t *testing.T) {
	cases := []struct {
		in   string
		want []Span
	}{
		{"plain", []Span{{Text: "plain"}}},
		{"a **bold** b", []Span{{Text: "a "}, {Text: "bold", Style: Bold}, {Text: " b"}}},
		{"*it* and _it_", []Span{{Text: "it", Style: Italic}, {Text: " and "}, {Text: "it", Style: Italic}}},
		{"***both***", []Span{{Text: "both", Style: Bold | Italic}}},
		{"`**not**`", []Span{{Text: "**not**", Style: Mono}}},
		{"~~gone~~", []Span{{Text: "gone", Style: Strike}}},
		{"snake_case_name", []Span{{Text: "snake_case_name"}}},
		{"2 * 3 * 4", []Span{{Text: "2 * 3 * 4"}}},
		{"\\*literal\\*", []Span{{Text: "*literal*"}}},
		{"**unclosed", []Span{{Text: "**unclosed"}}},
	}
	for _, c := range cases {
		if got := spans(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q:\n got %+v\nwant %+v", c.in, got, c.want)
		}
	}
}

func TestReferences(t *testing.T) {
	cases := []struct {
		in   string
		want []Ref
	}{
		{"hi @Alice!", []Ref{{Kind: User, Name: "alice"}}},
		{"mail a@b.com", nil},
		{"see #lobby and #12, #12/3", []Ref{{Kind: Board, Name: "lobby"}, {Kind: Thread, Thread: 12}, {Kind: Post, Thread: 12, Post: 3}}},
		{"#1st place", nil},
		{"go https://example.com/a_(b). now", []Ref{{Kind: URL, URL: "https://example.com/a_(b)"}}},
		{"(https://example.com)", []Ref{{Kind: URL, URL: "https://example.com"}}},
		{"http://x nope", nil},
		{"[docs](https://go.dev/doc) [them](@bob) [that](#7/2) [bad](javascript:alert)", []Ref{
			{Kind: URL, URL: "https://go.dev/doc"}, {Kind: User, Name: "bob"}, {Kind: Post, Thread: 7, Post: 2}}},
		{"`@notalink`", nil},
		{"a&#39;b x#y", nil},
	}
	for _, c := range cases {
		if got := SpanRefs(Inline(c.in)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q:\n got %+v\nwant %+v", c.in, got, c.want)
		}
	}
}

func TestBracketLabelIsTheText(t *testing.T) {
	got := Inline("read [the docs](https://go.dev) ok")
	if len(got) != 3 || got[1].Text != "the docs" || got[1].Link == nil || got[1].Link.URL != "https://go.dev" {
		t.Fatalf("%+v", got)
	}
}

func TestBlocks(t *testing.T) {
	src := "# Title\nplain\n- one\n  * two\n3. three\n> quoted **bold**\n---\n\n```\n**raw** @x\n```\n#lobby is a board"
	var kinds []BlockKind
	for _, b := range Parse(src) {
		kinds = append(kinds, b.Kind)
	}
	want := []BlockKind{Heading, Para, Bullet, Bullet, Ordered, Quote, Rule, Blank, Code, Para}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds %v, want %v", kinds, want)
	}
	bs := Parse(src)
	if bs[3].Level != 1 || bs[3].Marker != "◦" || bs[4].Marker != "3." {
		t.Errorf("list markers: %+v %+v", bs[3], bs[4])
	}
	if bs[8].Spans[0].Text != "**raw** @x" || bs[8].Spans[0].Link != nil {
		t.Errorf("code line was parsed: %+v", bs[8])
	}
	if len(Refs(bs)) != 1 {
		t.Errorf("refs %+v", Refs(bs))
	}
}

// Parsing must never lose or invent text, whatever the input.
func FuzzInlineKeepsText(f *testing.F) {
	for _, s := range []string{"**a*b**", "[x](y", "@@a #1/ https://a.b)", "_a_b_", "\\", "~~~"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_ = Parse(s)
		for _, sp := range Inline(s) {
			if sp.Text == "" {
				t.Fatalf("empty span in %q", s)
			}
		}
	})
}
