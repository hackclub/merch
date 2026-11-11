package httpapi

import (
	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
	"time"
)

// Public PO evidence has a deliberately smaller schema. Do not redact with zero
// values: identities must be absent from requester JSON, including action replies.
type publicPrintPO struct {
	Status      string     `json:"status"`
	CreatedAt   *time.Time `json:"created_at"`
	RequiredBy  *time.Time `json:"required_by"`
	CompletedAt *time.Time `json:"completed_at"`
	SyncedAt    time.Time  `json:"synced_at"`
	PolledAt    time.Time  `json:"polled_at"`
}
type publicPrintRun struct {
	*store.PrintRun
	Links         []publicPrintPO `json:"links"`
	POLineOrdered *int            `json:"po_line_ordered"`
}

func printRunView(p *store.PrintRun, admin bool, now time.Time) any {
	if admin {
		return p
	}
	copy := *p
	copy.InternalNote = ""
	copy.UserEmail = ""
	copy.ApprovedBy = 0 // an admin's user id
	out := publicPrintRun{PrintRun: &copy, Links: []publicPrintPO{}}
	for _, proof := range p.Links {
		status := "Received quantity unavailable"
		switch {
		case p.SourceStale || p.PollError != "" || proof.SyncedAt.IsZero() || proof.SyncedAt.After(now) || now.Sub(proof.SyncedAt) > time.Hour:
			status = "Not yet confirmed in latest copy"
		case proof.Draft || proof.Deleted:
			status = "Needs HQ review: draft or deleted PO"
		case proof.Completed:
			status = "PO complete (whole order)"
		}
		out.Links = append(out.Links, publicPrintPO{status, proof.CreatedAt, proof.RequiredBy, proof.CompletedAt, proof.SyncedAt, proof.PolledAt})
		if out.POLineOrdered == nil {
			out.POLineOrdered = new(int)
		}
		*out.POLineOrdered += proof.Quantity
	}
	return out
}

func requesterPrintHistory(history []store.PrintRunEvent) []store.PrintRunEvent {
	out := []store.PrintRunEvent{}
	var previous swag.PrintStage
	// Input is newest first. Compare in chronological insertion order so a
	// same-stage observation never masquerades as a receipt or approval.
	for i := len(history) - 1; i >= 0; i-- {
		e := history[i]
		changed := e.Stage != previous
		previous = e.Stage
		etaChanged, _ := e.Details["eta_changed"].(bool)
		update := e.Note != "" || e.Action == "eta_update" || etaChanged
		if !changed && !update {
			continue
		}
		details := map[string]any{}
		for _, key := range []string{"specifications", "approved_quantity", "art_revision", "update_source", "eta", "eta_changed"} {
			if value, ok := e.Details[key]; ok {
				details[key] = value
			}
		}
		e.Details = details
		e.ActorID = nil
		out = append(out, e)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
