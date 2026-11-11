package swag

import "time"

// PrintStage describes production of a print run, independently of catalog
// visibility and outbound shipment status. Guards on evidence and actor belong
// to the action that invokes a transition, in addition to this graph.
type PrintStage string

const (
	PrintSubmitted PrintStage = "submitted"
	PrintApproved  PrintStage = "approved"
	PrintOrdered   PrintStage = "ordered"
	PrintInTransit PrintStage = "in_transit"
	PrintReceived  PrintStage = "received"
	PrintAvailable PrintStage = "available"
	PrintRejected  PrintStage = "rejected"
	PrintCancelled PrintStage = "cancelled"
)

func CanTransitionPrint(from, to PrintStage) bool {
	switch from {
	case PrintSubmitted:
		return to == PrintApproved || to == PrintRejected || to == PrintCancelled
	case PrintApproved:
		// A late first observation may already confirm receipt. Do not invent
		// intervening timestamps or replay historical notification bursts.
		return to == PrintOrdered || to == PrintReceived || to == PrintRejected || to == PrintCancelled
	case PrintOrdered:
		return to == PrintInTransit || to == PrintReceived || to == PrintCancelled
	case PrintInTransit:
		return to == PrintReceived || to == PrintCancelled
	case PrintReceived:
		return to == PrintAvailable
	}
	return false
}

// PrintPOLink is explicitly selected by HQ; SKU matching only suggests links.
type PrintPOLink struct {
	POID   int64  `json:"po_id"`
	LineID int64  `json:"line_id"`
	SKU    string `json:"sku"`
}

// PrintPOEvidence deliberately has no per-line received quantity. Zenventory
// completion describes the entire PO. Stock comes from a separate feed.
type PrintPOEvidence struct {
	PrintPOLink
	OrderNumber string     `json:"order_number"`
	Supplier    string     `json:"supplier"`
	Quantity    int        `json:"quantity"`
	Draft       bool       `json:"draft"`
	Deleted     bool       `json:"deleted"`
	Completed   bool       `json:"completed"`
	CreatedAt   *time.Time `json:"created_at"`
	RequiredBy  *time.Time `json:"required_by"`
	CompletedAt *time.Time `json:"completed_at"`
	SyncedAt    time.Time  `json:"synced_at"`
	PolledAt    time.Time  `json:"polled_at"`
}

func (p PrintPOEvidence) Confirms(link PrintPOLink, now time.Time, maxAge time.Duration) bool {
	return p.PrintPOLink == link && link.POID > 0 && link.LineID > 0 && link.SKU != "" &&
		!p.Draft && !p.Deleted && p.Quantity > 0 && p.CreatedAt != nil && !p.CreatedAt.After(now) &&
		!p.SyncedAt.IsZero() && !p.SyncedAt.After(now) && maxAge > 0 && now.Sub(p.SyncedAt) <= maxAge
}

type PrintObservationResult struct {
	Stage     PrintStage
	Reconcile bool
}

type PrintApproval struct {
	ReviewedExistingOrder bool
	Quantity              int
	SKU                   string
	At                    time.Time
}

// ObservePrintPOs is a decision on a complete set of required links, not a
// mutation. The caller must persist it under a row lock and atomically save its
// history and notification job. Empty Stage means do not change the run.
func ObservePrintPOs(stage PrintStage, approval PrintApproval, links []PrintPOLink, observations []PrintPOEvidence, now time.Time, maxAge time.Duration) PrintObservationResult {
	if len(links) == 0 || approval.Quantity <= 0 || approval.SKU == "" || approval.At.IsZero() || approval.At.After(now) {
		return PrintObservationResult{}
	}
	seen := map[PrintPOLink]bool{}
	total, allComplete := 0, true
	var snapshot time.Time
	for _, link := range links {
		if link.SKU != approval.SKU {
			return PrintObservationResult{Reconcile: true}
		}
		if seen[link] {
			return PrintObservationResult{Reconcile: true}
		}
		seen[link] = true
		var found *PrintPOEvidence
		for i := range observations {
			if observations[i].PrintPOLink == link {
				if found != nil {
					return PrintObservationResult{Reconcile: true}
				}
				found = &observations[i]
			}
		}
		if found == nil || !found.Confirms(link, now, maxAge) {
			return PrintObservationResult{Reconcile: true}
		}
		if !snapshot.IsZero() && !snapshot.Equal(found.SyncedAt) {
			return PrintObservationResult{Reconcile: true}
		}
		snapshot = found.SyncedAt
		if found.CreatedAt.Before(approval.At) && !approval.ReviewedExistingOrder {
			return PrintObservationResult{Reconcile: true}
		}
		// Prevent overflow and require explicit reapproval for quantity changes.
		if found.Quantity > approval.Quantity-total {
			return PrintObservationResult{Reconcile: true}
		}
		total += found.Quantity
		allComplete = allComplete && found.Completed
	}
	if total != approval.Quantity {
		return PrintObservationResult{Reconcile: true}
	}
	if stage == PrintReceived || stage == PrintAvailable {
		return PrintObservationResult{Reconcile: !allComplete}
	}
	if stage != PrintApproved && stage != PrintOrdered && stage != PrintInTransit {
		return PrintObservationResult{}
	}
	if allComplete {
		return PrintObservationResult{Stage: PrintReceived}
	}
	if stage == PrintApproved {
		return PrintObservationResult{Stage: PrintOrdered}
	}
	return PrintObservationResult{}
}

// Warehouse sellable already excludes allocations at the warehouse. Only
// merch's pending and awaiting-payment requests are held again here.
func PrintRequestableStock(visible bool, sellable, held int) int {
	if !visible {
		return 0
	}
	return max(0, sellable-max(0, held))
}
