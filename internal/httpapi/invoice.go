package httpapi

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/hackclub/merch/internal/invoice"
	"github.com/hackclub/merch/internal/swag"
)

// customsInvoice is a PDF customs invoice for a request going abroad, which
// the requester can show customs: the contents are a gift, at the values the
// warehouse declares with the parcel. Admins can get one for any request.
func (s *server) customsInvoice(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	req, err := s.Store.GetRequest(r.Context(), id)
	if err != nil || (req.UserID != v.user.ID && !v.roles.Admin) {
		writeErr(w, http.StatusNotFound, "request not found")
		return
	}
	if req.Address.Country == "US" || (req.Status != swag.StatusPending && req.Status != swag.StatusDispatched) {
		writeErr(w, http.StatusUnprocessableEntity, "customs invoices are for paid requests shipping outside the US")
		return
	}

	lines := make([]swag.Shipped, len(req.Lines))
	names := map[string]string{}
	shipsWith := map[int64][]string{}
	for i, l := range req.Lines {
		lines[i] = swag.Shipped{ItemID: l.ItemID, SKU: l.SKU, Quantity: l.Quantity}
		names[l.SKU] = l.Name
		if l.Variant != "" {
			names[l.SKU] = fmt.Sprintf("%s (%s)", l.Name, l.Variant)
		}
		if _, seen := shipsWith[l.ItemID]; seen {
			continue
		}
		it, err := s.Store.GetItem(r.Context(), l.ItemID)
		if err != nil {
			s.serverErr(w, err)
			return
		}
		shipsWith[l.ItemID] = it.ShipsWith
	}

	list, err := s.cachedSKUs(r.Context())
	if err != nil {
		s.Log.Error("warehouse sku lookup failed", "err", err)
		writeErr(w, http.StatusServiceUnavailable, "couldn't reach the warehouse for the customs values; try again in a minute")
		return
	}
	skus := map[string]swag.CustomsSKU{}
	for _, k := range list {
		cost, _ := strconv.ParseFloat(k.UnitCost, 64)
		origin := k.CountryOfOrigin
		if len(origin) == 2 {
			origin = invoice.CountryName(origin)
		}
		skus[k.SKU] = swag.CustomsSKU{Name: k.Name, Description: k.CustomsDescription, HSCode: k.HSCode, Origin: origin, UnitCost: cost}
	}
	inv, err := swag.CustomsInvoice(swag.Shipment(lines, shipsWith), names, skus)
	if err != nil {
		s.Log.Error("customs invoice", "request_id", id, "err", err)
		writeErr(w, http.StatusServiceUnavailable, "the warehouse has no customs value for something in this request; ask Hack Club for an invoice")
		return
	}

	date := s.Now()
	if req.MailedAt != nil {
		date = *req.MailedAt
	} else if req.DispatchedAt != nil {
		date = *req.DispatchedAt
	}
	shipment := ""
	if req.TrackingNumber != "" {
		shipment = req.Carrier + " " + req.TrackingNumber
	}
	pdf, err := invoice.Render(invoice.Doc{
		Number:   fmt.Sprintf("MERCH-%d", id),
		Date:     date,
		To:       invoice.Consignee(req.Address),
		Shipment: shipment,
		Invoice:  inv,
	})
	if err != nil {
		s.serverErr(w, err)
		return
	}
	s.track(r, v.user, "customs_invoice", map[string]any{"request_id": id, "country": req.Address.Country, "status": req.Status,
		"lines": len(inv.Lines), "value_cents": inv.TotalCents, "admin": req.UserID != v.user.ID})
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="hack-club-customs-invoice-%d.pdf"`, id))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Write(pdf)
}
