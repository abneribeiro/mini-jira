package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testLimiter(cfg RateLimitConfig) (*RateLimiter, *time.Time) {
	now := time.Now()
	l := NewRateLimiter(cfg)
	l.now = func() time.Time { return now }

	return l, &now
}

func TestAllowBurstThenThrottle(t *testing.T) {
	l, _ := testLimiter(RateLimitConfig{Burst: 3, Refill: time.Second, TTL: time.Minute})

	for i := 1; i <= 3; i++ {
		if allowed, _ := l.Allow("1.2.3.4"); !allowed {
			t.Fatalf("request %d of the burst was throttled", i)
		}
	}

	allowed, retryAfter := l.Allow("1.2.3.4")
	if allowed {
		t.Error("the request past the burst was allowed")
	}
	if retryAfter <= 0 {
		t.Errorf("retryAfter = %v, want a positive wait", retryAfter)
	}
}

func TestBucketsAreIndependentPerKey(t *testing.T) {
	l, _ := testLimiter(RateLimitConfig{Burst: 1, Refill: time.Second, TTL: time.Minute})

	if allowed, _ := l.Allow("first"); !allowed {
		t.Fatal("first key was throttled immediately")
	}
	if allowed, _ := l.Allow("first"); allowed {
		t.Fatal("first key exceeded its burst")
	}

	// A different client must not inherit the first one's exhausted bucket.
	if allowed, _ := l.Allow("second"); !allowed {
		t.Error("second key was throttled by the first key's usage")
	}
}

func TestTokensRefillOverTime(t *testing.T) {
	l, now := testLimiter(RateLimitConfig{Burst: 1, Refill: time.Second, TTL: time.Minute})

	l.Allow("key")
	if allowed, _ := l.Allow("key"); allowed {
		t.Fatal("burst was not enforced")
	}

	*now = now.Add(time.Second)

	if allowed, _ := l.Allow("key"); !allowed {
		t.Error("a token should have refilled after one Refill interval")
	}
}

func TestRefillIsCappedAtBurst(t *testing.T) {
	l, now := testLimiter(RateLimitConfig{Burst: 2, Refill: time.Second, TTL: time.Minute})

	l.Allow("key")
	// Idle far longer than it takes to refill the whole bucket.
	*now = now.Add(time.Hour)

	for i := 1; i <= 2; i++ {
		if allowed, _ := l.Allow("key"); !allowed {
			t.Fatalf("request %d after a long idle was throttled", i)
		}
	}

	if allowed, _ := l.Allow("key"); allowed {
		t.Error("bucket refilled beyond Burst, so idling grants unlimited requests")
	}
}

// Without eviction a stream of distinct IPs grows the map forever.
func TestCleanupEvictsIdleBuckets(t *testing.T) {
	l, now := testLimiter(RateLimitConfig{Burst: 1, Refill: time.Second, TTL: time.Minute})

	l.Allow("stale")
	*now = now.Add(2 * time.Minute)
	l.Allow("fresh")

	l.Cleanup()

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, ok := l.buckets["stale"]; ok {
		t.Error("idle bucket was not evicted")
	}
	if _, ok := l.buckets["fresh"]; !ok {
		t.Error("active bucket was evicted")
	}
}

func TestMiddlewareReturns429WithRetryAfter(t *testing.T) {
	l, _ := testLimiter(RateLimitConfig{Burst: 1, Refill: 10 * time.Second, TTL: time.Minute})

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		req.RemoteAddr = "9.9.9.9:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		return rec
	}

	if rec := call(); rec.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", rec.Code)
	}

	rec := call()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 response has no Retry-After header")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestClientIPStripsPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.7:54321"

	if got := clientIP(req); got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want 203.0.113.7", got)
	}
}
