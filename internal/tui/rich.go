package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/markup"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

// Rich text: posts and chat drawn from the markup dialect (internal/markup).
//
// Links come in two looks so nobody mistakes one for the other:
//
//   - internal (@user, #board, #12 thread, #12/3 post) are bold and wear the
//     gradient of what they point at: the user's palette, or the board's;
//   - external URLs are underlined in a fixed link blue and end in "↗", and
//     only ever open a warning that shows where they really go.
//
// A reference that does not resolve (no such user, a thread that never
// existed) stays plain text. Every link is a hot spot keyed by its scope (the
// post or message) and its index among that text's live links, which is
// also how the keyboard walks them.

// linkStops is the external-link gradient.
var linkStops = []colorT{kit.Hex("#6ad7ff"), kit.Hex("#4b9cff"), kit.Hex("#8f86ff")}

type richOpts struct {
	ink    lipgloss.Style // body text
	accent []colorT       // headings, quote bars and rules
	scope  string         // hot-spot scope; "" draws links that cannot be clicked
}

func postScope(id int64) string    { return "post:" + strconv.FormatInt(id, 10) }
func previewScope(id int64) string { return "pv:" + strconv.FormatInt(id, 10) }
func chatScope(id int64) string    { return "chat:" + strconv.FormatInt(id, 10) }
func linkKey(scope string, i int) string {
	if scope == "" {
		return ""
	}
	return scope + ":" + strconv.Itoa(i)
}

// atom is a word or a run of spaces, the unit of wrapping.
type atom struct {
	text  string
	style markup.Style
	link  int // index among the text's live links, or -1
	ref   markup.Ref
	space bool
	sub   bool // a link's added context (a thread's title), drawn lighter
}

// target is the resolved link behind ref, if it resolved.
func (m Model) target(ref *markup.Ref) (board.Target, bool) {
	if ref == nil {
		return board.Target{}, false
	}
	t, ok := m.refs[*ref]
	return t, ok && t.OK
}

// bodyLinks lists a post body's live links in reading order.
func (m Model) bodyLinks(body string) []board.Target {
	var out []board.Target
	for _, b := range m.ui.parse(body) {
		out = append(out, m.spanLinks(b.Spans)...)
	}
	return out
}

// lineLinks lists a chat line's live links in reading order.
func (m Model) lineLinks(text string) []board.Target { return m.spanLinks(m.ui.parseLine(text)) }

func (m Model) spanLinks(spans []markup.Span) []board.Target {
	var out []board.Target
	for _, s := range spans {
		if t, ok := m.target(s.Link); ok {
			out = append(out, t)
		}
	}
	return out
}

// richBody lays a post body out at width.
func (m Model) richBody(body string, width int, o richOpts) []string {
	var out []string
	n := 0
	for _, b := range m.ui.parse(body) {
		out = append(out, m.richBlock(b, max(4, width), o, &n)...)
	}
	return out
}

// richLine lays a single line (a chat message) out at width.
func (m Model) richLine(text string, width int, o richOpts) []string {
	n := 0
	return m.richBlock(markup.Block{Kind: markup.Para, Spans: m.ui.parseLine(text)}, max(4, width), o, &n)
}

