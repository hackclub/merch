package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // print art comes as PNG or JPEG
	_ "image/png"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hackclub/merch/internal/imgprep"
	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
)

// Staff getting postcards and posters printed (swag.PrintProducts): the specs
// and a live estimate, uploading print-ready art, and the request HQ reviews.

// printProductView is a print product with the sizes the request form shows
// and checks art against.
type printProductView struct {
	swag.PrintProduct
	BleedWidthIn  float64 `json:"bleed_width_in"`
	BleedHeightIn float64 `json:"bleed_height_in"`
	SafeWidthIn   float64 `json:"safe_width_in"`
	SafeHeightIn  float64 `json:"safe_height_in"`
	BleedWidthPx  int     `json:"bleed_width_px"`
	BleedHeightPx int     `json:"bleed_height_px"`
}

func (s *server) printProducts(w http.ResponseWriter, r *http.Request) {
	out := make([]printProductView, len(swag.PrintProducts))
	for i, p := range swag.PrintProducts {
		v := printProductView{PrintProduct: p}
		v.BleedWidthIn, v.BleedHeightIn = p.BleedSize()
		v.SafeWidthIn, v.SafeHeightIn = p.SafeSize()
		v.BleedWidthPx, v.BleedHeightPx = p.BleedPixels()
		out[i] = v
	}
	writeJSON(w, http.StatusOK, map[string]any{"products": out})
}

