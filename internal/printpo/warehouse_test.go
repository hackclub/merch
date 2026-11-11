package printpo

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/hackclub/merch/internal/db/dbtest"
	"github.com/hackclub/merch/internal/swag"
)

func TestWarehouseExactLinkAndSuggestions(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	// Mirror shape, with invented data. Test a real read-only transaction.
	_, err := pool.Exec(ctx, `CREATE SCHEMA agh_fulfillment_zenventory;
	CREATE TABLE agh_fulfillment_zenventory.purchase_orders (
	 id bigint, order_number text, supplier_name text, draft boolean, deleted boolean, completed boolean,
	 created_date timestamptz, required_by_date date, completed_date timestamptz, _synced_at timestamptz, items jsonb);
	INSERT INTO agh_fulfillment_zenventory.purchase_orders VALUES
	(12,'DEMO-12','Demo printer',false,false,true,now()-interval '2 days',null,null,now(),
	 '[{"id":34,"sku":"Demo/Card","quantity":800,"unitCost":0.1}]'),
	(13,'DEMO-13','Demo printer',true,false,false,now(),null,null,now(),
	 '[{"id":35,"sku":"Demo/Card","quantity":900}]'),
	(14,'DEMO-14','Demo printer',false,true,false,now(),null,null,now(),
	 '[{"id":36,"sku":"Demo/Card","quantity":900}]')`)
	if err != nil {
		t.Fatal(err)
	}
	w := New(pool)
	links := []swag.PrintPOLink{{POID: 12, LineID: 34, SKU: "Demo/Card"}, {POID: 12, LineID: 99, SKU: "Demo/Card"}, {POID: 12, LineID: 34, SKU: "Demo/Card/Other"}}
	for i, link := range links {
		p, err := w.Exact(ctx, link)
		if i == 0 {
			if err != nil || !p.Completed || p.Quantity != 800 || p.CompletedAt != nil || p.SyncedAt.IsZero() || p.PolledAt.IsZero() {
				t.Fatalf("exact: %+v %v", p, err)
			}
		} else if err == nil {
			t.Fatalf("wrong identity accepted: %+v", link)
		}
	}
	suggestions, err := w.Suggest(ctx, "Demo/Card")
	if err != nil || len(suggestions) != 3 {
		t.Fatalf("suggestions: %+v %v", suggestions, err)
	}
	if _, err = w.Suggest(ctx, ""); err == nil {
		t.Fatal("empty SKU accepted")
	}
}

func TestDecodeLineRejectsAmbiguousSource(t *testing.T) {
	for _, raw := range []string{
		`[{"id":34,"sku":"Demo/Card","quantity":1.5}]`,
		`[{"id":34,"sku":"Demo/Card","quantity":0}]`,
		`[{"id":34,"sku":"Demo/Card","quantity":800},{"id":34,"sku":"Demo/Card","quantity":800}]`,
		`null`, `[]`,
	} {
		if _, err := decodeLine(json.RawMessage(raw), swag.PrintPOLink{POID: 12, LineID: 34, SKU: "Demo/Card"}); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

// Run explicitly with a read-only verification login and an exact SKU. These
// variables are test-only and must never be used as application configuration.
func TestWarehouseLiveReadOnly(t *testing.T) {
	dsn, sku := os.Getenv("PRINT_PO_VERIFY_DATABASE_URL"), os.Getenv("PRINT_PO_VERIFY_SKU")
	if dsn == "" || sku == "" {
		t.Skip("live read-only verification not requested")
	}
	w, err := Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal("cannot connect for live verification")
	}
	defer w.Close()
	rows, err := w.Suggest(context.Background(), sku)
	if err != nil {
		t.Fatal("live suggestion query failed")
	}
	if len(rows) == 0 {
		t.Fatal("no exact SKU candidates")
	}
	verified := false
	for _, p := range rows {
		if p.Draft || p.Deleted || !p.Completed {
			continue
		}
		exact, err := w.Exact(context.Background(), p.PrintPOLink)
		if err != nil || exact.Quantity != p.Quantity || !exact.Completed {
			t.Fatal("live exact-line verification failed")
		}
		if exact.CompletedAt == nil {
			t.Fatal("verification PO has no completion date")
		}
		if want := os.Getenv("PRINT_PO_VERIFY_COMPLETED_DATE"); want != "" && exact.CompletedAt.Format("2006-01-02") != want {
			continue
		}
		t.Logf("Verified exact PO/line/SKU; non-draft, non-deleted, completed %s; source sync %s; poll %s",
			exact.CompletedAt.Format("2006-01-02"), exact.SyncedAt.UTC().Format("2006-01-02T15:04:05Z"), exact.PolledAt.UTC().Format("2006-01-02T15:04:05Z"))
		verified = true
	}
	if !verified {
		t.Fatal("no completed PO satisfied verification date")
	}
}
