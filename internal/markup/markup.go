// Package markup parses the small Markdown dialect posts and chat are written
// in, and finds the references that become links.
//
// It is deliberately line-oriented: every source line stays a line (forum
// posts keep the breaks their author typed), and a line's leading marker
// decides its block kind. Inline, it understands **bold**, *italic* and
// _italic_, `code`, ~~strike~~, [label](target), bare http(s) URLs, and the
// bulletin's own references:
//
//	@handle    a user
//	#slug      a board
//	#12        thread 12
//	#12/3      the third post of thread 12
//
// Parsing never fails and never drops text: anything that is not markup is
// shown as typed. Whether a reference is real is not decided here. The
// renderer asks the board service, and an unresolved reference stays plain
// text.
package markup

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// BlockKind is what a source line is.
type BlockKind int

const (
	Para BlockKind = iota
	Blank
	Bullet
	Ordered
	Quote
	Heading
	Code // a line inside a ``` fence, shown verbatim
	Rule // ---, *** or ___ alone on a line
)

// Style is a set of inline emphasis flags.
type Style uint8

const (
	Bold Style = 1 << iota
	Italic
	Mono
	Strike
)

// Block is one source line.
type Block struct {
	Kind   BlockKind
	Level  int    // list nesting (0..maxLevel) or heading level (1..3)
	Marker string // "•", "3." and so on for list items
	Spans  []Span
}

// Span is a run of text in one style, or one link.
type Span struct {
	Text  string
	Style Style
	Link  *Ref // nil unless this span is a link
}

// RefKind is what a link points at.
type RefKind int

const (
	User RefKind = iota + 1
	Board
	Thread
	Post
	URL
)

// Ref is a link target. It is comparable, so it can key a map of resolved
// targets.
type Ref struct {
	Kind   RefKind
	Name   string // handle (lowercased) or board slug
	Thread int64
	Post   int // 1-based position within the thread
	URL    string
}

// Key is a stable string for the reference, also usable in a hit tag.
func (r Ref) Key() string {
	switch r.Kind {
	case User:
		return "@" + r.Name
	case Board:
		return "#" + r.Name
	case Thread:
		return "#" + strconv.FormatInt(r.Thread, 10)
	case Post:
		return "#" + strconv.FormatInt(r.Thread, 10) + "/" + strconv.Itoa(r.Post)
	case URL:
		return r.URL
	}
	return ""
}

// Internal reports whether the reference stays inside the bulletin.
func (r Ref) Internal() bool { return r.Kind != URL && r.Kind != 0 }

const (
	maxLevel   = 3
	maxHandle  = 20
	maxSlug    = 24
	maxURL     = 2048
	maxDigits  = 12
	maxNesting = 4 // emphasis inside emphasis inside...
)

// Parse reads a whole post body.
func Parse(src string) []Block {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	out := make([]Block, 0, len(lines))
	fenced := false
	for _, l := range lines {
		if isFence(l) {
			fenced = !fenced
			continue
		}
		if fenced {
			out = append(out, Block{Kind: Code, Spans: []Span{{Text: l, Style: Mono}}})
			continue
		}
		out = append(out, parseLine(l))
	}
	return out
}

// ParseLine reads a single-line text (chat, bios) as inline markup only:
// a chat line starting with "- " is a dash, not a list.
func ParseLine(src string) []Span { return Inline(src) }

func isFence(l string) bool { return strings.HasPrefix(strings.TrimSpace(l), "```") }

func parseLine(l string) Block {
	trimmed := strings.TrimLeft(l, " ")
	if strings.TrimSpace(trimmed) == "" {
		return Block{Kind: Blank}
	}
	indent := min(maxLevel, (len(l)-len(trimmed))/2)
	switch {
	case isRule(trimmed):
		return Block{Kind: Rule}
	case strings.HasPrefix(trimmed, "> "), trimmed == ">":
		return Block{Kind: Quote, Spans: Inline(strings.TrimPrefix(strings.TrimPrefix(trimmed, ">"), " "))}
	}
	if n := headingLevel(trimmed); n > 0 && indent == 0 {
		return Block{Kind: Heading, Level: n, Spans: Inline(strings.TrimSpace(trimmed[n:]))}
	}
	if len(trimmed) >= 2 && strings.ContainsRune("-*+", rune(trimmed[0])) && trimmed[1] == ' ' {
		return Block{Kind: Bullet, Level: indent, Marker: bullets[indent%len(bullets)], Spans: Inline(strings.TrimSpace(trimmed[2:]))}
	}
	if num, rest, ok := orderedItem(trimmed); ok {
		return Block{Kind: Ordered, Level: indent, Marker: num + ".", Spans: Inline(rest)}
	}
	return Block{Kind: Para, Spans: Inline(l)}
}

