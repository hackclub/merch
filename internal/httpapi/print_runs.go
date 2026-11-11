package httpapi

import (
	"context"
	"errors"
	"fmt"
	"github.com/hackclub/merch/internal/loops"
	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SlackDM sends a print-run update to an opted-in requester (internal/slackdm).
type SlackDM interface {
	Send(ctx context.Context, channel, text, idempotencyKey string) error
}

type PrintPOReader interface {
	Exact(context.Context, swag.PrintPOLink) (swag.PrintPOEvidence, error)
	Suggest(context.Context, string) ([]swag.PrintPOEvidence, error)
}

func (s *server) overlayLegacyPrints(ctx context.Context, prints []*store.PrintRequest) error {
	ids := []int64{}
	byID := map[int64]*store.PrintRequest{}
	for _, p := range prints {
		ids = append(ids, p.ID)
		byID[p.ID] = p
	}
	runs, err := s.Store.PrintRunsForSources(ctx, ids, nil)
	if err != nil {
		return err
	}
	for _, r := range runs {
		if r.SourcePrintID == nil {
			continue
		}
		p := byID[*r.SourcePrintID]
		if p == nil {
			continue
		}
		id := r.ID
		p.PrintRunID = &id
		p.PrintStage = r.Stage
		p.SKU = r.SKU
		p.AdminNote = r.PublicNote
		p.ShipmentRequestID = r.ShipmentRequestID
		p.Status = swag.LegacyPrintRequestStatus(r.Stage)
	}
	return nil
}
func (s *server) overlayLegacyAuthors(ctx context.Context, subs []*store.Submission) error {
	ids := []int64{}
	byID := map[int64]*store.Submission{}
	for _, p := range subs {
		ids = append(ids, p.ID)
		byID[p.ID] = p
	}
	runs, err := s.Store.PrintRunsForSources(ctx, nil, ids)
	if err != nil {
		return err
	}
	for _, r := range runs {
		if r.SourceAuthorID == nil {
			continue
		}
		p := byID[*r.SourceAuthorID]
		if p == nil {
			continue
		}
		id := r.ID
		p.PrintRunID = &id
		p.PrintStage = r.Stage
		p.SKU = r.SKU
		p.AdminNote = r.PublicNote
		p.Status = swag.LegacySubmissionStatus(r.Stage)
	}
	return nil
}

func (s *server) printRuns(w http.ResponseWriter, r *http.Request, v viewer) {
	uid := v.user.ID
	if strings.HasPrefix(r.URL.Path, "/api/admin/") || strings.HasPrefix(r.URL.Path, "/api/agent/") {
		uid = 0
	}
	runs, err := s.Store.PrintRuns(r.Context(), uid)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	for _, p := range runs {
		if uid != 0 {
			p.InternalNote = ""
			p.UserEmail = ""
		}
	}
	s.attachPrintStock(r.Context(), runs)
	views := make([]any, 0, len(runs))
	for _, p := range runs {
		views = append(views, printRunView(p, uid == 0, s.Now()))
	}
	writeJSON(w, 200, map[string]any{"runs": views, "po_linking_available": s.PrintPOs != nil})
}

func (s *server) attachPrintStock(ctx context.Context, runs []*store.PrintRun) {
	needed := false
	for _, r := range runs {
		if r.SKU != "" {
			needed = true
			break
		}
	}
	visible := map[string]bool{}
	if needed {
		items, err := s.Store.ListItems(ctx, false)
		if err == nil {
			for _, it := range items {
				visible[it.SKU] = it.Visible
			}
		}
	}
	for _, r := range runs {
		r.Stock = &store.PrintStock{Visible: visible[r.SKU], MaxAgeSeconds: skuCacheTTL.Seconds()}
		if !r.Stock.Visible {
			zero := 0
			r.Stock.Available = &zero
		}
	}
	if !needed {
		return
	}
	skus, err := s.cachedSKUs(ctx)
	if err != nil {
		return
	}
	held, err := s.Store.HeldQuantities(ctx)
	if err != nil {
		return
	}
	s.skuMu.Lock()
	at := s.skusAt
	s.skuMu.Unlock()
	index := map[string]int{}
	for i := range skus {
		index[skus[i].SKU] = i
	}
	for _, r := range runs {
		if i, ok := index[r.SKU]; ok {
			sku := skus[i]
			q, h := sku.InStock, held[r.SKU]
			available := swag.PrintRequestableStock(r.Stock.Visible, q, h)
			r.Stock.WarehouseSellable = &q
			r.Stock.Held = &h
			r.Stock.Available = &available
			r.Stock.Inbound = sku.Inbound
			r.Stock.SyncedAt = &at
		}
	}
}
func (s *server) printRunDetail(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.Store.PrintRun(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "print run not found")
		return
	}
	admin := s.Config.IsAdminEmail(v.user.Email)
	if !admin && p.UserID != v.user.ID {
		writeErr(w, 404, "print run not found")
		return
	}
	history, err := s.Store.PrintRunHistory(r.Context(), id)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	notices, err := s.Store.PrintNotifications(r.Context(), id)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	if !admin {
		p.InternalNote = ""
		p.UserEmail = ""
		for i := range notices {
			notices[i].Error = ""
		}
		history = requesterPrintHistory(history)
	}
	stock := map[string]any{"max_age_seconds": skuCacheTTL.Seconds(), "synced_at": nil, "warehouse_sellable": nil, "held": nil, "available": nil, "inbound": nil, "visible": false}
	stock["available"] = 0 // A hidden listing is never requestable, even with an unknown stock feed.
	if p.SKU != "" {
		skus, err := s.cachedSKUs(r.Context())
		held, herr := s.Store.HeldQuantities(r.Context())
		items, ierr := s.Store.ItemsBySKU(r.Context(), []string{p.SKU})
		visible := false
		if ierr == nil && len(items) > 0 {
			visible = items[0].Visible
		}
		stock["visible"] = visible
		if visible {
			stock["available"] = nil
		}
		if err == nil && herr == nil {
			s.skuMu.Lock()
			at := s.skusAt
			s.skuMu.Unlock()
			stock["synced_at"] = at
			for _, sku := range skus {
				if sku.SKU == p.SKU {
					stock["warehouse_sellable"] = sku.InStock
					stock["held"] = held[p.SKU]
					stock["inbound"] = sku.Inbound
					stock["available"] = swag.PrintRequestableStock(visible, sku.InStock, held[p.SKU])
				}
			}
		}
	}
	var shipment *store.Request
	if p.ShipmentRequestID != nil {
		shipment, _ = s.Store.GetRequest(r.Context(), *p.ShipmentRequestID)
		if shipment != nil && !admin && shipment.UserID != p.UserID {
			shipment = nil
		}
		if shipment != nil {
			shipment.InternalNote = ""
		}
	}
	var confirmations []store.PrintConfirmation
	if admin {
		confirmations, err = s.Store.PrintConfirmations(r.Context(), id)
		if err != nil {
			s.serverErr(w, err)
			return
		}
	}
	writeJSON(w, 200, map[string]any{"run": printRunView(p, admin, s.Now()), "history": history, "notifications": notices, "stock": stock, "shipment": shipment, "confirmations": confirmations, "po_automation": s.Config.PrintPOAutomatic, "po_linking_available": s.PrintPOs != nil})
}
func (s *server) printPOSuggestions(w http.ResponseWriter, r *http.Request, v viewer) {
	if s.PrintPOs == nil {
		writeErr(w, 503, "PO source is not configured")
		return
	}
	sku := strings.TrimSpace(r.URL.Query().Get("sku"))
	if sku == "" {
		writeErr(w, 422, "exact SKU required")
		return
	}
	p, err := s.PrintPOs.Suggest(r.Context(), sku)
	if err != nil {
		writeErr(w, 502, "PO source unavailable")
		return
	}
	writeJSON(w, 200, map[string]any{"suggestions": p, "polled_at": s.Now()})
}

