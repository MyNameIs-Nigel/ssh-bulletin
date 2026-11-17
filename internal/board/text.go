package board

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Length caps. These are not moderation; they keep one post from becoming a
// denial of service for everyone else's terminal.
const (
	MaxHandleLen = 20
	MinHandleLen = 2
	MaxTitleLen  = 80
	MaxBodyLen   = 4000
	MaxBioLen    = 160
	MaxBodyLines = 60
)

var (
	ErrHandleInvalid = errors.New("handles are 2–20 letters, digits, _ or -")
	ErrHandleTaken   = errors.New("that handle is taken")
	ErrEmptyTitle    = errors.New("a thread needs a title")
	ErrEmptyBody     = errors.New("say something first")
	ErrTooLong       = errors.New("that's too long")
	ErrNoAccount     = errors.New("create an account first")
	ErrNotFound      = errors.New("not found")
)

// CleanLine strips every control character (ESC included) and collapses the
// result to one line. Everything user-typed passes through Clean* before it is
// stored, so no post can smuggle escape sequences into other terminals.
func CleanLine(s string) string {
	s = strings.ToValidUTF8(s, "")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteRune(' ')
		case unicode.IsControl(r), isBidi(r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// CleanBody strips control characters but keeps newlines, trims trailing
// whitespace per line, and collapses runs of more than one blank line.
func CleanBody(s string) string {
	s = strings.ToValidUTF8(strings.ReplaceAll(s, "\r\n", "\n"), "")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteRune('\n')
		case r == '\t':
			b.WriteString("  ")
		case unicode.IsControl(r), isBidi(r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	lines := strings.Split(b.String(), "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, l := range lines {
		l = strings.TrimRightFunc(l, unicode.IsSpace)
		if l == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.Trim(strings.Join(out, "\n"), "\n ")
}

// isBidi reports Unicode bidi overrides/isolates, which can visually reorder
// a line in someone else's terminal.
func isBidi(r rune) bool {
	return r >= 0x202A && r <= 0x202E || r >= 0x2066 && r <= 0x2069 || r == 0x200E || r == 0x200F
}

// ValidHandle reports whether s is an acceptable handle.
func ValidHandle(s string) bool {
	n := utf8.RuneCountInString(s)
	if n < MinHandleLen || n > MaxHandleLen {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