func (m Model) richBlock(b markup.Block, width int, o richOpts, n *int) []string {
	accent := o.accent
	if len(accent) == 0 {
		accent = m.th.Brand.Stops
	}
	ink := o.ink
	first, cont := "", ""
	switch b.Kind {
	case markup.Blank:
		return []string{""}
	case markup.Rule:
		return []string{kit.FadeRule(width, "─", kit.At(accent, 0.5), m.th.BgBottom)}
	case markup.Code:
		bar := kit.S(m.th.Faint).Render("│ ")
		code := codeStyle(m.th)
		var out []string
		for _, l := range hardWrap(b.Spans[0].Text, width-2) {
			out = append(out, bar+code.Render(l))
		}
		return out
	case markup.Bullet, markup.Ordered:
		indent := strings.Repeat("  ", b.Level)
		mark := b.Marker + " "
		first = indent + kit.S(kit.At(accent, 0.3+0.2*float64(b.Level))).Bold(true).Render(mark)
		cont = indent + strings.Repeat(" ", lipgloss.Width(mark))
	case markup.Quote:
		bar := kit.S(kit.At(accent, 0.5)).Render("▍") + " "
		first, cont = bar, bar
		ink = kit.S(m.th.Subtle).Italic(true)
	case markup.Heading:
		ink = kit.S(m.th.Text).Bold(true)
	}
	atoms := m.atoms(b.Spans, n)
	fw, cw := width-lipgloss.Width(first), width-lipgloss.Width(cont)
	var out []string
	for i, line := range layout(atoms, max(1, fw), max(1, cw)) {
		lead := cont
		if i == 0 {
			lead = first
		}
		out = append(out, lead+m.renderAtoms(line, ink, o.scope, b.Kind == markup.Heading && b.Level == 1, accent))
	}
	return out
}

// atoms splits spans into words and spaces, numbering live links.
func (m Model) atoms(spans []markup.Span, n *int) []atom {
	var out []atom
	for _, s := range spans {
		text, extra, link, ref := s.Text, "", -1, markup.Ref{}
		if t, ok := m.target(s.Link); ok {
			link, ref = *n, *s.Link
			*n++
			text, extra = linkText(t, s.Text)
		}
		for _, piece := range splitSpaces(text) {
			out = append(out, atom{text: piece, style: s.Style, link: link, ref: ref, space: piece[0] == ' '})
		}
		if extra != "" {
			out = append(out, atom{text: " ", style: s.Style, link: link, ref: ref, space: true, sub: true})
			for _, piece := range splitSpaces(extra) {
				out = append(out, atom{text: piece, style: s.Style, link: link, ref: ref, space: piece[0] == ' ', sub: true})
			}
		}
	}
	return out
}

// linkText is what a link shows, and any context it gains: a bare thread or
// post number is followed by the thread's title, so "#12" says where it
// goes. A bare URL drops its scheme; external links end in ↗.
func linkText(t board.Target, src string) (text, extra string) {
	switch t.Ref.Kind {
	case markup.Thread, markup.Post:
		if src == t.Ref.Key() {
			return src, clip(t.Title, 36)
		}
	case markup.URL:
		if src == t.Ref.URL {
			src = shortURL(src)
		}
		return src + "↗", ""
	}
	return src, ""
}

func shortURL(u string) string {
	for _, p := range []string{"https://", "http://", "HTTPS://", "HTTP://"} {
		u = strings.TrimPrefix(u, p)
	}
	u = strings.TrimPrefix(u, "www.")
	return clip(strings.TrimSuffix(u, "/"), 48)
}

func splitSpaces(s string) []string {
	var out []string
	start := 0
	for i := 1; i <= len(s); i++ {
		if i == len(s) || (s[i] == ' ') != (s[start] == ' ') {
			out = append(out, s[start:i])
			start = i
		}
	}
	return out
}

// layout fills lines greedily: the first holds w0 cells, the rest w1. Words
// wider than a line break across lines; a line never starts with a space
// except the first, which keeps its author's indent.
func layout(atoms []atom, w0, w1 int) [][]atom {
	var lines [][]atom
	var cur []atom
	used, avail := 0, w0
	newline := func() {
		for len(cur) > 0 && cur[len(cur)-1].space {
			cur = cur[:len(cur)-1]
		}
		lines = append(lines, cur)
		cur, used, avail = nil, 0, w1
	}
	for _, a := range atoms {
		aw := ansi.StringWidth(a.text)
		if a.space {
			if len(cur) == 0 && len(lines) > 0 {
				continue
			}
			if used+aw > avail {
				newline()
				continue
			}
			cur, used = append(cur, a), used+aw
			continue
		}
		for used+aw > avail {
			if used > 0 && aw <= avail {
				newline()
				continue
			}
			head := ansi.Truncate(a.text, avail-used, "")
			if head == "" {
				if used > 0 {
					newline()
					continue
				}
				head = firstGrapheme(a.text) // wider than the whole line
			}
			cur = append(cur, atom{text: head, style: a.style, link: a.link, ref: a.ref})
			a.text = a.text[len(head):]
			aw = ansi.StringWidth(a.text)
			newline()
			if a.text == "" {
				break
			}
		}
		if a.text != "" {
			cur, used = append(cur, a), used+aw
		}
	}
	if len(cur) > 0 || len(lines) == 0 {
		newline()
	}
	return lines
}

