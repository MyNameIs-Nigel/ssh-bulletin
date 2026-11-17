package board

import (
	"strings"
	"testing"
)

func TestCleanStripsEscapes(t *testing.T) {
	evil := "hi\x1b[2J\x1b]0;pwned\x07 there‮\r\nnext\x00"
	if got := CleanLine(evil); strings.ContainsAny(got, "\x1b\x07\x00‮\n") || got != "hi[2J]0;pwned there next" {
		t.Fatalf("CleanLine = %q", got)
	}
	body := CleanBody("a\x1b[31m\n\n\n\nb\t\n")
	if body != "a[31m\n\nb" {
		t.Fatalf("CleanBody = %q", body)
	}
}

func TestValidHandle(t *testing.T) {
	for h, ok := range map[string]bool{"ok": true, "a": false, "Nigel_99": true, "has space": false,
		"ünï": false, strings.Repeat("x", 21): false, "dash-ok": true} {
		if ValidHandle(h) != ok {
			t.Errorf("ValidHandle(%q) != %v", h, ok)
		}
	}
}
