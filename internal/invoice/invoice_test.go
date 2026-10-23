package invoice

import (
	"bytes"
	"fmt"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/hackclub/merch/internal/swag"
)

func TestDollars(t *testing.T) {
	for v, want := range map[float64]string{
		9:       "$9.00",
		0.05:    "$0.05",
		13.967:  "$13.967",
		4.1027:  "$4.1027",
		0.17473: "$0.1747",
		1250.5:  "$1,250.50",
	} {
		if got := Dollars(v); got != want {
			t.Errorf("Dollars(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestConsignee(t *testing.T) {
	a := swag.Address{FirstName: "Orpheus", LastName: "Dino", Line1: "Str 1", Line2: "Apt 2", City: "Berlin", PostalCode: "10115", Country: "DE", Phone: "+49301234567"}
	want := []string{"Orpheus Dino", "Str 1", "Apt 2", "Berlin 10115", "Germany", "Phone: +49301234567"}
	if got := Consignee(a); !reflect.DeepEqual(got, want) {
		t.Fatalf("Consignee = %q", got)
	}
	a = swag.Address{FirstName: "O", LastName: "D", Line1: "1 Main", City: "Toronto", State: "ON", PostalCode: "M5V 2T6", Country: "CA"}
	want = []string{"O D", "1 Main", "Toronto, ON M5V 2T6", "Canada"}
	if got := Consignee(a); !reflect.DeepEqual(got, want) {
		t.Fatalf("Consignee = %q", got)
	}
}

func doc(lines int) Doc {
	var inv swag.Invoice
	for i := range lines {
		l := swag.InvoiceLine{SKU: fmt.Sprintf("Sti/%d", i), Description: fmt.Sprintf("Sticker %d", i), Quantity: 2, UnitValue: 0.25, ValueCents: 50}
		inv.Lines = append(inv.Lines, l)
		inv.TotalCents += l.ValueCents
	}
	return Doc{
		Number: "MERCH-42", Date: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC),
		// Names and addresses abroad aren't all Latin-1.
		To:       []string{"Łukasz Żółć", "ул. Пушкина 1", "Ελληνικά 2", "Poland"},
		Shipment: "Asendia 1234",
		Invoice:  inv,
	}
}

var pages = regexp.MustCompile(`/Type /Page\b`)

func TestRender(t *testing.T) {
	for _, tc := range []struct{ lines, pages int }{{3, 1}, {80, 3}} {
		b, err := Render(doc(tc.lines))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(b, []byte("%PDF-")) || !bytes.Contains(b[len(b)-16:], []byte("%%EOF")) {
			t.Fatalf("not a PDF: %q…", b[:16])
		}
		if n := len(pages.FindAll(b, -1)); n != tc.pages {
			t.Errorf("%d lines: %d pages, want %d", tc.lines, n, tc.pages)
		}
	}
}
