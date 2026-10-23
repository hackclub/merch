package httpapi

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg" // print art comes as PNG or JPEG
	_ "image/png"
	"net/http"
	"strconv"
	"strings"
	"time"

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
type printFile struct {
	URL    string `json:"url"`
	Bytes  int64  `json:"bytes"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
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
	name := strings.TrimSuffix(imageName(body.Filename), ".jpg") + ext
	url, err := s.Images.Upload(r.Context(), name, ctype, body.Data)
	if err != nil {
		s.Log.Error("cdn upload failed", "product", p.Key, "err", err)
		writeErr(w, http.StatusBadGateway, "cdn.hackclub.com refused the file: "+err.Error())
		return
	}
	s.track(r, v.user, "print_file_uploaded", map[string]any{"product": p.Key, "fit": check.Fit, "dpi": check.DPI, "bytes": len(body.Data), "type": ctype})
	writeJSON(w, http.StatusCreated, printFile{URL: url, Bytes: int64(len(body.Data)), Width: cfg.Width, Height: cfg.Height, ArtCheck: check})
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
	}
	pr, err := s.Store.CreatePrintRequest(r.Context(), store.PrintRequest{
		UserID: v.user.ID, Product: p.Key, Title: title, Quantity: body.Quantity, FrontURL: strings.TrimSpace(body.FrontURL),
		BackURL: strings.TrimSpace(body.BackURL), Note: note, NeededBy: due, EstimateCents: e.TotalCents,
	})
	if err != nil {
		s.serverErr(w, err)
		return
	}
	props := map[string]any{"print_request_id": pr.ID, "product": p.Key, "quantity": pr.Quantity, "estimate_cents": pr.EstimateCents,
		"has_back": pr.BackURL != "", "has_needed_by": due != nil}
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
	if !printRequestStatuses[body.Status] {
		writeErr(w, http.StatusUnprocessableEntity, "unknown status")
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
	pr, err := s.Store.UpdatePrintRequest(r.Context(), id, body.Status, strings.TrimSpace(body.AdminNote), strings.TrimSpace(body.SKU))
	if err != nil {
		s.serverErr(w, err)
		return
	}
	s.trackAdmin(r, v, "print_request_updated", map[string]any{"print_request_id": pr.ID, "from": prev.Status, "to": pr.Status,
		"product": pr.Product, "quantity": pr.Quantity, "has_note": pr.AdminNote != "", "has_sku": pr.SKU != "",
		"wait_h": int(s.Now().Sub(pr.CreatedAt).Hours())})
	writeJSON(w, http.StatusOK, pr)
}