func firstGrapheme(s string) string {
	g, _, _, _ := uniseg.FirstGraphemeClusterInString(s, -1)
	return g
}

func hardWrap(s string, w int) []string {
	if s == "" {
		return []string{""}
	}
	var out []string
	for s != "" {
		head := ansi.Truncate(s, max(1, w), "")
		if head == "" {
			head = firstGrapheme(s)
		}
		out = append(out, head)
		s = s[len(head):]
	}
	return out
}

// renderAtoms styles one laid-out line. Runs of one link become one hot
// spot; runs of one style become one styled string.
func (m Model) renderAtoms(line []atom, ink lipgloss.Style, scope string, banner bool, accent []colorT) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		j := i + 1
		for j < len(line) && line[j].link == line[i].link && line[j].sub == line[i].sub && (line[i].link >= 0 || line[j].style == line[i].style) {
			j++
		}
		var text strings.Builder
		for _, a := range line[i:j] {
			text.WriteString(a.text)
		}
		a := line[i]
		if a.link >= 0 {
			t := m.refs[a.ref]
			key := linkKey(scope, a.link)
			look := m.linkLook(t, text.String(), m.lit(key), a.style, a.sub)
			b.WriteString(m.hot(key, func(m *Model) tea.Cmd { return m.follow(t) }, look))
		} else if banner && a.style&markup.Mono == 0 {
			b.WriteString(kit.Gradient(text.String(), inlineStyle(ink, a.style, m.th), accent, 0))
		} else {
			b.WriteString(inlineStyle(ink, a.style, m.th).Render(text.String()))
		}
		i = j
	}
	return b.String()
}

func inlineStyle(base lipgloss.Style, st markup.Style, th kit.Theme) lipgloss.Style {
	if st&markup.Mono != 0 {
		return codeStyle(th)
	}
	if st&markup.Bold != 0 {
		base = base.Bold(true)
	}
	if st&markup.Italic != 0 {
		base = base.Italic(true)
	}
	if st&markup.Strike != 0 {
		base = base.Strikethrough(true).Foreground(th.Muted)
	}
	return base
}

func codeStyle(th kit.Theme) lipgloss.Style {
	return kit.S(kit.Lerp(th.Text, th.Warn, 0.45)).Background(kit.Lerp(th.BgTop, th.Surface, 0.85))
}

// linkLook draws a link: internal ones bold in their target's colors,
// external ones underlined in link blue. Lit, either flows.
func (m Model) linkLook(t board.Target, text string, lit bool, st markup.Style, sub bool) string {
	base := lipgloss.NewStyle().Bold(!sub).Italic(sub)
	stops := kit.PaletteNamed(t.Palette).Stops
	if t.Ref.Kind == markup.URL {
		base, stops = lipgloss.NewStyle().Underline(true), linkStops
	}
	if st&markup.Italic != 0 {
		base = base.Italic(true)
	}
	return kit.Flow(text, base, stops, lit, m.phase(), m.th)
}

// plainLine is a chat line with its markup applied but nothing clickable, for
// one-row teasers that cut the text off.
func (m Model) plainLine(text string, ink lipgloss.Style) string {
	n := 0
	var parts []string
	for _, line := range layout(m.atoms(m.ui.parseLine(text), &n), 1<<20, 1<<20) {
		parts = append(parts, m.renderAtoms(line, ink, "", false, nil))
	}
	return strings.Join(parts, " ")
}
