package middleware

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/abneribeiro/internal/httpx"
)

// RateLimitConfig describes a token bucket applied per client key.
type RateLimitConfig struct {
	// Burst is how many requests can arrive at once before throttling.
	Burst int
	// Refill is how long it takes to earn one more request.
	Refill time.Duration
	// TTL is how long an idle bucket is kept before the janitor drops it.
	TTL time.Duration
}

// LoginRateLimit guards password submission. Passwords are guessable and
// each attempt costs the server a bcrypt hash, so this one is strict.
func LoginRateLimit() RateLimitConfig {
	return RateLimitConfig{Burst: 10, Refill: 6 * time.Second, TTL: 10 * time.Minute}
}

// RefreshRateLimit guards token rotation. A refresh token is 256 bits of
// entropy, so it is not guessable the way a password is, and legitimate
// clients rotate on every access-token expiry. Throttling it as hard as
// login would lock real users out; this only bounds abuse.
func RefreshRateLimit() RateLimitConfig {
	return RateLimitConfig{Burst: 60, Refill: time.Second, TTL: 10 * time.Minute}
}

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// RateLimiter is a per-key token bucket. It is in-process, so it throttles
// one instance only; behind several replicas the effective limit multiplies
// by the replica count and a shared store would be needed.
type RateLimiter struct {
	cfg RateLimitConfig

	mu      sync.Mutex
	buckets map[string]*bucket

	// now is swappable so tests do not have to sleep.
	now func() time.Time
}

func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	return &RateLimiter{
		cfg:     cfg,
		buckets: make(map[string]*bucket),
		now:     time.Now,
	}
}

// Allow reports whether this key may proceed, and how long to wait if not.
func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()

	b, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &bucket{tokens: float64(l.cfg.Burst) - 1, lastSeen: now}
		return true, 0
	}

	// Refill proportionally to the time elapsed, capped at Burst.
	elapsed := now.Sub(b.lastSeen)
	b.tokens += elapsed.Seconds() / l.cfg.Refill.Seconds()
	if b.tokens > float64(l.cfg.Burst) {
		b.tokens = float64(l.cfg.Burst)
	}
	b.lastSeen = now

	if b.tokens < 1 {
		missing := 1 - b.tokens
		return false, time.Duration(missing * float64(l.cfg.Refill))
	}

	b.tokens--

	return true, 0
}

// Cleanup drops buckets no one has touched for longer than TTL, so a stream
// of distinct IPs cannot grow the map without bound.
func (l *RateLimiter) Cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := l.now().Add(-l.cfg.TTL)
	for key, b := range l.buckets {
		if b.lastSeen.Before(cutoff) {
			delete(l.buckets, key)
		}
	}
}

// Run sweeps idle buckets until ctx-driven shutdown closes done.
func (l *RateLimiter) Run(done <-chan struct{}) {
	ticker := time.NewTicker(l.cfg.TTL)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			l.Cleanup()
		}
	}
}

// Middleware throttles by client IP.
func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed, retryAfter := l.Allow(clientIP(r))
		if !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			httpx.WriteError(w, http.StatusTooManyRequests, "rate_limited",
				"too many requests, slow down and try again shortly")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// clientIP prefers the address chi's RealIP middleware resolved.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}
