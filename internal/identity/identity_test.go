package identity

import (
	"strings"
	"testing"
)

func TestParseProxiedUsername(t *testing.T) {
	hex64 := strings.Repeat("ab", 32)
	cases := []struct {
		in   string
		slot string
		ok   bool
	}{
		{hex64 + ".default", "default", true},
		{hex64 + ".a_b-9", "a_b-9", true},
		{hex64 + "." + strings.Repeat("s", 32), strings.Repeat("s", 32), true},
		{hex64[:63] + ".x", "", false},                     // short hex
		{strings.ToUpper(hex64) + ".x", "", false},         // uppercase hex
		{hex64 + "x", "", false},                           // missing dot
		{hex64 + ".", "", false},                           // empty slot
		{hex64 + ".UP", "", false},                         // slot not sanitized
		{hex64 + ".a b", "", false},                        // space
		{hex64 + "." + strings.Repeat("s", 33), "", false}, // oversized
		{"g" + hex64[1:] + ".x", "", false},                // non-hex
		{"", "", false},
	}
	for _, c := range cases {
		_, slot, err := ParseProxiedUsername(c.in)
		if (err == nil) != c.ok || slot != c.slot {
			t.Errorf("Parse(%q) = %q, %v; want ok=%v slot=%q", c.in, slot, err, c.ok, c.slot)
		}
	}
}

func TestSanitizeSlot(t *testing.T) {
	for in, want := range map[string]string{
		"Nigel": "nigel", " a.b@c ": "abc", "": "", "!!!": "",
		strings.Repeat("x", 40): strings.Repeat("x", 32),
	} {
		if got := SanitizeSlot(in); got != want {
			t.Errorf("SanitizeSlot(%q) = %q, want %q", in, got, want)
		}
	}
}