var bullets = []string{"•", "◦", "▪", "‣"}

func isRule(s string) bool {
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	if len(s) < 3 {
		return false
	}
	for _, ch := range []string{"-", "*", "_"} {
		if strings.Trim(s, ch) == "" {
			return true
		}
	}
	return false
}

// headingLevel is n for a line starting with n "#"s and a space (n ≤ 3), so
// "# Title" is a heading and "#lobby" stays a board reference.
func headingLevel(s string) int {
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	if n == 0 || n > 3 || n >= len(s) || s[n] != ' ' {
		return 0
	}
	return n
}

func orderedItem(s string) (num, rest string, ok bool) {
	i := 0
	for i < len(s) && i < 4 && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(s) || (s[i] != '.' && s[i] != ')') || s[i+1] != ' ' {
		return "", "", false
	}
	return s[:i], strings.TrimSpace(s[i+2:]), true
}

// Inline parses emphasis and links in one line of text.
func Inline(s string) []Span {
	p := inlineParser{src: []rune(s)}
	p.run(0, len(p.src), 0, 0)
	return mergeSpans(p.out)
}

type inlineParser struct {
	src []rune
	out []Span
}

func (p *inlineParser) emit(text string, st Style, link *Ref) {
	if text == "" {
		return
	}
	p.out = append(p.out, Span{Text: text, Style: st, Link: link})
}

// run parses src[from:to] in style st. depth bounds emphasis nesting, so a
// pathological line cannot recurse without limit.
func (p *inlineParser) run(from, to int, st Style, depth int) {
	var plain strings.Builder
	flush := func() {
		p.emit(plain.String(), st, nil)
		plain.Reset()
	}
	src := p.src
	for i := from; i < to; {
		r := src[i]
		switch {
		case r == '\\' && i+1 < to && isPunct(src[i+1]):
			plain.WriteRune(src[i+1])
			i += 2
			continue

		case r == '`':
			if j := indexRune(src, '`', i+1, to); j > i+1 {
				flush()
				p.emit(string(src[i+1:j]), st|Mono, nil)
				i = j + 1
				continue
			}

		case r == '[':
			if label, target, end, ok := p.bracketLink(i, to); ok {
				if ref, ok := parseTarget(target); ok {
					flush()
					p.emit(label, st, &ref)
					i = end
					continue
				}
			}

		case (r == '*' || r == '_' || r == '~') && depth < maxNesting:
			if n, add, end, ok := p.emphasis(i, to); ok {
				flush()
				p.run(i+n, end, st|add, depth+1)
				i = end + n
				continue
			}

		case r == '@' && boundaryBefore(src, i, from):
			if n := scanHandle(src, i+1, to); n >= 2 {
				flush()
				text := string(src[i : i+1+n])
				p.emit(text, st, &Ref{Kind: User, Name: strings.ToLower(text[1:])})
				i += 1 + n
				continue
			}

		case r == '#' && boundaryBefore(src, i, from):
			if ref, n, ok := scanHash(src, i+1, to); ok {
				flush()
				p.emit(string(src[i:i+1+n]), st, &ref)
				i += 1 + n
				continue
			}

		case (r == 'h' || r == 'H') && boundaryBefore(src, i, from):
			if n := scanURL(src, i, to); n > 0 {
				flush()
				u := string(src[i : i+n])
				p.emit(u, st, &Ref{Kind: URL, URL: u})
				i += n
				continue
			}
		}
		plain.WriteRune(r)
		i++
	}
	flush()
}