func (s *server) printRunAction(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		store.PrintAction
		Links []swag.PrintPOLink `json:"links"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	p, err := s.Store.PrintRun(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "print run not found")
		return
	}
	admin := s.Config.IsAdminEmail(v.user.Email)
	if !admin && p.UserID != v.user.ID {
		writeErr(w, 404, "print run not found")
		return
	}
	a := body.PrintAction
	a.ActorID = v.user.ID
	a.Requester = !admin
	if err := swag.AuthorizePrintAction(swag.PrintRunState{UserID: p.UserID, Stage: p.Stage, Links: p.Links}, a, swag.PrintActor{ID: v.user.ID, Admin: admin}); err != nil {
		writePrintActionError(w, err)
		return
	}
	if a.Address != nil {
		a.Address.Country = strings.ToUpper(strings.TrimSpace(a.Address.Country))
	}
	if !admin {
		a.InternalNote = ""
		a.ConfirmationURL = ""
		a.CancellationDisposition = ""
	}
	if a.Action == "preferences" {
		if a.SlackOptIn && (s.PrintSlack == nil || v.user.SlackID == "") {
			writeErr(w, 422, "Slack DMs are not configured for this account")
			return
		}
		a.Note = ""
		a.InternalNote = ""
	}
	a.Now = s.Now()
	if a.DispatchedAt != nil && a.DispatchedAt.After(a.Now) {
		writeErr(w, 422, "printer dispatch time cannot be in the future")
		return
	}
	if a.Action == "refresh_po" {
		if !s.Config.PrintPOAutomatic || s.PrintPOs == nil {
			writeErr(w, 503, "PO automatic transitions are not configured")
			return
		}
		if p.Version != a.Version {
			writeErr(w, 409, store.ErrPrintConflict.Error())
			return
		}
		s.pollPrintRun(r.Context(), p)
		updated, err := s.Store.PrintRun(r.Context(), p.ID)
		if err != nil {
			s.serverErr(w, err)
			return
		}
		s.track(r, v.user, "print_run_action", map[string]any{"print_run_id": p.ID, "action": "refresh_po", "from": p.Stage, "to": updated.Stage})
		writeJSON(w, 200, printRunView(updated, admin, s.Now()))
		return
	}
	if len(a.Note) > 5000 || len(a.InternalNote) > 10000 || len(a.Specifications) > 5000 || len(a.Tracking) > 500 || len(a.ETA) > 500 {
		writeErr(w, 422, "update is too long")
		return
	}
	if a.ETA != "" {
		if _, err := time.Parse(time.DateOnly, a.ETA); err != nil {
			writeErr(w, 422, "expected arrival must be a date (YYYY-MM-DD)")
			return
		}
	}
	for _, u := range []string{a.ConfirmationURL} {
		if u != "" && (!isHTTPURL(u) || len(u) > 2000) {
			writeErr(w, 422, "confirmation must be an http(s) link")
			return
		}
	}
	if a.Action == "reorder" {
		next, err := s.Store.ReorderPrintRun(r.Context(), p.ID, a.Version, s.printMessages())
		if errors.Is(err, store.ErrPrintConflict) {
			writeErr(w, 409, err.Error())
			return
		}
		if errors.Is(err, store.ErrPrintAction) {
			writePrintActionError(w, err)
			return
		}
		if err != nil {
			s.serverErr(w, err)
			return
		}
		s.track(r, v.user, "print_run_action", map[string]any{"print_run_id": id, "action": "reorder", "new_run_id": next.ID})
		writeJSON(w, 201, printRunView(next, admin, s.Now()))
		return
	}
	if a.Action == "link" || a.Action == "replace_links" {
		if s.PrintPOs == nil {
			writeErr(w, 503, "PO source is not configured")
			return
		}
		if len(body.Links) < 1 || len(body.Links) > 20 {
			writeErr(w, 422, "select 1–20 exact PO lines")
			return
		}
		for _, link := range body.Links {
			proof, err := s.PrintPOs.Exact(r.Context(), link)
			if err != nil {
				writeErr(w, 422, "exact PO/line/SKU not found in latest copy")
				return
			}
			a.Evidence = append(a.Evidence, proof)
		}
		if a.SKU == "" {
			a.SKU = body.Links[0].SKU
		}
	}
	if a.Action == "release" && !(p.Stage == swag.PrintAvailable && p.ShipmentRequestID != nil) {
		items, err := s.Store.ItemsBySKU(r.Context(), []string{p.SKU})
		if err != nil || len(items) != 1 {
			writeErr(w, 422, "prepare a complete listing first")
			return
		}
		skus, err := s.cachedSKUs(r.Context())
		if err != nil {
			writeErr(w, 503, "stock is not available")
			return
		}
		s.skuMu.Lock()
		stockAt := s.skusAt
		s.skuMu.Unlock()
		if s.Now().Sub(stockAt) > skuCacheTTL {
			writeErr(w, 503, "stock is stale; retry after refresh")
			return
		}
		for _, sku := range skus {
			if sku.SKU == p.SKU {
				a.Sellable = sku.InStock
			}
		}
		if p.Request.InitialQuantity > 0 && p.Request.Address != nil {
			st, err := s.Store.GetSettings(r.Context())
			if err != nil {
				s.serverErr(w, err)
				return
			}
			lines := []swag.Line{{ItemID: items[0].ID, Quantity: p.Request.InitialQuantity}}
			_, est, err := s.quote(r.Context(), st, p.Request.Address.Country, lines, map[int64]swag.Item{items[0].ID: items[0]})
			if err != nil {
				writeErr(w, 422, err.Error())
				return
			}
			a.ReleaseStatus, a.ReleaseFee = swag.InitialStatus(st, p.Request.Address.Country, est)
		}
	}
	updated, err := s.Store.ActPrintRun(r.Context(), id, a, s.printMessages())
	if errors.Is(err, store.ErrPrintConflict) {
		writeErr(w, 409, err.Error())
		return
	}
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, 409, "This PO line is already linked. Review the existing print-run links.")
		return
	}
	if errors.Is(err, store.ErrPrintAction) {
		writePrintActionError(w, err)
		return
	}
	if err != nil {
		s.serverErr(w, err)
		return
	}
	s.track(r, v.user, "print_run_action", map[string]any{"print_run_id": id, "action": a.Action, "from": p.Stage, "to": updated.Stage, "version": updated.Version, "has_note": a.Note != ""})
	// Linked PO creation may confirm ordering, but only with explicit automation configuration.
	if (a.Action == "link" || a.Action == "replace_links") && s.Config.PrintPOAutomatic {
		s.pollPrintRun(r.Context(), updated)
		if latest, err := s.Store.PrintRun(r.Context(), updated.ID); err == nil {
			updated = latest
		}
	}
	writeJSON(w, 200, printRunView(updated, admin, s.Now()))
}

func (s *server) pollPrintRun(ctx context.Context, p *store.PrintRun) {
	if s.PrintPOs == nil || !s.Config.PrintPOAutomatic || len(p.Links) == 0 {
		return
	}
	a := store.PrintAction{Action: "observe", Auto: true, Version: p.Version, Now: s.Now()}
	for _, link := range p.Links {
		proof, err := s.PrintPOs.Exact(ctx, link.PrintPOLink)
		if err != nil {
			a.PollError = "Not yet confirmed in latest copy"
			break
		}
		a.Evidence = append(a.Evidence, proof)
	}
	updated, err := s.Store.ActPrintRun(ctx, p.ID, a, s.printMessages())
	if err != nil {
		return
	}
	changed := updated.Version != p.Version || updated.PollError != p.PollError || updated.SourceStale != p.SourceStale
	if changed || a.PollError != "" {
		s.track(&http.Request{URL: &url.URL{Path: "/internal/print-poll"}, Header: http.Header{}}, &store.User{ID: p.UserID}, "print_po_polled", map[string]any{"print_run_id": p.ID, "from": p.Stage, "to": updated.Stage, "reconcile": updated.Reconcile, "failed": a.PollError != "", "changed": changed})
	}
}
func (s *server) printWorkers(ctx context.Context) {
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			if s.Config.PrintPOAutomatic && s.PrintPOs != nil {
				pollCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
				runs, err := s.Store.PrintRunsToPoll(pollCtx, s.Now())
				if err == nil {
					for _, p := range runs {
						s.pollPrintRun(pollCtx, p)
					}
				}
				cancel()
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		s.processPrintJobs(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (s *server) processPrintJobs(ctx context.Context) {
	if s.PrintSlack == nil {
		if err := s.Store.SkipUnconfiguredPrintSlack(ctx); err != nil {
			s.Log.Warn("skip unconfigured print DMs", "error", err)
			return
		}
	}
	if s.Mailer != nil {
		if err := s.Store.PreparePrintShipmentEmails(ctx, func(request *store.Request) (string, string, string, string) {
			e := receivedEmail(request, s.Config.BaseURL)
			return e.Subject, e.Preview, e.Body, fmt.Sprintf("%s-request-%d-received", s.Config.TheseusOrderTag, request.ID)
		}); err != nil {
			s.Log.Warn("prepare first parcel email", "error", err)
			return
		}
		for _, channel := range []string{"email", "shipment_email"} {
			err := s.Store.DeliverPrintNotifications(ctx, channel, func(n store.PrintNotification) error {
				preview := n.Preview
				if preview == "" {
					preview = n.Subject
				}
				err := s.Mailer.Send(ctx, loops.Email{To: n.Destination, Subject: n.Subject, Preview: preview, Body: n.Body, IdempotencyKey: n.IdempotencyKey})
				s.track(&http.Request{URL: &url.URL{Path: "/internal/print-notifications"}, Header: http.Header{}}, nil, "print_notification_attempted", map[string]any{"print_run_id": n.RunID, "event_id": n.EventID, "channel": "email", "failed": err != nil})
				return err
			})
			if err != nil {
				s.Log.Warn("print email worker failed", "error", err)
			}
		}
	}
	if s.PrintSlack != nil {
		err := s.Store.DeliverPrintNotifications(ctx, "slack", func(n store.PrintNotification) error {
			err := s.PrintSlack.Send(ctx, n.Destination, n.Body, n.IdempotencyKey)
			s.track(&http.Request{URL: &url.URL{Path: "/internal/print-notifications"}, Header: http.Header{}}, nil, "print_notification_attempted", map[string]any{"print_run_id": n.RunID, "event_id": n.EventID, "channel": "slack", "failed": err != nil})
			return err
		})
		if err != nil {
			s.Log.Warn("print Slack worker failed", "error", err)
		}
	}
}

func writePrintActionError(w http.ResponseWriter, err error) {
	status := http.StatusUnprocessableEntity
	var actionErr *swag.PrintActionError
	if errors.As(err, &actionErr) && actionErr.Forbidden {
		status = http.StatusForbidden
	}
	writeErr(w, status, err.Error())
}
