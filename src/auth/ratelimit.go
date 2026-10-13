package auth

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrRateLimited means too many failed logins from this IP and username.
var ErrRateLimited = errors.New("too many failed login attempts")

const (
	defaultMaxFailures   = 5
	defaultIPMaxFailures = 30
	defaultWindow        = 15 * time.Minute
	maxLimiterKeys       = 10000
	evictBatch           = maxLimiterKeys / 10
)

type failures struct {
	n     int
	first time.Time // start of the current window
}

// LoginLimiter counts failed logins per IP+username and per IP in memory. State is lost on restart,
// which is acceptable for a brake on guessing.
type LoginLimiter struct {
	mu            sync.Mutex
	entries       map[string]*failures
	maxFailures   int
	ipMaxFailures int
	window        time.Duration
	now           func() time.Time
}

func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{entries: map[string]*failures{}, maxFailures: defaultMaxFailures, ipMaxFailures: defaultIPMaxFailures, window: defaultWindow, now: time.Now}
}

// normaliseUsername makes DOMAIN\Bob, bob@corp and " BOB " share one counter, so varying the
// spelling does not buy extra attempts.
func normaliseUsername(u string) string {
	u = strings.ToLower(strings.TrimSpace(u))
	if i := strings.Index(u, `\`); i >= 0 {
		u = u[i+1:]
	}
	if i := strings.Index(u, "@"); i >= 0 {
		u = u[:i]
	}
	return u
}

func limiterKey(ip, username string) string {
	return ip + "|" + normaliseUsername(username)
}

// Allow returns ErrRateLimited when either the IP+username key or the IP alone has used up its
// failures in the current window.
func (l *LoginLimiter) Allow(ip, username string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now()
	if l.exhausted(limiterKey(ip, username), l.maxFailures, t) || l.exhausted(ip, l.ipMaxFailures, t) {
		return ErrRateLimited
	}
	return nil
}

func (l *LoginLimiter) exhausted(k string, max int, t time.Time) bool {
	f, ok := l.entries[k]
	return ok && t.Sub(f.first) < l.window && f.n >= max
}

// Failure records a failed login against both counters.
func (l *LoginLimiter) Failure(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now()
	l.bump(limiterKey(ip, username), t)
	l.bump(ip, t)
}

func (l *LoginLimiter) bump(k string, t time.Time) {
	f, ok := l.entries[k]
	if !ok || t.Sub(f.first) >= l.window {
		if !ok && len(l.entries) >= maxLimiterKeys {
			l.makeRoom(t)
		}
		f = &failures{first: t}
		l.entries[k] = f
	}
	f.n++
}

// Success clears the IP+username key only; the per-IP counter stays so one valid login cannot
// reset the budget of an IP that is guessing other accounts.
func (l *LoginLimiter) Success(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, limiterKey(ip, username))
}

// makeRoom drops expired entries, then, if still full of live ones, the oldest by window start.
func (l *LoginLimiter) makeRoom(t time.Time) {
	for k, f := range l.entries {
		if t.Sub(f.first) >= l.window {
			delete(l.entries, k)
		}
	}
	if len(l.entries) < maxLimiterKeys {
		return
	}
	keys := make([]string, 0, len(l.entries))
	for k := range l.entries {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return l.entries[keys[i]].first.Before(l.entries[keys[j]].first) })
	for _, k := range keys[:evictBatch] {
		delete(l.entries, k)
	}
}
