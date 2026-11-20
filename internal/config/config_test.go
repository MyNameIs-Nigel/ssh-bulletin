package config

import (
	"strings"
	"testing"

	"github.com/mynameis-nigel/ssh-bulletin/internal/identity"
)

func TestOwnerFingerprintConfig(t *testing.T) {
	owner := identity.FingerprintFromBytes([32]byte{1})
	for _, tt := range []struct {
		name, dev, fingerprint, wantError string
	}{
		{"production requires key", "false", "", "required"},
		{"production accepts key", "false", owner, ""},
		{"development can omit key", "true", "", ""},
		{"development can reserve key", "true", owner, ""},
		{"partial key rejected", "false", owner[:25], "full OpenSSH"},
		{"malformed development key rejected", "true", "SHA256:invalid", "full OpenSSH"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BULLETIN_DEV_MODE", tt.dev)
			t.Setenv("BULLETIN_OWNER_FINGERPRINT", tt.fingerprint)
			cfg, err := Load()
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("Load error = %v, want %q", err, tt.wantError)
				}
				return
			}
			if err != nil || cfg.OwnerFingerprint != tt.fingerprint {
				t.Fatalf("Load owner = %q, error = %v", cfg.OwnerFingerprint, err)
			}
		})
	}
}
