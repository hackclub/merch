package store_test

import (
	"context"
	"encoding/csv"
	"os"
	"strings"
	"testing"

	"github.com/hackclub/merch/internal/db/dbtest"
)

func TestShippingExportAsendiaReportingGap(t *testing.T) {
	p := dbtest.New(t)
	ctx := context.Background()
	_, err := p.Exec(ctx, `create schema agh_fulfillment_zenventory;
create table agh_fulfillment_zenventory.customer_orders (order_number text, ordered_date timestamptz);
create table agh_fulfillment_zenventory.shipments (
order_number text, country text, service text, carrier text, weight float8,
shipping_handling float8, shipped_date date, tracking_number text);
insert into agh_fulfillment_zenventory.customer_orders values ('synthetic', '2025-01-01');
insert into agh_fulfillment_zenventory.shipments
select 'synthetic', 'IN', 'Asendia ePAQ', 'Asendia', 1, 14.64, d::date, ''
from unnest(array['2026-09-30','2026-10-01','2026-10-20','2026-10-21','2026-11-05']) d;
insert into agh_fulfillment_zenventory.shipments values
('synthetic','US','USPS Ground Advantage','USPS',1,8,'2026-10-10','');`)
	if err != nil {
		t.Fatal(err)
	}
	sql, err := os.ReadFile("../../scripts/shipping-history/zenventory.sql")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := p.Query(ctx, string(sql))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := map[string]string{"2026-09-30": "1464", "2026-10-01": "", "2026-10-20": "", "2026-10-21": "1464", "2026-11-05": "1464", "2026-10-10": "800"}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		r, err := csv.NewReader(strings.NewReader(line)).Read()
		if err != nil {
			t.Fatal(err)
		}
		if value, ok := want[r[6]]; !ok || r[4] != value {
			t.Errorf("%s postage = %q, want %q", r[6], r[4], value)
		}
		delete(want, r[6])
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(want) > 0 {
		t.Fatalf("missing shipments: %v", want)
	}
}
