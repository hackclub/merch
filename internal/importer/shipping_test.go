package importer

import (
	"bytes"
	"strings"
	"testing"
)

func TestShipmentsRoundTrip(t *testing.T) {
	in := `zenventory,GB,parcel,9.1,630,2026-09-11,2026-09-14,,AS000000001US
theseus_letter,US,letter,1.00,74,,2026-08-03,2026-08-08,
`
	ships, err := ParseShipments(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(ships) != 2 {
		t.Fatalf("got %d", len(ships))
	}
	gb := ships[0]
	if gb.Country != "GB" || gb.Class != "parcel" || *gb.WeightOz != 9.1 || *gb.PostageCents != 630 ||
		gb.OrderedOn.Format("2006-01-02") != "2026-09-11" || gb.DeliveredOn != nil || gb.Tracking != "AS000000001US" {
		t.Fatalf("got %+v", gb)
	}
	if ships[1].OrderedOn != nil || ships[1].DeliveredOn.Format("2006-01-02") != "2026-08-08" {
		t.Fatalf("got %+v", ships[1])
	}

	var buf bytes.Buffer
	if err := WriteShipments(&buf, ships, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "AS000000001US") {
		t.Fatal("tracking numbers must not be written unless asked")
	}
	again, err := ParseShipments(&buf)
	if err != nil || len(again) != 2 || *again[0].PostageCents != 630 {
		t.Fatalf("round trip: %+v, %v", again, err)
	}
}

func TestShipmentsRejectBadRows(t *testing.T) {
	for _, row := range []string{
		"zenventory,Canada,parcel,1,1,,2026-01-01,,",
		"zenventory,CA,box,1,1,,2026-01-01,,",
		"zenventory,CA,parcel,heavy,1,,2026-01-01,,",
		"zenventory,CA,parcel,1,1,,,,",
	} {
		if _, err := ParseShipments(strings.NewReader(row)); err == nil {
			t.Errorf("%q: want an error", row)
		}
	}
}