// emphasis recognises a delimiter run at i that opens a span closed before
// to. It returns the delimiter length, the style it adds, and where the
// closing delimiter starts. Underscores only count at word boundaries, so
// snake_case_names survive.
func (p *inlineParser) emphasis(i, to int) (n int, add Style, end int, ok bool) {
	src := p.src
	r := src[i]
	n = 1
	if i+1 < to && src[i+1] == r {
		n = 2
	}
	switch {
	case r == '~' && n == 2:
		add = Strike
	case r == '~':
		return 0, 0, 0, false
	case n == 2:
		add = Bold
	default:
		add = Italic
	}
	open := i + n
	if open >= to || unicode.IsSpace(src[open]) {
		return 0, 0, 0, false
	}
	if r == '_' && i > 0 && isWord(src[i-1]) {
		return 0, 0, 0, false
	}
	for j := open + 1; j+n <= to; j++ {
		if src[j] == '\\' {
			j++
			continue
		}
		if src[j] == '`' { // emphasis does not reach into code
			if k := indexRune(src, '`', j+1, to); k > j {
				j = k
				continue
			}
		}
		if !matchRun(src, j, r, n, to) || unicode.IsSpace(src[j-1]) {
			continue
		}
		after := j + n
		if r == '_' && after < to && isWord(src[after]) {
			continue
		}
		if after < to && src[after] == r {
			continue // close on the last run of a "***", so the inner one pairs up
		}
		return n, add, j, true
	}
	return 0, 0, 0, false
}

func matchRun(src []rune, j int, r rune, n, to int) bool {
	if j+n > to {
		return false
	}
	for k := range n {
		if src[j+k] != r {
			return false
		}
	}
	return true
}

// bracketLink recognises [label](target) starting at i.
func (p *inlineParser) bracketLink(i, to int) (label, target string, end int, ok bool) {
	src := p.src
	close := indexRune(src, ']', i+1, to)
	if close <= i+1 || close+1 >= to || src[close+1] != '(' {
		return "", "", 0, false
	}
	paren := indexRune(src, ')', close+2, to)
	if paren < 0 {
		return "", "", 0, false
	}
	label = strings.TrimSpace(string(src[i+1 : close]))
	target = strings.TrimSpace(string(src[close+2 : paren]))
	if label == "" || target == "" || strings.ContainsAny(target, " \t") {
		return "", "", 0, false
	}
	return label, target, paren + 1, true
}

// parseTarget reads the (target) of a bracket link.
func parseTarget(t string) (Ref, bool) {
	rs := []rune(t)
	switch {
	case strings.HasPrefix(t, "@"):
		if n := scanHandle(rs, 1, len(rs)); n == len(rs)-1 && n >= 2 {
			return Ref{Kind: User, Name: strings.ToLower(t[1:])}, true
		}
	case strings.HasPrefix(t, "#"):
		if ref, n, ok := scanHash(rs, 1, len(rs)); ok && n == len(rs)-1 {
			return ref, true
		}
	default:
		if n := scanURL(rs, 0, len(rs)); n == len(rs) {
			return Ref{Kind: URL, URL: t}, true
		}
	}
	return Ref{}, false
}

func scanHandle(src []rune, i, to int) int {
	n := 0
	for i+n < to && n <= maxHandle && isHandleRune(src[i+n]) {
		n++
	}
	if n > maxHandle || n < 2 {
		return 0
	}
	return n
}

