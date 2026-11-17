package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/config"
	"github.com/mynameis-nigel/ssh-bulletin/internal/identity"
	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
	"github.com/mynameis-nigel/ssh-bulletin/internal/version"
)

// These tests play the arcade router's half of
// ../ssh-arcadelobby/docs/02-bridge-and-identity-protocol.md against a real
// server: proxy key auth, `<fp-hex64>.<slot>` usernames, ARCADE_PLAYER_KEY,
// a PTY and a shell — exactly what bridge.Run sends.

func signer(t *testing.T) gossh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type fixture struct {
	addr  string
	st    *store.Store
	proxy gossh.Signer
}

func startServer(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	proxy := signer(t)
	proxyPath := filepath.Join(dir, "proxy_keys")
	if err := os.WriteFile(proxyPath, gossh.MarshalAuthorizedKey(proxy.PublicKey()), 0o600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	cfg := config.Config{
		ListenHost: "127.0.0.1", ListenPort: port,
		HostKeyPath: filepath.Join(dir, "host_key"), IdleTimeout: time.Minute,
		MaxSessionsPerKey: 3, MaxConnections: 20,
		RateLimitPerSecond: 100, RateLimitBurst: 100, RateLimitMaxEntries: 100,
		DBPath: filepath.Join(dir, "bulletin.db"), ProxyKeysPath: proxyPath,
	}
	st, err := store.Open(context.Background(), cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := identity.LoadProxyKeys(proxyPath)
	if err != nil {
		t.Fatal(err)
	}
	svc := board.NewService(st, board.NewBroker(), time.Now)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := New(cfg, logger, svc, identity.NewResolver(keys))
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.ListenAndServe() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		_ = st.Close()
	})
	addr := cfg.ListenAddr()
	for range 50 {
		if c, err := net.Dial("tcp", addr); err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fixture{addr: addr, st: st, proxy: proxy}
}

// session is one interactive connection, as the router opens it.
type session struct {
	client *gossh.Client
	sess   *gossh.Session
	stdin  io.WriteCloser
	mu     sync.Mutex
	out    strings.Builder
}

func (s *session) output() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String()
}

func dial(t *testing.T, addr, user string, auth gossh.Signer, playerKey gossh.PublicKey) *session {
	t.Helper()
	client, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{
		User: user, Auth: []gossh.AuthMethod{gossh.PublicKeys(auth)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if playerKey != nil {
		_ = sess.Setenv("ARCADE_PLAYER_KEY", strings.TrimSpace(string(gossh.MarshalAuthorizedKey(playerKey))))
	}
	if err := sess.RequestPty("xterm-256color", 40, 120, gossh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	stdin, _ := sess.StdinPipe()
	stdout, _ := sess.StdoutPipe()
	s := &session{client: client, sess: sess, stdin: stdin}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			s.mu.Lock()
			s.out.Write(buf[:n])
			s.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	if err := sess.Shell(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close(); _ = client.Close() })
	return s
}

// press types keys with a pause, as a person (or the bridge) would.
func (s *session) press(keys ...string) {
	for _, k := range keys {
		time.Sleep(250 * time.Millisecond)
		_, _ = io.WriteString(s.stdin, k)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func accounts(t *testing.T, st *store.Store) int64 {
	t.Helper()
	s, err := st.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s.Accounts
}

// TestAccountIsTheKeyNotTheUsername is the bulletin's core identity rule:
// one key is one account whether it arrives through the arcade under any
// slot, or directly.
func TestAccountIsTheKeyNotTheUsername(t *testing.T) {
	f := startServer(t)
	player := signer(t)
	fp := identity.Fingerprint(player.PublicKey())

	// 1. Through the arcade as "alice": splash → onboarding (handle
	// prefilled from the slot) → enter, enter → account created.
	s1 := dial(t, f.addr, identity.EncodeProxiedUsername(player.PublicKey(), "alice"), f.proxy, player.PublicKey())
	s1.press("x", "\r", "\r")
	waitFor(t, "registration", func() bool {
		a, err := f.st.AccountByFingerprint(context.Background(), fp)
		return err == nil && a.Handle == "alice"
	})
	a, _ := f.st.AccountByFingerprint(context.Background(), fp)
	if !strings.Contains(a.PublicKey, "ssh-ed25519") {
		t.Errorf("ARCADE_PLAYER_KEY not stored: %q", a.PublicKey)
	}
	_ = s1.sess.Close()

	// 2. Same key, different slot. If this were treated as a new account
	// the same keystrokes would register "bob".
	s2 := dial(t, f.addr, identity.EncodeProxiedUsername(player.PublicKey(), "bob"), f.proxy, player.PublicKey())
	s2.press("x", "\r", "\r")
	// 3. Same key, direct (dev path, no router).
	s3 := dial(t, f.addr, "carol", player, nil)
	s3.press("x", "\r", "\r")
	time.Sleep(600 * time.Millisecond)

	if n := accounts(t, f.st); n != 1 {
		t.Fatalf("one key must be one account, got %d accounts", n)
	}
}

// TestProxyRefusals covers every proxied identity the protocol says to
// refuse, plus the untrusted-key forgery attempt.
func TestProxyRefusals(t *testing.T) {
	f := startServer(t)
	player := signer(t)
	cases := []struct {
		name, user, want string
	}{
		{"malformed", "not-a-fingerprint", "Could not verify"},
		{"uppercase hex", strings.ToUpper(identity.EncodeProxiedUsername(player.PublicKey(), "a")[:64]) + ".a", "Could not verify"},
		{"bad slot", identity.EncodeProxiedUsername(player.PublicKey(), "a")[:65] + "A!", "Could not verify"},
		{"guest", "guest.0123456789abcdef0123456789abcdef", "guests can't sign in"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := dial(t, f.addr, c.user, f.proxy, nil)
			waitFor(t, c.want, func() bool { return strings.Contains(s.output(), c.want) })
		})
	}
	if n := accounts(t, f.st); n != 0 {
		t.Fatalf("refused sessions created %d accounts", n)
	}
}

// TestUntrustedKeyCannotForge: a direct connection presenting a compound
// username lands on its own key's identity, never the named fingerprint.
func TestUntrustedKeyCannotForge(t *testing.T) {
	f := startServer(t)
	victim, attacker := signer(t), signer(t)
	s := dial(t, f.addr, identity.EncodeProxiedUsername(victim.PublicKey(), "mallory"), attacker, nil)
	// The compound username is not a valid handle, so nothing is prefilled:
	// type one, so registration really happens and the assertion below
	// cannot pass merely because no account was created at all.
	s.press("x", "mallory", "\r", "\r")
	waitFor(t, "registration", func() bool { return accounts(t, f.st) == 1 })
	if _, err := f.st.AccountByFingerprint(context.Background(), identity.Fingerprint(victim.PublicKey())); err == nil {
		t.Fatal("attacker created an account under the victim's fingerprint")
	}
	if _, err := f.st.AccountByFingerprint(context.Background(), identity.Fingerprint(attacker.PublicKey())); err != nil {
		t.Fatalf("the account must belong to the key that authenticated: %v", err)
	}
}

// TestBannerCarriesVersion: the router's prober reads the version from the
// SSH banner.
func TestBannerCarriesVersion(t *testing.T) {
	old := version.Version
	version.Version = "9.8.7"
	t.Cleanup(func() { version.Version = old })
	f := startServer(t)
	s := dial(t, f.addr, "probe", signer(t), nil)
	if got := string(s.client.ServerVersion()); got != "SSH-2.0-9.8.7" {
		t.Fatalf("banner = %q", got)
	}
}