func (s *server) printEstimate(w http.ResponseWriter, r *http.Request) {
	p, ok := swag.PrintProductByKey(r.URL.Query().Get("product"))
	if !ok {
		writeErr(w, http.StatusUnprocessableEntity, "unknown product")
		return
	}
	qty, _ := strconv.Atoi(r.URL.Query().Get("quantity"))
	e, err := swag.EstimatePrint(p, qty, s.Now())
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// printFile is a piece of print art on the CDN, untouched, and how it fits.
// PreviewURL is the same art trimmed and shrunk to a web JPEG, for the shop
// item the run becomes.
type printFile struct {
	URL        string `json:"url"`
	PreviewURL string `json:"preview_url"`
	Bytes      int64  `json:"bytes"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	swag.ArtCheck
}

var printArtTypes = map[string]string{"image/png": ".png", "image/jpeg": ".jpg"}

// uploadPrintFile stores one side's print-ready art on the CDN as is, once it's
// a PNG or JPEG the shape of the product at a printable resolution.
func (s *server) uploadPrintFile(w http.ResponseWriter, r *http.Request, v viewer) {
	if s.Images == nil {
		writeErr(w, http.StatusServiceUnavailable, "uploads are off: set HACKCLUB_CDN_API_KEY")
		return
	}
	var body struct {
		Product  string `json:"product"`
		Filename string `json:"filename"`
		Data     []byte `json:"data"` // base64
	}
	if !readJSONLimit(w, r, &body, maxPrintFile*4/3+4096) {
		return
	}
	p, ok := swag.PrintProductByKey(body.Product)
	if !ok {
		writeErr(w, http.StatusUnprocessableEntity, "unknown product")
		return
	}
	ctype := http.DetectContentType(body.Data)
	ext, known := printArtTypes[ctype]
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body.Data))
	if !known || err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "upload a PNG or JPEG (export PDFs as a 300 DPI PNG)")
		return
	}
	check := swag.CheckArt(p, cfg.Width, cfg.Height)
	if check.Problem != "" {
		writeErr(w, http.StatusUnprocessableEntity, "This file won't print well: "+check.Problem+".")
		return
	}
	base := strings.TrimSuffix(imageName(body.Filename), ".jpg")
	url, err := s.Images.Upload(r.Context(), base+ext, ctype, body.Data)
	if err != nil {
		s.Log.Error("cdn upload failed", "product", p.Key, "err", err)
		writeErr(w, http.StatusBadGateway, "cdn.hackclub.com refused the file: "+err.Error())
		return
	}
	// Without a preview, the item falls back to the print file; that's no reason to refuse the art.
	preview := ""
	if jpg, err := imgprep.WebInset(body.Data, swag.TrimInset(p, cfg.Width, cfg.Height)); err != nil {
		s.Log.Warn("print preview failed", "product", p.Key, "err", err)
	} else if preview, err = s.Images.Upload(r.Context(), base+"-preview.jpg", "image/jpeg", jpg); err != nil {
		s.Log.Warn("print preview upload failed", "product", p.Key, "err", err)
	}
	s.track(r, v.user, "print_file_uploaded", map[string]any{"product": p.Key, "fit": check.Fit, "dpi": check.DPI, "bytes": len(body.Data), "type": ctype})
	writeJSON(w, http.StatusCreated, printFile{URL: url, PreviewURL: preview, Bytes: int64(len(body.Data)), Width: cfg.Width, Height: cfg.Height, ArtCheck: check})
}

func (s *server) createPrintRequest(w http.ResponseWriter, r *http.Request, v viewer) {
	var body struct {
		Product  string `json:"product"`
		Title    string `json:"title"`
		Quantity int    `json:"quantity"`
		FrontURL string `json:"front_url"`
		BackURL  string `json:"back_url"`
		Note     string `json:"note"`
		NeededBy string `json:"needed_by"` // YYYY-MM-DD, optional
		// How many to send them once it's stocked, and where (needed when there are any).
		InitialQuantity int           `json:"initial_quantity"`
		Address         *swag.Address `json:"address"`
		FrontPreviewURL string        `json:"front_preview_url"`
		BackPreviewURL  string        `json:"back_preview_url"`
		Landscape       bool          `json:"landscape"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	title, note := strings.TrimSpace(body.Title), strings.TrimSpace(body.Note)
	p, ok := swag.PrintProductByKey(body.Product)
	if !ok {
		writeErr(w, http.StatusUnprocessableEntity, "unknown product")
		return
	}
	e, err := swag.EstimatePrint(p, body.Quantity, s.Now())
	var due *time.Time
	if d, perr := time.Parse(time.DateOnly, body.NeededBy); perr == nil {
		due = &d
	}
	addrErr := error(nil)
	if body.InitialQuantity <= 0 {
		body.Address = nil // nothing to send, so nowhere to send it
	} else if body.Address == nil {
		addrErr = errors.New("where should we send the first ones?")
	} else {
		body.Address.Country = strings.ToUpper(strings.TrimSpace(body.Address.Country))
		addrErr = body.Address.Validate()
	}
	badPreview := false
	for _, u := range []string{body.FrontPreviewURL, body.BackPreviewURL} {
		badPreview = badPreview || (u != "" && (!isHTTPURL(u) || len(u) > 2000))
	}
	switch {
	case err != nil:
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	case title == "" || len(title) > 200:
		writeErr(w, http.StatusUnprocessableEntity, "give it a name (up to 200 characters)")
		return
	case note == "" || len(note) > 5000:
		writeErr(w, http.StatusUnprocessableEntity, "say how they'll be used (up to 5000 characters)")
		return
	case !isHTTPURL(body.FrontURL) || len(body.FrontURL) > 2000:
		writeErr(w, http.StatusUnprocessableEntity, "upload the front")
		return
	case body.BackURL != "" && (!isHTTPURL(body.BackURL) || len(body.BackURL) > 2000):
		writeErr(w, http.StatusUnprocessableEntity, "the back must be an uploaded file")
		return
	case body.NeededBy != "" && due == nil:
		writeErr(w, http.StatusUnprocessableEntity, "needed_by must be a date (YYYY-MM-DD)")
		return
	case badPreview:
		writeErr(w, http.StatusUnprocessableEntity, "previews must be uploaded files")
		return
	case body.InitialQuantity < 0 || body.InitialQuantity > body.Quantity:
		writeErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("send between 0 and all %d to you first", body.Quantity))
		return
	case p.Postcard && body.InitialQuantity > 0 && !swag.PackQuantity("postcard", body.InitialQuantity):
		writeErr(w, http.StatusUnprocessableEntity, "the first postcards come in "+swag.PackSizes("postcard"))
		return
	case addrErr != nil:
		writeErr(w, http.StatusUnprocessableEntity, addrErr.Error())
		return
	}
	pr, err := s.Store.CreatePrintRequest(r.Context(), store.PrintRequest{
		UserID: v.user.ID, Product: p.Key, Title: title, Quantity: body.Quantity, FrontURL: strings.TrimSpace(body.FrontURL),
		BackURL: strings.TrimSpace(body.BackURL), Note: note, NeededBy: due, EstimateCents: e.TotalCents,
		InitialQuantity: body.InitialQuantity, Address: body.Address, FrontPreviewURL: body.FrontPreviewURL,
		BackPreviewURL: body.BackPreviewURL, Landscape: body.Landscape,
	})
	if err != nil {
		s.serverErr(w, err)
		return
	}
	props := map[string]any{"print_request_id": pr.ID, "product": p.Key, "quantity": pr.Quantity, "estimate_cents": pr.EstimateCents,
		"has_back": pr.BackURL != "", "has_needed_by": due != nil, "initial_quantity": pr.InitialQuantity}
	if pr.Address != nil {
		props["country"] = pr.Address.Country
	}
	if due != nil {
		props["days_until_needed"] = swag.BusinessDays(s.Now(), *due)
		props["max_days"] = e.MaxDays
	}
	s.track(r, v.user, "print_request_created", props)
	writeJSON(w, http.StatusCreated, pr)
}

func (s *server) myPrintRequests(w http.ResponseWriter, r *http.Request, v viewer) {
	prs, err := s.Store.ListPrintRequestsByUser(r.Context(), v.user.ID)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"print_requests": prs})
}