// scanHash reads what follows a "#": digits (a thread, optionally /post) or
// a board slug of lowercase letters, digits and dashes starting with a letter.
func scanHash(src []rune, i, to int) (Ref, int, bool) {
	n := 0
	for i+n < to && isDigit(src[i+n]) {
		n++
	}
	if n > 0 {
		if n > maxDigits || (i+n < to && isWord(src[i+n]) && src[i+n] != '/') {
			return Ref{}, 0, false
		}
		id, err := strconv.ParseInt(string(src[i:i+n]), 10, 64)
		if err != nil || id <= 0 {
			return Ref{}, 0, false
		}
		ref := Ref{Kind: Thread, Thread: id}
		if i+n+1 < to && src[i+n] == '/' && isDigit(src[i+n+1]) {
			m := 0
			for i+n+1+m < to && isDigit(src[i+n+1+m]) {
				m++
			}
			post, err := strconv.Atoi(string(src[i+n+1 : i+n+1+m]))
			if err == nil && post > 0 && m <= 6 && !(i+n+1+m < to && isWord(src[i+n+1+m])) {
				ref.Kind, ref.Post = Post, post
				n += 1 + m
			}
		}
		return ref, n, true
	}
	if i >= to || !(src[i] >= 'a' && src[i] <= 'z' || src[i] >= 'A' && src[i] <= 'Z') {
		return Ref{}, 0, false
	}
	for i+n < to && n <= maxSlug && (isAlnum(src[i+n]) || src[i+n] == '-') {
		n++
	}
	for n > 0 && src[i+n-1] == '-' {
		n--
	}
	if n > maxSlug || (i+n < to && isWord(src[i+n])) {
		return Ref{}, 0, false
	}
	return Ref{Kind: Board, Name: strings.ToLower(string(src[i : i+n]))}, n, true
}

// scanURL reads an http(s) URL at i and returns its length, or 0. Trailing
// sentence punctuation is left out, and so is a closing parenthesis the URL
// did not open.
func scanURL(src []rune, i, to int) int {
	rest := string(src[i:min(to, i+8)])
	lower := strings.ToLower(rest)
	var scheme int
	switch {
	case strings.HasPrefix(lower, "https://"):
		scheme = 8
	case strings.HasPrefix(lower, "http://"):
		scheme = 7
	default:
		return 0
	}
	n := scheme
	for i+n < to && n < maxURL && urlRune(src[i+n]) {
		n++
	}
	for n > scheme {
		last := src[i+n-1]
		if strings.ContainsRune(".,;:!?'\"*_~", last) {
			n--
			continue
		}
		if last == ')' && strings.Count(string(src[i:i+n]), "(") < strings.Count(string(src[i:i+n]), ")") {
			n--
			continue
		}
		break
	}
	if n <= scheme || !strings.ContainsAny(string(src[i+scheme:i+n]), ".:") && string(src[i+scheme:i+n]) != "localhost" {
		return 0
	}
	return n
}

func urlRune(r rune) bool {
	if r > unicode.MaxASCII {
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	}
	return r > ' ' && !strings.ContainsRune("<>\"`{}|\\^", r)
}

func boundaryBefore(src []rune, i, from int) bool {
	if i == 0 {
		return true
	}
	prev := src[i-1]
	return !isWord(prev) && prev != '@' && prev != '#' && prev != '/' && prev != '&'
}

func isHandleRune(r rune) bool { return isAlnum(r) || r == '_' || r == '-' }
func isAlnum(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || isDigit(r)
}
func isDigit(r rune) bool { return r >= '0' && r <= '9' }
func isWord(r rune) bool  { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }
func isPunct(r rune) bool { return r < utf8.RuneSelf && unicode.IsPunct(r) || r == '`' || r == '~' }

func indexRune(src []rune, r rune, from, to int) int {
	for j := from; j < to; j++ {
		if src[j] == r {
			return j
		}
	}
	return -1
}

func mergeSpans(in []Span) []Span {
	out := in[:0]
	for _, s := range in {
		if n := len(out); n > 0 && s.Link == nil && out[n-1].Link == nil && out[n-1].Style == s.Style {
			out[n-1].Text += s.Text
			continue
		}
		out = append(out, s)
	}
	return out
}

// Refs lists the distinct references in blocks, in order of appearance.
func Refs(blocks []Block) []Ref {
	var out []Ref
	seen := map[Ref]bool{}
	for _, b := range blocks {
		for _, s := range b.Spans {
			if s.Link != nil && !seen[*s.Link] {
				seen[*s.Link] = true
				out = append(out, *s.Link)
			}
		}
	}
	return out
}

// SpanRefs lists the distinct references in a single line's spans.
func SpanRefs(spans []Span) []Ref { return Refs([]Block{{Spans: spans}}) }

// Plain is the text with markup removed, for one-line previews.
func Plain(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}
