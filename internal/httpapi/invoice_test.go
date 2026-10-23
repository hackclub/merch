package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hackclub/merch/internal/auth"
	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
)

// get fetches path raw, for responses that aren't JSON.
func (h *harness) get(c *http.Client, path string) (*http.Response, []byte) {
	h.t.Helper()
	resp, err := c.Get(h.srv.URL + path)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

// mkRequestFor makes userEmail's request straight in the store (abroad, it
// skips the card payment).
func (h *harness) mkRequestFor(addr swag.Address, status swag.Status, lines ...swag.Line) *store.Request {
	h.t.Helper()
	u, err := h.store.UpsertUser(h.t.Context(), auth.Identity{ID: "ident!" + userEmail, Email: userEmail, Name: userEmail})
	if err != nil {
		h.t.Fatal(err)
	}
	r, err := h.store.CreateRequest(h.t.Context(), store.NewRequest{UserID: u.ID, Address: addr, Purpose: swag.PurposePersonal,
		Status: status, Lines: lines}, func(map[int64]int) error { return nil })
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

func TestCustomsInvoice(t *testing.T) {
	h := newHarness(t)
	sticker := h.mkItem("Sti/A", true)
	hoodie, err := h.store.CreateItem(t.Context(), swag.Item{SKU: "Swa/Hood/Small", Name: "Hoodie", Visible: true, MaxPerRequest: 1, ShipsWith: []string{"Sti/A"}})
	if err != nil {
		t.Fatal(err)
	}
	r := h.mkRequestFor(deAddr, swag.StatusPending, swag.Line{ItemID: sticker.ID, Quantity: 2}, swag.Line{ItemID: hoodie.ID, Quantity: 1})
	path := fmt.Sprintf("/api/requests/%d/customs-invoice", r.ID)

	resp, b := h.get(h.client(userEmail), path)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(b, []byte("%PDF-")) {
		t.Fatalf("invoice = %d %s %q", resp.StatusCode, resp.Header.Get("Content-Type"), b[:min(len(b), 40)])
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, fmt.Sprintf(`filename="hack-club-customs-invoice-%d.pdf"`, r.ID)) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	evs := h.events("customs_invoice")
	if len(evs) != 1 {
		t.Fatalf("%d customs_invoice events", len(evs))
	}
	// Two stickers and the one the hoodie ships with at $0.25, and a $30 hoodie.
	p := props(t, evs[0])
	if p["request_id"] != float64(r.ID) || p["country"] != "DE" || p["lines"] != float64(2) || p["value_cents"] != float64(3075) || p["status"] != "pending" {
		t.Errorf("props = %v", p)
	}

	// Someone else's request is not found; admins can get one for support.
	if resp, _ := h.get(h.client(authorEmail), path); resp.StatusCode != 404 {
		t.Errorf("other user = %d", resp.StatusCode)
	}
	if resp, _ := h.get(h.client(adminEmail), path); resp.StatusCode != 200 {
		t.Errorf("admin = %d", resp.StatusCode)
	}
	if resp, _ := h.get(h.client(""), path); resp.StatusCode != 401 {
		t.Errorf("signed out = %d", resp.StatusCode)
	}

	// Only for what's going abroad: not in the US, not before it's paid for,
	// and not once it's cancelled.
	for _, other := range []*store.Request{
		h.mkRequestFor(usAddr, swag.StatusPending, swag.Line{ItemID: sticker.ID, Quantity: 1}),
		h.mkRequestFor(deAddr, swag.StatusAwaitingPayment, swag.Line{ItemID: sticker.ID, Quantity: 1}),
		h.mkRequestFor(deAddr, swag.StatusCancelled, swag.Line{ItemID: sticker.ID, Quantity: 1}),
	} {
		if resp, _ := h.get(h.client(userEmail), fmt.Sprintf("/api/requests/%d/customs-invoice", other.ID)); resp.StatusCode != 422 {
			t.Errorf("%s %s = %d", other.Address.Country, other.Status, resp.StatusCode)
		}
	}
}

// The values come from the warehouse, so without it there's no invoice.
func TestCustomsInvoiceNeedsWarehouse(t *testing.T) {
	h := newHarness(t)
	h.wh.skuFail = errors.New("down")
	it := h.mkItem("Sti/A", true)
	r := h.mkRequestFor(deAddr, swag.StatusPending, swag.Line{ItemID: it.ID, Quantity: 1})
	if resp, _ := h.get(h.client(userEmail), fmt.Sprintf("/api/requests/%d/customs-invoice", r.ID)); resp.StatusCode != 503 {
		t.Fatalf("invoice = %d", resp.StatusCode)
	}
}