func (s *server) adminPrintRequests(w http.ResponseWriter, r *http.Request, v viewer) {
	prs, err := s.Store.ListPrintRequests(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"print_requests": prs})
}

var printRequestStatuses = map[string]bool{"submitted": true, "ordered": true, "stocked": true, "rejected": true}

func (s *server) adminUpdatePrintRequest(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Status    string `json:"status"`
		AdminNote string `json:"admin_note"`
		SKU       string `json:"sku"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	body.SKU = strings.TrimSpace(body.SKU)
	if !printRequestStatuses[body.Status] {
		writeErr(w, http.StatusUnprocessableEntity, "unknown status")
		return
	}
	if body.Status == "stocked" && body.SKU == "" {
		writeErr(w, http.StatusUnprocessableEntity, "add the warehouse SKU it's stocked under: it's listed in the shop under it")
		return
	}
	prev, err := s.Store.GetPrintRequest(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "print request not found")
		return
	} else if err != nil {
		s.serverErr(w, err)
		return
	}
	pr, err := s.Store.UpdatePrintRequest(r.Context(), id, body.Status, strings.TrimSpace(body.AdminNote), body.SKU)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	// In the warehouse: list it, and send them their first ones (once; saving again retries a failure).
	if pr.Status == "stocked" && pr.InitialQuantity > 0 && pr.Address != nil && pr.ShipmentRequestID == nil {
		if pr, err = s.sendFirstShipment(r, pr); err != nil {
			s.serverErr(w, err)
			return
		}
	}
	s.trackAdmin(r, v, "print_request_updated", map[string]any{"print_request_id": pr.ID, "from": prev.Status, "to": pr.Status,
		"product": pr.Product, "quantity": pr.Quantity, "has_note": pr.AdminNote != "", "has_sku": pr.SKU != "",
		"wait_h": int(s.Now().Sub(pr.CreatedAt).Hours()), "shipment_request_id": pr.ShipmentRequestID})
	writeJSON(w, http.StatusOK, pr)
}

// printItem is the shop item a stocked print run is listed as: the one under
// its SKU, or a new one made from the run's art, open to everyone.
func (s *server) printItem(ctx context.Context, pr *store.PrintRequest) (swag.Item, error) {
	items, err := s.Store.ItemsBySKU(ctx, []string{pr.SKU})
	if err != nil || len(items) > 0 {
		return firstItem(items), err
	}
	p, _ := swag.PrintProductByKey(pr.Product)
	w, h, t := p.WidthIn, p.HeightIn, p.ThicknessIn
	if pr.Landscape {
		w, h = h, w
	}
	images := []string{}
	for _, u := range []string{pr.FrontPreviewURL, pr.BackPreviewURL} {
		if u != "" {
			images = append(images, u)
		}
	}
	if len(images) == 0 {
		images = []string{pr.FrontURL}
	}
	it := swag.Item{SKU: pr.SKU, Name: pr.Title, Images: images, Visible: true, MaxPerRequest: 25,
		WidthIn: &w, HeightIn: &h, Finish: p.Finish, ThicknessIn: &t}
	if p.Postcard {
		it.Packs, it.MaxPerRequest = "postcard", 100
	}
	return s.Store.CreateItem(ctx, it)
}

func firstItem(items []swag.Item) swag.Item {
	if len(items) == 0 {
		return swag.Item{}
	}
	return items[0]
}

// sendFirstShipment makes the swag request for a stocked run's first
// shipment to the person who asked for it. It's reviewed and shipped like
// any request; abroad, it waits for them to pay the postage.
func (s *server) sendFirstShipment(r *http.Request, pr *store.PrintRequest) (*store.PrintRequest, error) {
	ctx := r.Context()
	it, err := s.printItem(ctx, pr)
	if err != nil {
		return nil, err
	}
	st, err := s.Store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	lines := []swag.Line{{ItemID: it.ID, Quantity: pr.InitialQuantity}}
	_, est, err := s.quote(ctx, st, pr.Address.Country, lines, map[int64]swag.Item{it.ID: it})
	if err != nil && !isRuleErr(err) {
		return nil, err
	}
	status, fee := swag.InitialStatus(st, pr.Address.Country, est)
	note := fmt.Sprintf("The first %d of your print run, %s (print request #%d), now that it's in our warehouse.", pr.InitialQuantity, pr.Title, pr.ID)
	// Their own run: the shop's per-person limits and stock holds don't apply.
	req, err := s.Store.CreateRequest(ctx, store.NewRequest{UserID: pr.UserID, Address: *pr.Address, Purpose: swag.PurposeStaff,
		Lines: lines, Status: status, ShippingFeeCents: fee, Note: note}, func(map[int64]int) error { return nil })
	if err != nil {
		return nil, err
	}
	if req.Status == swag.StatusPending {
		s.emailRequest(r, req, "received")
	}
	return s.Store.SetPrintShipment(ctx, pr.ID, req.ID)
}
