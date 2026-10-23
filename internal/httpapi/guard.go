package httpapi

import (
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Hardening for a public site: browser security headers on every response,
// and per-client rate limits on the API so one script can't flood the
// database with events, run up Google Places bills or starve the pod's CPU.

// headers sets the browser security headers. The CSP only covers framing and
// plugins: the shop loads Stripe, three.js, fonts and CDN images from many
// origins, and SvelteKit inlines its boot script.
func (s *server) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'; base-uri 'self'; object-src 'none'")
		if s.secure(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// Per-client token buckets. They're generous because a whole school can share
// one address. Costly routes cost money (address lookups) or CPU (estimates),
// or start something (sign-in, requests, payments, submissions).
const (
	generalBurst = 600
	generalRate  = 10 // per second
	costlyBurst  = 120
	costlyRate   = 2
	maxBuckets   = 50000 // past this, idle clients are forgotten
)

func costly(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/api/address/"), p == "/auth/login":
		return true
	case r.Method != http.MethodPost:
		return false
	case p == "/api/shipping/estimate", p == "/api/requests", p == "/api/author/submissions",
		p == "/api/print/files", p == "/api/print-requests":
		return true
	case strings.HasPrefix(p, "/api/requests/") && strings.HasSuffix(p, "/pay"):
		return true
	}
	return false
}

// limit rate-limits /api and /auth calls per client when Deps.RateLimit is on.
func (s *server) limit(next http.Handler) http.Handler {
	if !s.RateLimit {
		return next
	}
	general := newLimiter(generalBurst, generalRate, s.Now)
	expensive := newLimiter(costlyBurst, costlyRate, s.Now)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		ip := clientIP(r)
		if !general.allow(ip) || (costly(r) && !expensive.allow(ip)) {
			w.Header().Set("Retry-After", "30")
			writeErr(w, http.StatusTooManyRequests, "too many requests; wait a minute and try again")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP is the address the ingress saw: the last X-Forwarded-For entry
// (HAProxy appends it; earlier entries are whatever the client sent), or the
// connection's address without a proxy.
func clientIP(r *http.Request) string {
	if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
		parts := strings.Split(xff[len(xff)-1], ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

type bucket struct {
	tokens float64
	at     time.Time
}

type limiter struct {
	mu      sync.Mutex
	burst   float64
	rate    float64
	now     func() time.Time
	buckets map[string]*bucket
}

func newLimiter(burst, perSecond float64, now func() time.Time) *limiter {
	return &limiter{burst: burst, rate: perSecond, now: now, buckets: map[string]*bucket{}}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.buckets) >= maxBuckets {
		l.forgetIdle(now)
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, at: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.at).Seconds()*l.rate)
	b.at = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// forgetIdle drops buckets that have refilled: those clients start full anyway.
func (l *limiter) forgetIdle(now time.Time) {
	for k, b := range l.buckets {
		if b.tokens+now.Sub(b.at).Seconds()*l.rate >= l.burst {
			delete(l.buckets, k)
		}
	}
}
