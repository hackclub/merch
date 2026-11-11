// Package printpo reads only the Zenventory PO copy. It never places a printer
// order, edits warehouse data, or attaches a suggested match to a print run.
package printpo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hackclub/merch/internal/swag"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Warehouse struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Warehouse { return &Warehouse{pool: pool} }

// Connect requires a separately provisioned, narrowly scoped read-only login.
// No fallback to merch's DB or an agent's warehouse login is permitted.
func Connect(ctx context.Context, url string) (*Warehouse, error) {
	if url == "" {
		return nil, errors.New("PRINT_PO_DATABASE_URL is required")
	}
	c, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("invalid PRINT_PO_DATABASE_URL")
	}
	c.MaxConns = 2
	c.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	c.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	p, err := pgxpool.NewWithConfig(ctx, c)
	if err != nil {
		return nil, errors.New("cannot initialize print PO connection")
	}
	return New(p), nil
}

func (w *Warehouse) Close() { w.pool.Close() }

const poColumns = `id, coalesce(order_number,''), coalesce(supplier_name,''), draft, deleted, completed,
	created_date, required_by_date, completed_date, _synced_at, items`

func scanPO(row pgx.Row) (swag.PrintPOEvidence, json.RawMessage, error) {
	var p swag.PrintPOEvidence
	var raw json.RawMessage
	// Null flags deliberately fail the scan: unknown isn't false.
	err := row.Scan(&p.POID, &p.OrderNumber, &p.Supplier, &p.Draft, &p.Deleted, &p.Completed,
		&p.CreatedAt, &p.RequiredBy, &p.CompletedAt, &p.SyncedAt, &raw)
	p.PolledAt = time.Now().UTC()
	return p, raw, err
}

type line struct {
	ID       int64  `json:"id"`
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

func decodeLine(raw json.RawMessage, link swag.PrintPOLink) (int, error) {
	var lines []line
	if err := json.Unmarshal(raw, &lines); err != nil {
		return 0, errors.New("invalid PO lines")
	}
	qty, count := 0, 0
	for _, l := range lines {
		if l.ID == link.LineID && l.SKU == link.SKU {
			qty = l.Quantity
			count++
		}
	}
	if count != 1 || qty <= 0 {
		return 0, errors.New("exact PO line and SKU not uniquely present with positive quantity")
	}
	return qty, nil
}

func (w *Warehouse) Exact(ctx context.Context, link swag.PrintPOLink) (swag.PrintPOEvidence, error) {
	if link.POID <= 0 || link.LineID <= 0 || link.SKU == "" {
		return swag.PrintPOEvidence{}, errors.New("PO, line and exact SKU required")
	}
	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return swag.PrintPOEvidence{}, err
	}
	defer tx.Rollback(ctx)
	p, raw, err := scanPO(tx.QueryRow(ctx, `SELECT `+poColumns+` FROM agh_fulfillment_zenventory.purchase_orders WHERE id=$1`, link.POID))
	if err != nil {
		return p, err
	}
	qty, err := decodeLine(raw, link)
	if err != nil {
		return p, err
	}
	p.PrintPOLink = link
	p.Quantity = qty
	return p, tx.Commit(ctx)
}

// Suggest returns at most 50 exact-SKU lines, including drafts and deleted POs
// so HQ can see why a candidate is invalid. Suggestions are never links.
func (w *Warehouse) Suggest(ctx context.Context, sku string) ([]swag.PrintPOEvidence, error) {
	if sku == "" {
		return nil, errors.New("exact SKU required")
	}
	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT `+poColumns+` FROM agh_fulfillment_zenventory.purchase_orders
		WHERE items @> jsonb_build_array(jsonb_build_object('sku',$1::text)) ORDER BY created_date DESC NULLS LAST, id DESC LIMIT 50`, sku)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []swag.PrintPOEvidence{}
	for rows.Next() {
		p, raw, err := scanPO(rows)
		if err != nil {
			return nil, err
		}
		var lines []line
		if err = json.Unmarshal(raw, &lines); err != nil {
			return nil, fmt.Errorf("invalid PO lines")
		}
		for _, l := range lines {
			if l.SKU == sku {
				v := p
				v.LineID = l.ID
				v.SKU = l.SKU
				v.Quantity = l.Quantity
				out = append(out, v)
				if len(out) == 50 {
					break
				}
			}
		}
		if len(out) == 50 {
			break
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, tx.Commit(ctx)
}
