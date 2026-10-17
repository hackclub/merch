package store

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func TestRecordEvents(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "orpheus@hackclub.com")
	at := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
	err := s.RecordEvents(ctx, []Event{
		{At: at, Source: "client", Name: "page_view", Visitor: "v1", Visit: "s1", Path: "/", Props: json.RawMessage(`{"from":""}`), Release: "20261017T141645Z"},
		{Source: "server", Name: "shipping_estimate", Visitor: "v1", Visit: "s1", UserID: &u.ID, Path: "/", Props: json.RawMessage(`{"country":"DE"}`)},
		{Source: "client", Name: "click", Visitor: "v2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	evs, err := s.RecentEvents(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("got %d events", len(evs))
	}
	// Newest first; a zero At means now.
	pv := evs[2]
	if pv.Name != "page_view" || !pv.At.Equal(at) || pv.Visit != "s1" || string(pv.Props) != `{"from": ""}` || pv.UserID != nil || pv.Release != "20261017T141645Z" {
		t.Errorf("page_view = %+v (%s)", pv, pv.Props)
	}
	est := evs[1]
	if est.Name != "shipping_estimate" || est.UserID == nil || *est.UserID != u.ID || time.Since(est.At) > time.Minute {
		t.Errorf("estimate = %+v", est)
	}
	if string(evs[0].Props) != `{}` {
		t.Errorf("missing props should be {}, got %s", evs[0].Props)
	}
	only, err := s.RecentEvents(ctx, "click", 10)
	if err != nil || len(only) != 1 || only[0].Visitor != "v2" {
		t.Fatalf("by name = %+v, %v", only, err)
	}
}

func TestPageTimesView(t *testing.T) {
	s := newStore(t)
	snap := func(pv string, visible int) Event {
		return Event{Source: "client", Name: "page_time", Visitor: "v", Path: "/items/3-x",
			Props: json.RawMessage(`{"pv":"` + pv + `","item_id":3,"active_ms":1,"visible_ms":` + strconv.Itoa(visible) + `,"scroll_pct":50,"clicks":2,"seen":[3,4]}`), Release: "r1"}
	}
	if err := s.RecordEvents(ctx, []Event{snap("a", 1000), snap("a", 5000), snap("b", 200)}); err != nil {
		t.Fatal(err)
	}
	var n, seen int
	var visible int64
	var release string
	if err := s.pool.QueryRow(ctx, `SELECT count(*), sum(visible_ms), sum(jsonb_array_length(seen)), min(release) FROM usage_page_times WHERE item_id = 3`).Scan(&n, &visible, &seen, &release); err != nil {
		t.Fatal(err)
	}
	if seen != 4 || release != "r1" {
		t.Errorf("page times seen %d items in release %q; want 4 in r1", seen, release)
	}
	if n != 2 || visible != 5200 {
		t.Errorf("page times = %d rows, %d ms; want 2, 5200 (latest snapshot per view)", n, visible)
	}
}
