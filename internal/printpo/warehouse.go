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
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err = verifyScope(checkCtx, p); err != nil {
		p.Close()
		return nil, err
	}
	return New(p), nil
}

// Check effective privileges, including inherited and PUBLIC grants. A scoped
// URL's name is not proof of isolation. Only PO columns needed by this adapter
// and system/extension reads are allowed. Extension ownership is established by
// pg_depend, never a name prefix. Extra write grants and access to other users'
// SQL remain forbidden; a failed check never falls back to an agent credential.
const scopeCheckSQL = `WITH relations AS (
 SELECT c.oid,c.relname,n.nspname,
 EXISTS(SELECT 1 FROM pg_depend d WHERE d.classid='pg_class'::regclass AND d.objid=c.oid AND d.objsubid=0 AND d.refclassid='pg_extension'::regclass AND d.deptype='e') AS extension_member,
 n.nspname IN ('pg_catalog','information_schema') AND c.oid<16384
 AND NOT EXISTS(SELECT 1 FROM pg_attribute att CROSS JOIN unnest(ARRAY['SELECT','INSERT','UPDATE','REFERENCES']) AS privilege
 WHERE att.attrelid=c.oid AND att.attnum>0 AND NOT att.attisdropped AND has_column_privilege(current_user,c.oid,att.attnum,privilege)
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(c.relacl,acldefault('r',c.relowner))) a WHERE a.grantee=0 AND a.privilege_type=privilege)
 AND NOT EXISTS(SELECT 1 FROM aclexplode(att.attacl) a WHERE a.grantee=0 AND a.privilege_type=privilege))
 AND NOT EXISTS(SELECT 1 FROM unnest(ARRAY['DELETE','TRUNCATE','TRIGGER']) AS privilege WHERE has_table_privilege(current_user,c.oid,privilege)
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(c.relacl,acldefault('r',c.relowner))) a WHERE a.grantee=0 AND a.privilege_type=privilege)) AS public_builtin
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN ('r','v','m','p','f')
 ) SELECT
 NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname=current_user AND (rolsuper OR rolcreatedb OR rolcreaterole))
 AND NOT pg_has_role(current_user,'pg_read_all_stats','USAGE')
 AND NOT EXISTS (SELECT 1 FROM relations c WHERE NOT c.public_builtin AND c.nspname NOT LIKE 'pg_toast%'
 AND NOT(c.nspname='agh_fulfillment_zenventory' AND c.relname='purchase_orders')
 AND ((has_any_column_privilege(current_user,c.oid,'SELECT') AND NOT c.extension_member)
 OR has_any_column_privilege(current_user,c.oid,'INSERT,UPDATE,REFERENCES')
 OR has_table_privilege(current_user,c.oid,'DELETE,TRUNCATE,TRIGGER')))
 AND NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid='agh_fulfillment_zenventory.purchase_orders'::regclass AND a.attnum>0 AND NOT a.attisdropped
 AND a.attname NOT IN ('id','order_number','supplier_name','draft','deleted','completed','created_date','required_by_date','completed_date','_synced_at','items')
 AND has_column_privilege(current_user,a.attrelid,a.attname,'SELECT'))
 AND NOT has_table_privilege(current_user,'agh_fulfillment_zenventory.purchase_orders','INSERT,UPDATE,DELETE,TRUNCATE,TRIGGER')
 AND NOT has_any_column_privilege(current_user,'agh_fulfillment_zenventory.purchase_orders','INSERT,UPDATE,REFERENCES')
 AND (SELECT bool_and(has_column_privilege(current_user,'agh_fulfillment_zenventory.purchase_orders',col,'SELECT')) FROM unnest(ARRAY['id','order_number','supplier_name','draft','deleted','completed','created_date','required_by_date','completed_date','_synced_at','items']) col)
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind='S' AND n.nspname NOT IN ('pg_catalog','information_schema') AND has_sequence_privilege(current_user,c.oid,'SELECT,UPDATE,USAGE'))`

func verifyScope(ctx context.Context, p *pgxpool.Pool) error {
	var ok bool
	err := p.QueryRow(ctx, scopeCheckSQL).Scan(&ok)
	if err != nil || !ok {
		return errors.New("print PO login must have only scoped read access to the required PO columns")
	}
	return nil
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
	// Omitted rows can survive a source upsert. Only its latest snapshot confirms.
	p, raw, err := scanPO(tx.QueryRow(ctx, `SELECT `+poColumns+` FROM agh_fulfillment_zenventory.purchase_orders WHERE id=$1 AND _synced_at=(SELECT max(_synced_at) FROM agh_fulfillment_zenventory.purchase_orders)`, link.POID))
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
		WHERE _synced_at=(SELECT max(_synced_at) FROM agh_fulfillment_zenventory.purchase_orders)
		AND items @> jsonb_build_array(jsonb_build_object('sku',$1::text)) ORDER BY created_date DESC NULLS LAST, id DESC LIMIT 50`, sku)
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
