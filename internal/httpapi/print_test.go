package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math/rand"
	"net/http"
	"runtime"
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
	if e.ToYouDays != [2]int{} || e.ArriveBy != "" {
		t.Fatalf("no first shipment, but a leg to them: %+v", e)
	}
	// With a first shipment, the last leg is how packages like it went to that country before.
	shippingFixture(t, h)
	if code := h.do(anon, "GET", "/api/print/estimate?product=postcard_4x6&quantity=1000&first=25&country=de", nil, &e); code != 200 || e.ToYouDays[1] == 0 || e.ArriveBy <= e.ReadyBy {
		t.Fatalf("estimate with delivery = %d %+v", code, e)
	}
	for _, q := range []string{"product=banner&quantity=100", "product=postcard_4x6&quantity=7", "product=postcard_4x6&quantity=x"} {
		if code := h.do(anon, "GET", "/api/print/estimate?"+q, nil, nil); code != 422 {
			t.Errorf("estimate %s = %d, want 422", q, code)
		}
	}
}

// raw posts body as contentType: the print form sends art as the file's own bytes, not base64 JSON.
func (h *harness) raw(c *http.Client, path, contentType string, body []byte, out any) int {
	h.t.Helper()
	req, _ := http.NewRequest("POST", h.srv.URL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	resp, err := c.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func jpegOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestPrintFileUpload(t *testing.T) {
	h := newHarness(t)
	staff := h.client("staff@hackclub.com")
	const path = "/api/print/files?product=postcard_4x6&filename=Front%20Side.png"
	art := pngOf(t, 1875, 1275)
	var f printFile
	if code := h.raw(staff, path, "image/png", art, &f); code != 201 {
		t.Fatalf("upload = %d", code)
	}
	if f.URL != "https://cdn.example/1/front-side.png" || f.Width != 1875 || f.Height != 1275 || f.Fit != swag.ArtBleed || f.DPI != 300 || f.Bytes != int64(len(art)) {
		t.Fatalf("file = %+v", f)
	}
	// Stored untouched, and never decoded whole on the server: the browser sends the preview.
	if len(h.cdn.uploads) != 1 || h.cdn.uploads[0].contentType != "image/png" || !bytes.Equal(h.cdn.uploads[0].data, art) {
		t.Fatalf("uploads = %d", len(h.cdn.uploads))
	}
	// The preview is the trimmed card the browser drew for the 3D view, stored as sent.
	preview := jpegOf(t, 1800, 1200)
	var pf printFile
	if code := h.raw(staff, path+"&kind=preview", "image/jpeg", preview, &pf); code != 201 || pf.URL != "https://cdn.example/2/front-side-preview.jpg" {
		t.Fatalf("preview = %d %+v", code, pf)
	}
	if up := h.cdn.uploads[1]; up.contentType != "image/jpeg" || !bytes.Equal(up.data, preview) {
		t.Fatalf("preview uploaded as %s", up.contentType)
	}
	e := h.events("print_file_uploaded")
	if len(e) != 2 || props(t, e[0])["fit"] != "bleed" || props(t, e[0])["kind"] != "print" || props(t, e[1])["kind"] != "preview" || props(t, e[0])["product"] != "postcard_4x6" {
		t.Fatalf("events = %+v", e)
	}
	// A square won't print as a postcard; under 300 DPI won't print sharp; only PNGs print; previews are
	// web-sized JPEGs; the old base64 JSON isn't art; nobody signed out uploads.
	if code := h.raw(staff, path, "image/png", pngOf(t, 500, 500), nil); code != 422 {
		t.Fatalf("square upload = %d", code)
	}
	var low struct{ Error string }
	if code := h.raw(staff, path, "image/png", pngOf(t, 1274, 1874), &low); code != 422 || !strings.Contains(low.Error, "at least 300") {
		t.Fatalf("299 DPI upload = %d %q", code, low.Error)
	}
	var notPNG struct{ Error string }
	if code := h.raw(staff, path, "image/jpeg", jpegOf(t, 1875, 1275), &notPNG); code != 422 || !strings.Contains(notPNG.Error, "PNG") {
		t.Fatalf("jpeg upload = %d %q", code, notPNG.Error)
	}
	if code := h.raw(staff, path, "image/png", []byte("%PDF-1.7\n"+strings.Repeat("x", 100)), nil); code != 422 {
		t.Fatalf("pdf upload = %d", code)
	}
	if code := h.raw(staff, path+"&kind=preview", "image/jpeg", jpegOf(t, 3000, 2000), nil); code != 422 {
		t.Fatalf("oversized preview = %d", code)
	}
	if code := h.raw(staff, path+"&kind=preview", "image/png", art, nil); code != 422 {
		t.Fatalf("png preview = %d", code)
	}
	if code := h.do(staff, "POST", path, map[string]any{"data": base64.StdEncoding.EncodeToString(art)}, nil); code != 422 {
		t.Fatalf("json upload = %d", code)
	}
	if code := h.raw(staff, "/api/print/files?product=banner&filename=x.png", "image/png", art, nil); code != 422 {
		t.Fatalf("unknown product upload = %d", code)
	}
	if code := h.raw(h.client(""), path, "image/png", art, nil); code != 401 {
		t.Fatalf("signed-out upload = %d", code)
	}
	if len(h.cdn.uploads) != 2 {
		t.Fatalf("%d uploads, want 2", len(h.cdn.uploads))
	}
	// Too big, by its declared length, is refused before any of it is read or stored.
	if code := h.raw(staff, path+"&kind=preview", "image/jpeg", make([]byte, maxPrintPreview+1), nil); code != http.StatusRequestEntityTooLarge || len(h.cdn.uploads) != 2 {
		t.Fatalf("oversized upload = %d, %d uploads", code, len(h.cdn.uploads))
	}
	// Images skip the JSON-only guard on this route alone.
	if code := h.raw(staff, "/api/requests", "image/png", art, nil); code != http.StatusUnsupportedMediaType {
		t.Fatalf("png to /api/requests = %d", code)
	}
}

func TestPrintRequestFlow(t *testing.T) {
	h := newHarness(t)
	staff, admin := h.client("staff@hackclub.com"), h.client(adminEmail)
	body := map[string]any{
		"product": "postcard_4x6", "title": " Haven postcard ", "program": " Haven ", "quantity": 1000, "finish": "",
		"front_url": "https://cdn.example/1/front.png", "back_url": "https://cdn.example/2/back.png",
		"note": "Handing out at Haven events", "deadline": "Haven in Boston, Nov 14-16",
		"front_preview_url": "https://cdn.example/3/front-preview.jpg", "back_preview_url": "https://cdn.example/4/back-preview.jpg",
		"landscape": true, "initial_quantity": 25, "address": usAddr,
	}
	var pr store.PrintRequest
	if code := h.do(staff, "POST", "/api/print-requests", body, &pr); code != 201 {
		t.Fatalf("create = %d", code)
	}
	if pr.InitialQuantity != 25 || pr.Address == nil || pr.Address.City != "Burlington" || !pr.Landscape || pr.FrontPreviewURL == "" {
		t.Fatalf("first shipment = %+v", pr)
	}
	if pr.Title != "Haven postcard" || pr.Finish != "linen" || pr.Program != "Haven" || pr.Deadline != "Haven in Boston, Nov 14-16" || pr.Status != "submitted" || pr.EstimateCents != 8259+3699 {
		t.Fatalf("created = %+v", pr)
	}
	e := h.events("print_request_created")
	if len(e) != 1 {
		t.Fatalf("events = %+v", e)
	}
	p := props(t, e[0])
	if p["product"] != "postcard_4x6" || p["quantity"] != float64(1000) || p["has_back"] != true || p["estimate_cents"] != float64(11958) || p["note"] != nil || p["title"] != nil ||
		p["initial_quantity"] != float64(25) || p["country"] != "US" || p["address"] != nil || p["program"] != nil || p["deadline"] != nil || p["same_back"] != false || p["finish"] != "linen" {
		t.Fatalf("props = %+v", p)
	}

	for k, v := range map[string]any{"finish": "gloss", "product": "banner", "title": "", "program": " ", "deadline": "", "quantity": 7, "front_url": "javascript:alert(1)", "back_url": "ftp://x", "note": "",
		"initial_quantity": 30, "address": map[string]any{"city": "Burlington"}, "front_preview_url": "ftp://x"} {
		bad := map[string]any{}
		for kk, vv := range body {
			bad[kk] = vv
		}
		bad[k] = v
		if code := h.do(staff, "POST", "/api/print-requests", bad, nil); code != 422 {
			t.Errorf("bad %s = %d, want 422", k, code)
		}
	}
	// More than the whole run, none at all, or a first shipment with nowhere to go, is refused.
	for _, change := range []map[string]any{{"initial_quantity": 1050}, {"address": nil}, {"initial_quantity": -5}, {"initial_quantity": 0}} {
		bad := map[string]any{}
		for kk, vv := range body {
			bad[kk] = vv
		}
		for kk, vv := range change {
			bad[kk] = vv
		}
		if code := h.do(staff, "POST", "/api/print-requests", bad, nil); code != 422 {
			t.Errorf("%v = %d, want 422", change, code)
		}
	}
	// The same art on both sides is the default: the back is the front.
	if code := h.do(staff, "POST", "/api/print-requests", map[string]any{"product": "poster_11x17", "title": "Sprig poster", "program": "Sprig", "deadline": "None",
		"quantity": 100, "front_url": "https://cdn.example/f.png", "back_url": "https://cdn.example/f.png", "note": "Libraries", "initial_quantity": 5, "address": usAddr}, nil); code != 201 {
		t.Fatalf("same back = %d", code)
	}
	if e := h.events("print_request_created"); len(e) != 2 || props(t, e[1])["same_back"] != true {
		t.Fatalf("same back events = %+v", e)
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
	if len(mine.PrintRequests) != 2 || len(others.PrintRequests) != 0 {
		t.Fatalf("mine = %d, others = %d", len(mine.PrintRequests), len(others.PrintRequests))
	}

	path := fmt.Sprintf("/api/admin/print-requests/%d", pr.ID)
	if code := h.do(staff, "PUT", path, map[string]any{"status": "ordered"}, nil); code != 403 {
		t.Fatalf("non-admin update = %d", code)
	}
	var all struct {
		PrintRequests []store.PrintRequest `json:"print_requests"`
	}
	if code := h.do(admin, "GET", "/api/admin/print-requests", nil, &all); code != 200 || len(all.PrintRequests) != 2 || all.PrintRequests[0].UserEmail != "staff@hackclub.com" {
		t.Fatalf("admin list = %d %+v", code, all)
	}
	// Stocked needs the SKU it's under, to list it and send the first shipment.
	if code := h.do(admin, "PUT", path, map[string]any{"status": "stocked"}, nil); code != 422 {
		t.Fatalf("stocked without SKU = %d", code)
	}
	var upd store.PrintRequest
	if code := h.do(admin, "PUT", path, map[string]any{"status": "stocked", "admin_note": "In the shop", "sku": " Pri/Hav/4x6/1st "}, &upd); code != 200 || upd.Status != "stocked" || upd.SKU != "Pri/Hav/4x6/1st" {
		t.Fatalf("update = %d %+v", code, upd)
	}
	// It's listed in the shop for everyone, made from the art (landscape like the art).
	items, _ := h.store.ItemsBySKU(context.Background(), []string{"Pri/Hav/4x6/1st"})
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	it := items[0]
	if !it.Visible || it.StaffOnly || it.Name != "Haven postcard" || it.Packs != "postcard" || it.Finish != "linen" || *it.WidthIn != 6 || *it.HeightIn != 4 ||
		len(it.Images) != 2 || it.Images[0] != "https://cdn.example/3/front-preview.jpg" {
		t.Fatalf("item = %+v", it)
	}
	// The first shipment is a request on their account, waiting for review like any other.
	if upd.ShipmentRequestID == nil {
		t.Fatalf("no first shipment request: %+v", upd)
	}
	req, err := h.store.GetRequest(context.Background(), *upd.ShipmentRequestID)
	if err != nil || req.Status != swag.StatusPending || req.Purpose != "staff" || req.Address.City != "Burlington" ||
		len(req.Lines) != 1 || req.Lines[0].ItemID != it.ID || req.Lines[0].Quantity != 25 || !strings.Contains(req.Note, "first 25") {
		t.Fatalf("first shipment = %+v, %v", req, err)
	}
	if e := h.events("print_request_updated"); len(e) != 1 || props(t, e[0])["shipment_request_id"] != float64(req.ID) {
		t.Fatalf("stocked events = %+v", e)
	}
	// Saving it again doesn't send a second shipment.
	if code := h.do(admin, "PUT", path, map[string]any{"status": "stocked", "sku": "Pri/Hav/4x6/1st"}, &upd); code != 200 || *upd.ShipmentRequestID != req.ID {
		t.Fatalf("resave = %d %+v", code, upd)
	}
	if code := h.do(admin, "PUT", path, map[string]any{"status": "lost"}, nil); code != 422 {
		t.Fatalf("bad status = %d", code)
	}
	if e := h.events("print_request_updated"); len(e) != 2 || props(t, e[0])["from"] != "submitted" || props(t, e[0])["to"] != "stocked" {
		t.Fatalf("update events = %+v", e)
	}
}

// A run stocked under a SKU that's already listed ships from that item; abroad,
// the first shipment waits for its postage like any international request.
func TestPrintFirstShipmentUsesListedItem(t *testing.T) {
	h := newHarness(t)
	staff, admin := h.client("staff@hackclub.com"), h.client(adminEmail)
	listed := h.mkItem("Pri/Spr/11x/Lar", true)
	var pr store.PrintRequest
	if code := h.do(staff, "POST", "/api/print-requests", map[string]any{"product": "poster_11x17", "title": "Sprig poster", "program": "Sprig", "deadline": "None",
		"quantity": 100, "front_url": "https://cdn.example/f.png", "note": "Libraries", "initial_quantity": 5, "address": deAddr}, &pr); code != 201 {
		t.Fatalf("create = %d", code)
	}
	var upd store.PrintRequest
	if code := h.do(admin, "PUT", fmt.Sprintf("/api/admin/print-requests/%d", pr.ID), map[string]any{"status": "stocked", "sku": "Pri/Spr/11x/Lar"}, &upd); code != 200 || upd.ShipmentRequestID == nil {
		t.Fatalf("stocked = %d %+v", code, upd)
	}
	req, _ := h.store.GetRequest(context.Background(), *upd.ShipmentRequestID)
	if req.Lines[0].ItemID != listed.ID || req.Status != swag.StatusAwaitingPayment || req.ShippingFeeCents <= 0 {
		t.Fatalf("first shipment = %+v", req)
	}
	if items, _ := h.store.ItemsBySKU(context.Background(), []string{"Pri/Spr/11x/Lar"}); len(items) != 1 {
		t.Fatalf("items = %d, want the one listed", len(items))
	}
}

// A glossy poster run is listed glossy, so the shop's 3D view shows it shiny.
func TestPrintGlossyPoster(t *testing.T) {
	h := newHarness(t)
	staff, admin := h.client("staff@hackclub.com"), h.client(adminEmail)
	var pr store.PrintRequest
	if code := h.do(staff, "POST", "/api/print-requests", map[string]any{"product": "poster_11x17", "finish": "gloss", "title": "Sprig poster", "program": "Sprig",
		"deadline": "None", "quantity": 100, "front_url": "https://cdn.example/f.png", "note": "Libraries", "initial_quantity": 5, "address": usAddr}, &pr); code != 201 || pr.Finish != "gloss" {
		t.Fatalf("create = %d %+v", code, pr)
	}
	if e := h.events("print_request_created"); len(e) != 1 || props(t, e[0])["finish"] != "gloss" {
		t.Fatalf("events = %+v", e)
	}
	if code := h.do(admin, "PUT", fmt.Sprintf("/api/admin/print-requests/%d", pr.ID), map[string]any{"status": "stocked", "sku": "Pri/Spr/11x/Gls"}, nil); code != 200 {
		t.Fatalf("stocked = %d", code)
	}
	if items, _ := h.store.ItemsBySKU(context.Background(), []string{"Pri/Spr/11x/Gls"}); len(items) != 1 || items[0].Finish != "gloss" {
		t.Fatalf("items = %+v", items)
	}
}

// Print art streams through to the CDN: a 24 MB poster costs the server a few
// MB of buffers, not the file (or its pixels) in memory.
func TestPrintFileUploadStreams(t *testing.T) {
	h := newHarness(t)
	h.cdn.discard = true
	img := image.NewNRGBA(image.Rect(0, 0, 3375, 5175))
	rand.New(rand.NewSource(1)).Read(img.Pix[:len(img.Pix)/3]) // noise, so it doesn't compress away
	var b bytes.Buffer
	(&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&b, img)
	art := b.Bytes()
	img = nil
	staff := h.client("staff@hackclub.com")
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var f printFile
	code := h.raw(staff, "/api/print/files?product=poster_11x17&filename=poster.png", "image/png", art, &f)
	runtime.ReadMemStats(&after)
	if code != 201 || f.Bytes != int64(len(art)) || f.Fit != swag.ArtBleed {
		t.Fatalf("upload = %d %+v", code, f)
	}
	got := after.TotalAlloc - before.TotalAlloc
	t.Logf("a %.1f MB upload allocated %.2f MB (client and server)", float64(len(art))/1e6, float64(got)/1e6)
	if got > uint64(len(art))/4 {
		t.Fatalf("a %.1f MB upload allocated %.1f MB: it should stream", float64(len(art))/1e6, float64(got)/1e6)
	}
}
