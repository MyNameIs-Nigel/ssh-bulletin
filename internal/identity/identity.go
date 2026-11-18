// Package identity resolves which account an SSH session belongs to.
//
// The bulletin's account is the player's SSH public key and nothing else.
// Farm and Moon Miner key saves on (fingerprint, slot), where the slot is the
// SSH username; the bulletin deliberately does not — `ssh alice@…` and
// `ssh bob@…` with the same key are the same poster. The slot is still parsed
// strictly on proxied connections (the wire contract in
// ../ssh-arcadelobby/docs/02-bridge-and-identity-protocol.md requires it) and
// is offered as a suggested handle on first run, but it never selects the
// account.
package identity

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/ssh"
	gossh "golang.org/x/crypto/ssh"
)

const maxSlotLen = 32

// ErrProxiedIdentity means a trusted proxy sent an identity that does not
// match protocol v2. The session is refused; it never falls back to treating
// the router's own key as an account.
var ErrProxiedIdentity = errors.New("proxied identity protocol error")

// ErrGuestRefused is returned for `guest.<token>` assertions. A bulletin post
// needs an author who can come back to it, so guests are refused fail-closed
// (games.toml carries allow_guests = false, so the lobby never sends one).
var ErrGuestRefused = errors.New("guest sessions are not supported")

// ErrNoKey means the session did not authenticate with a public key.
var ErrNoKey = errors.New("public key authentication is required")

// Resolved is the outcome of resolving one session.
type Resolved struct {
	Fingerprint     string // canonical "SHA256:<base64>" — the account key
	SuggestedHandle string // the SSH username / slot, for first-run only
	PublicKey       string // authorized_keys format; may be empty
	Proxied         bool
}

// RefusalMessage is the player-facing line written before closing a session
// whose identity could not be resolved.
func RefusalMessage(err error) string {
	switch {
	case errors.Is(err, ErrGuestRefused):
		return "The bulletin needs a key to post under — guests can't sign in here.\r\n" +
			"Connect with an SSH key to create your account.\r\n"
	case errors.Is(err, ErrProxiedIdentity):
		return "Could not verify who you are through the arcade.\r\nPlease reconnect from the lobby.\r\n"
	default:
		return "Public key authentication is required.\r\n"
	}
}

// Fingerprint returns the canonical SHA-256 fingerprint for a public key.
func Fingerprint(key ssh.PublicKey) string {
	if key == nil {
		return ""
	}
	return FingerprintFromBytes(sha256.Sum256(key.Marshal()))
}

// FingerprintFromBytes encodes raw sha256 key bytes in the canonical format,
// identical to `ssh-keygen -l`, so an account is the same whether the player
// arrives through the arcade or directly.
func FingerprintFromBytes(sum [32]byte) string {
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// ValidFingerprint checks the complete canonical OpenSSH SHA-256 fingerprint.
func ValidFingerprint(fp string) bool {
	raw, ok := strings.CutPrefix(fp, "SHA256:")
	if !ok {
		return false
	}
	sum, err := base64.RawStdEncoding.Strict().DecodeString(raw)
	return err == nil && len(sum) == sha256.Size && FingerprintFromBytes([32]byte(sum)) == fp
}

// SanitizeSlot normalizes an SSH username: lowercase, [a-z0-9_-], 1–32 chars.
func SanitizeSlot(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	for _, r := range raw {
		if b.Len() >= maxSlotLen {
			break
		}
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ParseProxiedUsername decodes `<fp-hex64>.<slot>` strictly per protocol v2.
func ParseProxiedUsername(username string) ([32]byte, string, error) {
	var zero [32]byte
	switch {
	case len(username) < 66:
		return zero, "", fmt.Errorf("%w: username too short", ErrProxiedIdentity)
	case len(username) > 97:
		return zero, "", fmt.Errorf("%w: username too long", ErrProxiedIdentity)
	case username[64] != '.':
		return zero, "", fmt.Errorf("%w: missing separator", ErrProxiedIdentity)
	}
	for _, c := range username[:64] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return zero, "", fmt.Errorf("%w: invalid fingerprint hex", ErrProxiedIdentity)
		}
	}
	raw, err := hex.DecodeString(username[:64])
	if err != nil || len(raw) != 32 {
		return zero, "", fmt.Errorf("%w: invalid fingerprint hex", ErrProxiedIdentity)
	}
	slot := username[65:]
	if slot == "" || SanitizeSlot(slot) != slot {
		return zero, "", fmt.Errorf("%w: invalid slot %q", ErrProxiedIdentity, slot)
	}
	var fp [32]byte
	copy(fp[:], raw)
	return fp, slot, nil
}

// EncodeProxiedUsername builds a protocol-v2 username (tests and tooling).
func EncodeProxiedUsername(key ssh.PublicKey, slot string) string {
	sum := sha256.Sum256(key.Marshal())
	return hex.EncodeToString(sum[:]) + "." + slot
}

// LoadProxyKeys reads trusted proxy public keys from an authorized_keys file.
// An empty path yields no keys (direct-only dev mode).
func LoadProxyKeys(path string) ([]ssh.PublicKey, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read proxy keys %q: %w", path, err)
	}
	var keys []ssh.PublicKey
	rest := data
	for len(bytes.TrimSpace(rest)) > 0 {
		key, _, _, remaining, err := gossh.ParseAuthorizedKey(rest)
		if err != nil {
			return nil, fmt.Errorf("parse proxy keys %q: %w", path, err)
		}
		keys = append(keys, key)
		rest = remaining
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("parse proxy keys %q: no keys found", path)
	}
	return keys, nil
}

// Session is the subset of ssh.Session identity resolution needs.
type Session interface {
	User() string
	PublicKey() ssh.PublicKey
	Environ() []string
}

// Resolver resolves direct and proxied sessions.
type Resolver struct{ proxyKeys []ssh.PublicKey }

// NewResolver builds a resolver trusting the given proxy keys.
func NewResolver(proxyKeys []ssh.PublicKey) *Resolver { return &Resolver{proxyKeys: proxyKeys} }

// Resolve derives the account fingerprint for a session.
func (r *Resolver) Resolve(s Session) (Resolved, error) {
	wire := s.PublicKey()
	if wire == nil {
		return Resolved{}, ErrNoKey
	}
	if !r.isProxyKey(wire) {
		return Resolved{
			Fingerprint:     Fingerprint(wire),
			SuggestedHandle: SanitizeSlot(s.User()),
			PublicKey:       string(gossh.MarshalAuthorizedKey(wire)),
		}, nil
	}

	user := s.User()
	if strings.HasPrefix(user, "guest.") {
		return Resolved{}, fmt.Errorf("%w: %w", ErrGuestRefused, ErrProxiedIdentity)
	}
	fp, slot, err := ParseProxiedUsername(user)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{
		Fingerprint:     FingerprintFromBytes(fp),
		SuggestedHandle: slot,
		PublicKey:       strings.TrimSpace(envValue(s.Environ(), "ARCADE_PLAYER_KEY")),
		Proxied:         true,
	}, nil
}

func (r *Resolver) isProxyKey(key ssh.PublicKey) bool {
	wire := key.Marshal()
	for _, pk := range r.proxyKeys {
		if bytes.Equal(wire, pk.Marshal()) {
			return true
		}
	}
	return false
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):]
		}
	}
	return ""
}
