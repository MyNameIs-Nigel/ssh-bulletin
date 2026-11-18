// Package server is the Wish SSH front door: identity resolution, session
// caps, PTY enforcement, and one Bubble Tea program per session. There is no
// shell — the TUI is the only thing a session can reach.
package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/ssh"
	"charm.land/wish/v2"
	"charm.land/wish/v2/bubbletea"
	"charm.land/wish/v2/logging"
	"charm.land/wish/v2/ratelimiter"
	"github.com/charmbracelet/colorprofile"
	"golang.org/x/time/rate"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/config"
	"github.com/mynameis-nigel/ssh-bulletin/internal/identity"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui"
	"github.com/mynameis-nigel/ssh-bulletin/internal/version"
)

type sessionKey struct{}

type sessionState struct {
	resolved identity.Resolved
	account  *board.Account
	events   <-chan board.Event
}

// Server wraps the Wish SSH server.
type Server struct {
	cfg      config.Config
	logger   *slog.Logger
	ssh      *ssh.Server
	svc      *board.Service
	identity *identity.Resolver
	version  string // read once at construction; sessions never touch the global
}

// New constructs and configures the SSH server.
func New(cfg config.Config, logger *slog.Logger, svc *board.Service, resolver *identity.Resolver) (*Server, error) {
	if dir := filepath.Dir(cfg.HostKeyPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	limits := NewSessionLimits(cfg.MaxConnections, cfg.MaxSessionsPerKey, cfg.MaxSessionsPerSource, resolver)
	rl := ratelimiter.NewRateLimiter(rate.Limit(cfg.RateLimitPerSecond), cfg.RateLimitBurst, cfg.RateLimitMaxEntries)
	srv := &Server{cfg: cfg, logger: logger, svc: svc, identity: resolver, version: version.Version}

	s, err := wish.NewServer(
		wish.WithAddress(cfg.ListenAddr()),
		wish.WithHostKeyPath(cfg.HostKeyPath),
		wish.WithIdleTimeout(cfg.IdleTimeout),
		withVersion(srv.version),
		wish.WithPublicKeyAuth(func(_ ssh.Context, key ssh.PublicKey) bool { return key != nil }),
		// Middlewares run bottom-up: logging → rate limit → scope → caps →
		// PTY → account attach → the TUI.
		wish.WithMiddleware(
			bubbletea.MiddlewareWithProgramHandler(srv.newProgram),
			srv.attach(),
			RequirePTY(),
			limits.Middleware(),
			scopeSession(),
			ratelimiter.Middleware(rl),
			logging.Middleware(),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create ssh server: %w", err)
	}
	srv.ssh = s
	return srv, nil
}

// withVersion embeds the build version in the SSH banner ("SSH-2.0-1.2.3"),
// which the arcade router's health prober reads on every probe cycle
// (../ssh-arcadelobby/docs/03-games-registry-and-health.md).
func withVersion(v string) ssh.Option {
	return func(srv *ssh.Server) error {
		srv.Version = v
		return nil
	}
}

// attach resolves the session's account (which may not exist yet — first
// run creates it inside the TUI) and subscribes it to live board events for
// exactly as long as the session lives.
func (srv *Server) attach() func(ssh.Handler) ssh.Handler {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			resolved, err := srv.identity.Resolve(s)
			if err != nil {
				srv.logger.Warn("identity resolution failed", "error", err)
				_, _ = io.WriteString(s, identity.RefusalMessage(err))
				s.Exit(1)
				return
			}
			ctx, cancel := context.WithTimeout(s.Context(), 5*time.Second)
			acct, ok, err := srv.svc.Connect(ctx, resolved.Fingerprint)
			cancel()
			if err != nil {
				srv.logger.Error("account lookup failed", "error", err)
				_, _ = io.WriteString(s, "The bulletin could not be opened just now. Please try again shortly.\r\n")
				s.Exit(1)
				return
			}
			state := &sessionState{resolved: resolved}
			if ok {
				_ = srv.svc.Touch(s.Context(), acct.ID, resolved.PublicKey)
				state.account = &acct
			}
			events, unsubscribe := srv.svc.Broker().Subscribe()
			defer unsubscribe()
			state.events = events

			srv.logger.Info("session start",
				"fingerprint", shortFP(resolved.Fingerprint),
				"registered", ok,
				"proxied", resolved.Proxied)
			started := time.Now()
			s.Context().SetValue(sessionKey{}, state)
			next(s)
			srv.logger.Info("session end", "fingerprint", shortFP(resolved.Fingerprint), "duration", time.Since(started).Round(time.Second))
		}
	}
}

func shortFP(fp string) string {
	if len(fp) > 19 {
		return fp[:19]
	}
	return fp
}

// newProgram builds the per-session Bubble Tea program. TrueColor is forced
// last: the gradients are the product, and the bridge forwards only TERM, so
// detection would otherwise guess low.
func (srv *Server) newProgram(s ssh.Session) *tea.Program {
	state, _ := s.Context().Value(sessionKey{}).(*sessionState)
	if state == nil {
		return nil
	}
	w, h := 80, 24
	if pty, _, ok := s.Pty(); ok {
		w, h = pty.Window.Width, pty.Window.Height
	}
	model := tui.New(tui.Deps{
		Service:     srv.svc,
		Fingerprint: state.resolved.Fingerprint,
		PublicKey:   state.resolved.PublicKey,
		Suggested:   state.resolved.SuggestedHandle,
		Account:     state.account,
		Events:      state.events,
		Width:       w,
		Height:      h,
		IdleTimeout: srv.cfg.IdleTimeout,
		Context:     s.Context(),
		Version:     srv.version,
	})
	opts := append(bubbletea.MakeOptions(s), tea.WithColorProfile(colorprofile.TrueColor))
	return tea.NewProgram(model, opts...)
}

// ListenAndServe starts accepting connections.
func (srv *Server) ListenAndServe() error {
	srv.logger.Info("ssh server listening", "addr", srv.cfg.ListenAddr(), "version", srv.version)
	return srv.ssh.ListenAndServe()
}

// Shutdown stops accepting connections and waits for sessions.
func (srv *Server) Shutdown(ctx context.Context) error { return srv.ssh.Shutdown(ctx) }
