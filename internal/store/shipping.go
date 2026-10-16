package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hackclub/merch/internal/importer"
	"github.com/hackclub/merch/internal/swag"
)

// ReplaceShippingHistory loads past shipments, replacing every row from the
// sources they come from (a reload of the warehouse export replaces the last one).
func (s *Store) ReplaceShippingHistory(ctx context.Context, ships []importer.Shipment) error {
	sources := map[string]bool{}
	rows := make([][]any, len(ships))
	for i, sh := range ships {
		sources[sh.Source] = true
		rows[i] = []any{sh.Source, sh.Country, sh.Class, sh.WeightOz, sh.PostageCents, sh.OrderedOn, sh.ShippedOn, sh.DeliveredOn}
	}
	var names []string
	for src := range sources {
		names = append(names, src)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM shipping_history WHERE source = ANY($1)`, names); err != nil {
		return err
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"shipping_history"},
		[]string{"source", "country", "class", "weight_oz", "postage_cents", "ordered_on", "shipped_on", "delivered_on"},
		pgx.CopyFromRows(rows)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ShippingHistory returns every past shipment, for estimates, with handling
// and transit in business days.
func (s *Store) ShippingHistory(ctx context.Context) ([]swag.PastShipment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT country, class, coalesce(weight_oz, 0), coalesce(postage_cents, 0),
			ordered_on, shipped_on, delivered_on
		FROM shipping_history`)
	if err != nil {
		return nil, err
	}
	between := func(from, to *time.Time) *float64 {
		if from == nil || to == nil {
			return nil
		}
		d := float64(swag.BusinessDays(*from, *to))
		return &d
	}
	return collect(rows, func(r pgx.Rows) (swag.PastShipment, error) {
		var p swag.PastShipment
		var ordered, delivered *time.Time
		var shipped time.Time
		err := r.Scan(&p.Country, &p.Class, &p.WeightOz, &p.PostageCents, &ordered, &shipped, &delivered)
		p.ShippedOn = shipped
		p.HandlingDays, p.TransitDays = between(ordered, &shipped), between(&shipped, delivered)
		return p, err
	})
}
