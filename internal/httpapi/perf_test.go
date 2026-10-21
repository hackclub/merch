package httpapi

import (
	"compress/gzip"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"
)

// JSON from the API is gzipped when the browser takes it: the catalog is about 65 KB raw and
// 14 KB gzipped, which fits in the first round trip to a server in Finland.
func TestAPIResponsesAreGzipped(t *testing.T) {
	h := newHarness(t)
	h.mkItem("Sti/A", true)
	get := func(enc string) *http.Response {
		req, _ := http.NewRequest("GET", h.srv.URL+"/api/catalog", nil)
		if enc != "" {
			req.Header.Set("Accept-Encoding", enc)
		}
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

	resp := get("gzip, deflate, br")
	if resp.Header.Get("Content-Encoding") != "gzip" || resp.Header.Get("Vary") != "Accept-Encoding" {
		t.Fatalf("headers = %v", resp.Header)
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var cat struct{ Items []map[string]any }
	if err := json.NewDecoder(zr).Decode(&cat); err != nil || len(cat.Items) != 1 {
		t.Fatalf("decode = %v %+v", err, cat)
	}

	plain := get("")
	if plain.Header.Get("Content-Encoding") != "" {
		t.Fatalf("plain headers = %v", plain.Header)
	}
	if err := json.NewDecoder(plain.Body).Decode(&cat); err != nil {
		t.Fatal(err)
	}
	if refused := get("gzip;q=0"); refused.Header.Get("Content-Encoding") != "" {
		t.Fatalf("gzip;q=0 headers = %v", refused.Header)
	}
}

// Once the warehouse's SKU list is a minute old, the catalog still answers at once with it and
// fetches a fresh one in the background, instead of making a visitor wait on the warehouse.
func TestStaleSKUsServeWhileRefreshing(t *testing.T) {
	var mu sync.Mutex
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	h := newHarness(t, func(d *Deps) { d.Now = clock })
	h.mkItem("Sti/A", true)
	anon := h.client("")
	if code := h.do(anon, "GET", "/api/catalog", nil, nil); code != 200 || h.wh.skuCalls != 1 {
		t.Fatalf("first catalog = %d, calls %d", code, h.wh.skuCalls)
	}

	mu.Lock()
	now = now.Add(2 * time.Minute)
	mu.Unlock()
	wait := make(chan struct{})
	h.wh.mu.Lock()
	h.wh.skuWait = wait
	h.wh.mu.Unlock()
	done := make(chan map[string]any)
	go func() {
		var cat map[string]any
		h.do(anon, "GET", "/api/catalog", nil, &cat)
		done <- cat
	}()
	select {
	case cat := <-done:
		if st, _ := cat["stock"].(map[string]any); st["Sti/A"] == nil {
			t.Fatalf("stale catalog has no stock: %v", cat["stock"])
		}
	case <-time.After(2 * time.Second):
		close(wait)
		t.Fatal("catalog waited on the warehouse")
	}
	close(wait)
	// The refresh lands in the background, once.
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.wh.mu.Lock()
		calls := h.wh.skuCalls
		h.wh.mu.Unlock()
		if calls == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("warehouse calls = %d, want 2", calls)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
