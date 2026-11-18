package server

import (
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"

	"charm.land/ssh"

	"github.com/mynameis-nigel/ssh-bulletin/internal/identity"
)

// SessionLimits tracks global, per-key, and direct network concurrent sessions.
type SessionLimits struct {
	mu           sync.Mutex
	global       int
	perKey       map[string]int
	perSource    map[netip.Prefix]int
	maxGlobal    int
	maxPerKey    int
	maxPerSource int
	resolver     *identity.Resolver
}

func NewSessionLimits(maxGlobal, maxPerKey, maxPerSource int, resolver *identity.Resolver) *SessionLimits {
	return &SessionLimits{
		perKey:       make(map[string]int),
		perSource:    make(map[netip.Prefix]int),
		maxGlobal:    maxGlobal,
		maxPerKey:    maxPerKey,
		maxPerSource: maxPerSource,
		resolver:     resolver,
	}
}

func (l *SessionLimits) Middleware() func(ssh.Handler) ssh.Handler {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			resolved, err := l.resolver.Resolve(s)
			if err != nil {
				_, _ = io.WriteString(s, identity.RefusalMessage(err))
				s.Exit(1)
				return
			}
			// The router's address is shared by all proxied sessions.
			var source netip.Prefix
			if !resolved.Proxied {
				source = sourceNetwork(s.RemoteAddr())
			}
			fp := resolved.Fingerprint
			if refusal := l.acquire(fp, source); refusal != "" {
				_, _ = io.WriteString(s, refusal+" Try again later.\r\n")
				s.Exit(1)
				return
			}
			defer l.release(fp, source)
			next(s)
		}
	}
}

func sourceNetwork(addr net.Addr) netip.Prefix {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return netip.Prefix{}
	}
	ip := tcp.AddrPort().Addr().Unmap().WithZone("")
	bits := 64
	if ip.Is4() {
		bits = 32
	}
	return netip.PrefixFrom(ip, bits).Masked()
}

func (l *SessionLimits) acquire(fp string, source netip.Prefix) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.global >= l.maxGlobal:
		return fmt.Sprintf("Too many active sessions (global max %d).", l.maxGlobal)
	case l.perKey[fp] >= l.maxPerKey:
		return fmt.Sprintf("Too many active sessions (per-key max %d).", l.maxPerKey)
	case source.IsValid() && l.maxPerSource > 0 && l.perSource[source] >= l.maxPerSource:
		return fmt.Sprintf("Too many active sessions from this network (max %d).", l.maxPerSource)
	}
	l.global++
	l.perKey[fp]++
	if source.IsValid() {
		l.perSource[source]++
	}
	return ""
}

func (l *SessionLimits) release(fp string, source netip.Prefix) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.global--
	l.perKey[fp]--
	if l.perKey[fp] == 0 {
		delete(l.perKey, fp)
	}
	if source.IsValid() {
		l.perSource[source]--
		if l.perSource[source] == 0 {
			delete(l.perSource, source)
		}
	}
}
