package store_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hackclub/merch/internal/db/dbtest"
)

func TestReplayExportMatchesTheOriginalCart(t *testing.T) {
	p := dbtest.New(t)
	ctx := t.Context()
	_, err := p.Exec(ctx, `
insert into users(id,hca_id,email) values(1,'example','orpheus@hackclub.com');
insert into items(id,sku,name) values(1,'Sti/Test','Example sticker');
insert into swag_requests(id,user_id,status,first_name,last_name,line_1,city,postal_code,country,
created_at,mailed_at,theseus_order_id,shipping_fee_cents,warehouse_postage_cents)
values(1,1,'dispatched','Orpheus','Test','1 Example Way','Example','00000','CA',
'2026-11-01 12:00Z','2026-11-04','pkg!example',950,1000);
insert into swag_request_items(request_id,item_id,quantity,requested_quantity,sku)
values(1,1,50,50,'Sti/Test');
insert into usage_events(at,source,name,visitor,user_id,props) values
('2026-11-01 12:00Z','server','request_created','example-visitor',1,
'{"request_id":1,"country":"CA","lines":[{"item_id":1,"quantity":50}]}'),
('2026-11-01 11:50Z','server','shipping_estimate','example-visitor',null,
'{"country":"CA","lines":[{"item_id":1,"quantity":50}],"weight_oz":4,"postage_typical_cents":950}'),
('2026-11-01 11:59Z','server','shipping_estimate','example-visitor',1,
'{"country":"CA","lines":[{"item_id":1,"quantity":10}],"weight_oz":1,"postage_typical_cents":100}'),
('2026-11-01 11:59:30Z','server','shipping_estimate','example-visitor',1,
'{"country":"GB","lines":[{"item_id":1,"quantity":50}],"weight_oz":5,"postage_typical_cents":200}'),
('2026-11-01 12:01Z','server','shipping_estimate','example-visitor',1,
'{"country":"CA","lines":[{"item_id":1,"quantity":50}],"weight_oz":6,"postage_typical_cents":300}');`)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("../../scripts/shipping-history/replay-requests.sql")
	if err != nil {
		t.Fatal(err)
	}
	query := strings.ReplaceAll(strings.ReplaceAll(string(b), ":'since'", "'2026-11-01'"), ":'until'", "'2026-11-10'")
	check := func(wantUnchanged bool) {
		t.Helper()
		var id int64
		var country, order, quote, weight string
		var day time.Time
		var charged, actual int
		var lines []byte
		var unchanged bool
		err := p.QueryRow(ctx, query).Scan(&id, &country, &day, &order, &charged, &quote, &weight, &actual, &lines, &unchanged)
		if err != nil {
			t.Fatal(err)
		}
		if country != "CA" || order != "hack.club/pkg!example" || quote != "950" || weight != "4" || charged != 950 || actual != 1000 || unchanged != wantUnchanged {
			t.Fatalf("wrong quote match: country=%s order=%s quote=%s weight=%s unchanged=%v", country, order, quote, weight, unchanged)
		}
	}
	check(true)
	if _, err = p.Exec(ctx, "update swag_request_items set quantity=20 where request_id=1"); err != nil {
		t.Fatal(err)
	}
	check(false)
}
