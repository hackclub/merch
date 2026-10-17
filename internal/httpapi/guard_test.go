package httpapi

import (
	"net/http"
	"testing"
	"time"
)

func TestSecurityHeaders(t *testing.T) {
	h := newHarness(t)
	for _, p := range []string{"/", "/api/catalog", "/healthz"} {
		resp, err := http.Get(h.srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		for k, want := range map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
			"Referrer-Policy":         "strict-origin-when-cross-origin",
			"Content-Security-Policy": "frame-ancestors 'none'; base-uri 'self'; object-src 'none'",
		} {
			if got := resp.Header.Get(k); got != want {
				t.Errorf("%s %s = %q, want %q", p, k, got, want)
			}
		}
		// HSTS only on an https origin; the harness is http.
		if got := resp.Header.Get("Strict-Transport-Security"); got != "" {
			t.Errorf("%s HSTS on http = %q", p, got)
		}
	}
}

func TestHSTSOnHTTPS(t *testing.T) {
	h := newHarness(t, func(d *Deps) { d.Config.BaseURL = "https://merch.test" })
	req, _ := http.NewRequest("GET", h.srv.URL+"/", nil)
	req.Header.Set("X-Forwarded-Host", "merch.test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Fatalf("HSTS = %q", got)
	}
}

func TestRateLimit(t *testing.T) {
	at := time.Now() // stopped, so buckets don't refill mid-test
	h := newHarness(t, func(d *Deps) { d.RateLimit = true; d.Now = func() time.Time { return at } })
	get := func(path, ip string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", h.srv.URL+path, nil)
		if ip != "" {
			// The ingress appends the client's address; anything before it is what the client sent.
			req.Header.Set("X-Forwarded-For", "6.6.6.6, "+ip)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	// Costly routes (address lookups cost money, estimates cost CPU) have a smaller bucket.
	for i := range costlyBurst {
		if resp := get("/api/address/suggest?q=1", "1.1.1.1"); resp.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("call %d limited too soon", i)
		}
	}
	resp := get("/api/address/suggest?q=1", "1.1.1.1")
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("over the costly limit = %d (Retry-After %q)", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	// Each client has its own bucket, whatever it claims earlier in X-Forwarded-For.
	if resp := get("/api/address/suggest?q=1", "2.2.2.2"); resp.StatusCode == http.StatusTooManyRequests {
		t.Fatal("another client was limited")
	}
	// Cheap routes still answer the first client, and health checks are never limited.
	if resp := get("/api/catalog", "1.1.1.1"); resp.StatusCode != 200 {
		t.Fatalf("catalog after costly limit = %d", resp.StatusCode)
	}
	for range generalBurst + 5 {
		get("/api/me", "3.3.3.3")
	}
	if resp := get("/api/me", "3.3.3.3"); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("over the general limit = %d", resp.StatusCode)
	}
	if resp := get("/healthz", "3.3.3.3"); resp.StatusCode != 200 {
		t.Fatalf("healthz limited = %d", resp.StatusCode)
	}
	// Limited calls are logged like any other API call, so 429s show up in usage_events.
	n := 0
	for _, e := range h.events("api") {
		if props(t, e)["status"] == float64(http.StatusTooManyRequests) {
			n++
		}
	}
	if n == 0 {
		t.Fatal("no api event for a 429")
	}
}

func TestBucketRefills(t *testing.T) {
	now := time.Unix(0, 0)
	l := newLimiter(2, 1, func() time.Time { return now })
	if !l.allow("a") || !l.allow("a") || l.allow("a") {
		t.Fatal("burst of 2 not enforced")
	}
	now = now.Add(time.Second)
	if !l.allow("a") || l.allow("a") {
		t.Fatal("didn't refill one token a second")
	}
}

func TestLimiterForgetsIdleClients(t *testing.T) {
	now := time.Unix(0, 0)
	l := newLimiter(2, 1, func() time.Time { return now })
	for i := range maxBuckets {
		l.allow(string(rune(i)))
	}
	now = now.Add(time.Minute)
	l.allow("new")
	if len(l.buckets) > 1 {
		t.Fatalf("kept %d idle buckets", len(l.buckets))
	}
}
