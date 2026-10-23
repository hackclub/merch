package httpapi

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
)

// pngOf is a blank w x h PNG, the shape print art comes in.
func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestPrintProductsAndEstimate(t *testing.T) {
	h := newHarness(t)
	anon := h.client("")
	var got struct{ Products []printProductView }
	if code := h.do(anon, "GET", "/api/print/products", nil, &got); code != 200 || len(got.Products) != len(swag.PrintProducts) {
		t.Fatalf("products = %d %+v", code, got)
	}
	pc := got.Products[0]
	if pc.Key != "postcard_4x6" || pc.BleedWidthIn != 4.25 || pc.BleedHeightIn != 6.25 || pc.SafeWidthIn != 3.75 || pc.BleedWidthPx != 1275 || pc.BleedHeightPx != 1875 {
		t.Fatalf("postcard = %+v", pc)
	}
	var e swag.PrintEstimate
	if code := h.do(anon, "GET", "/api/print/estimate?product=postcard_4x6&quantity=1000", nil, &e); code != 200 || e.PrintCents != 8259 || e.ReadyBy == "" {
		t.Fatalf("estimate = %d %+v", code, e)
	}
	for _, q := range []string{"product=banner&quantity=100", "product=postcard_4x6&quantity=7", "product=postcard_4x6&quantity=x"} {
		if code := h.do(anon, "GET", "/api/print/estimate?"+q, nil, nil); code != 422 {
			t.Errorf("estimate %s = %d, want 422", q, code)
		}
	}
}

func TestPrintFileUpload(t *testing.T) {
	h := newHarness(t)
	staff := h.client("staff@hackclub.com")
	upload := func(c *http.Client, product string, data []byte, out any) int {
		return h.do(c, "POST", "/api/print/files", map[string]any{"product": product, "filename": "Front Side.png", "data": base64.StdEncoding.EncodeToString(data)}, out)
	}
	art := pngOf(t, 1875, 1275)
	var f printFile
	if code := upload(staff, "postcard_4x6", art, &f); code != 201 {
		t.Fatalf("upload = %d", code)
	}
	if f.URL != "https://cdn.example/1/front-side.png" || f.Width != 1875 || f.Height != 1275 || f.Fit != swag.ArtBleed || f.DPI != 300 || f.Bytes != int64(len(art)) {
		t.Fatalf("file = %+v", f)
	}
	if up := h.cdn.uploads[0]; up.contentType != "image/png" || !bytes.Equal(up.data, art) {
		t.Fatalf("uploaded %s, %d bytes", up.contentType, len(up.data))
	}
	if e := h.events("print_file_uploaded"); len(e) != 1 || props(t, e[0])["fit"] != "bleed" || props(t, e[0])["product"] != "postcard_4x6" {
		t.Fatalf("events = %+v", e)
	}
	// A square won't print as a postcard; a PDF can't be previewed; nobody signed out uploads.
	if code := upload(staff, "postcard_4x6", pngOf(t, 500, 500), nil); code != 422 {
		t.Fatalf("square upload = %d", code)
	}
	if code := upload(staff, "postcard_4x6", []byte("%PDF-1.7\n"+strings.Repeat("x", 100)), nil); code != 422 {
		t.Fatalf("pdf upload = %d", code)
	}
	if code := upload(staff, "banner", art, nil); code != 422 {
		t.Fatalf("unknown product upload = %d", code)
	}
	if code := upload(h.client(""), "postcard_4x6", art, nil); code != 401 {
		t.Fatalf("signed-out upload = %d", code)
	}
	if len(h.cdn.uploads) != 1 {
		t.Fatalf("%d uploads, want 1", len(h.cdn.uploads))
	}
}

func TestPrintRequestFlow(t *testing.T) {
	h := newHarness(t)
	staff, admin := h.client("staff@hackclub.com"), h.client(adminEmail)
	body := map[string]any{
		"product": "postcard_4x6", "title": " Haven postcard ", "quantity": 1000,
		"front_url": "https://cdn.example/1/front.png", "back_url": "https://cdn.example/2/back.png",
		"note": "Handing out at Haven events", "needed_by": "2026-11-10",
	}
	var pr store.PrintRequest
	if code := h.do(staff, "POST", "/api/print-requests", body, &pr); code != 201 {
		t.Fatalf("create = %d", code)
	}
	if pr.Title != "Haven postcard" || pr.Status != "submitted" || pr.EstimateCents != 8259+3699 || pr.NeededBy == nil || pr.NeededBy.Format("2006-01-02") != "2026-11-10" {
		t.Fatalf("created = %+v", pr)
	}
	e := h.events("print_request_created")
	if len(e) != 1 {
		t.Fatalf("events = %+v", e)
	}
	p := props(t, e[0])
	if p["product"] != "postcard_4x6" || p["quantity"] != float64(1000) || p["has_back"] != true || p["estimate_cents"] != float64(11958) || p["note"] != nil || p["title"] != nil {
		t.Fatalf("props = %+v", p)
	}

	for k, v := range map[string]any{"product": "banner", "title": "", "quantity": 7, "front_url": "javascript:alert(1)", "back_url": "ftp://x", "note": "", "needed_by": "soon"} {
		bad := map[string]any{}
		for kk, vv := range body {
			bad[kk] = vv
		}
		bad[k] = v
		if code := h.do(staff, "POST", "/api/print-requests", bad, nil); code != 422 {
			t.Errorf("bad %s = %d, want 422", k, code)
		}
	}
	if code := h.do(h.client(""), "POST", "/api/print-requests", body, nil); code != 401 {
		t.Fatalf("signed-out create = %d", code)
	}

	var mine struct {
		PrintRequests []store.PrintRequest `json:"print_requests"`
	}
	h.do(staff, "GET", "/api/print-requests", nil, &mine)
	var others struct {
		PrintRequests []store.PrintRequest `json:"print_requests"`
	}
	h.do(h.client(userEmail), "GET", "/api/print-requests", nil, &others)
	if len(mine.PrintRequests) != 1 || len(others.PrintRequests) != 0 {
		t.Fatalf("mine = %d, others = %d", len(mine.PrintRequests), len(others.PrintRequests))
	}

	path := fmt.Sprintf("/api/admin/print-requests/%d", pr.ID)
	if code := h.do(staff, "PUT", path, map[string]any{"status": "ordered"}, nil); code != 403 {
		t.Fatalf("non-admin update = %d", code)
	}
	var all struct {
		PrintRequests []store.PrintRequest `json:"print_requests"`
	}
	if code := h.do(admin, "GET", "/api/admin/print-requests", nil, &all); code != 200 || len(all.PrintRequests) != 1 || all.PrintRequests[0].UserEmail != "staff@hackclub.com" {
		t.Fatalf("admin list = %d %+v", code, all)
	}
	var upd store.PrintRequest
	if code := h.do(admin, "PUT", path, map[string]any{"status": "stocked", "admin_note": "In the shop", "sku": " Pri/Hav/4x6/1st "}, &upd); code != 200 || upd.Status != "stocked" || upd.SKU != "Pri/Hav/4x6/1st" {
		t.Fatalf("update = %d %+v", code, upd)
	}
	if code := h.do(admin, "PUT", path, map[string]any{"status": "lost"}, nil); code != 422 {
		t.Fatalf("bad status = %d", code)
	}
	if e := h.events("print_request_updated"); len(e) != 1 || props(t, e[0])["from"] != "submitted" || props(t, e[0])["to"] != "stocked" {
		t.Fatalf("update events = %+v", e)
	}
}
