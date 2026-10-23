// Package invoice renders a customs invoice for a request shipped abroad: a
// pro forma invoice that says the contents are a gift, at the values the
// warehouse declares with the parcel, for the requester to show customs.
package invoice

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"github.com/hackclub/merch/internal/swag"
)

// Shipper is the exporter: Hack Club HQ, as on mail.hackclub.com's own
// customs receipts.
var Shipper = []string{"Hack Club", "212 Battery St, Suite 3", "Burlington, VT 05401", "United States"}

const footer = "Hack Club is a 501(c)(3) public charity. EIN 81-2908499. hackclub.com"

const declaration = "These goods are a gift. Hack Club, a nonprofit that runs coding clubs for teenagers, " +
	"sends them to the consignee free of charge. They were not sold and are not for resale. " +
	"Each value is Hack Club's cost per unit, given for customs purposes only."

// Doc is one invoice.
type Doc struct {
	Number   string    // "MERCH-42"
	Date     time.Time // when it shipped, or today
	To       []string  // the consignee (Consignee)
	Shipment string    // carrier and tracking number, once known
	Invoice  swag.Invoice
}

// Consignee is the address as invoice lines, with the country's English name
// and the phone number customs uses to reach the recipient.
func Consignee(a swag.Address) []string {
	out := []string{strings.TrimSpace(a.FirstName + " " + a.LastName), a.Line1}
	if a.Line2 != "" {
		out = append(out, a.Line2)
	}
	city := a.City
	if a.State != "" {
		city += ", " + a.State
	}
	out = append(out, strings.TrimSpace(city+" "+a.PostalCode), CountryName(a.Country))
	if a.Phone != "" {
		out = append(out, "Phone: "+a.Phone)
	}
	return out
}

// CountryName is an ISO 3166-1 alpha-2 code's English name ("DE" → "Germany").
func CountryName(code string) string {
	if r, err := language.ParseRegion(code); err == nil {
		if n := display.English.Regions().Name(r); n != "" {
			return n
		}
	}
	return code
}

// Dollars shows a value as precisely as the warehouse declares it, to at most
// four decimal places and at least two: "$0.1747", "$13.967", "$9.00".
func Dollars(v float64) string {
	s := strconv.FormatFloat(v, 'f', 4, 64)
	whole, frac, _ := strings.Cut(s, ".")
	frac = strings.TrimRight(frac, "0")
	for len(frac) < 2 {
		frac += "0"
	}
	return "$" + thousands(whole) + "." + frac
}

// Cents shows cents as dollars: "$1,250.50".
func Cents(c int) string {
	return "$" + thousands(strconv.Itoa(c/100)) + fmt.Sprintf(".%02d", c%100)
}

func thousands(s string) string {
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// Page layout, in millimeters on US Letter.
const (
	margin = 18.0
	bottom = 24.0 // room for the footer
	rowH   = 7.0
)

var (
	red   = [3]int{236, 55, 80} // Hack Club red
	ink   = [3]int{31, 45, 61}
	muted = [3]int{122, 135, 148}
	rule  = [3]int{221, 225, 228}
)

type column struct {
	label string
	w     float64
	align string
	cell  func(swag.InvoiceLine) string
}

// Render draws the invoice as a PDF.
func Render(d Doc) ([]byte, error) {
	p := fpdf.New("P", "mm", "Letter", "")
	p.AddUTF8FontFromBytes("go", "", goregular.TTF)
	p.AddUTF8FontFromBytes("go", "B", gobold.TTF)
	p.AddUTF8FontFromBytes("mono", "", gomono.TTF)
	p.SetMargins(margin, margin, margin)
	p.SetAutoPageBreak(true, bottom)
	p.SetTitle("Customs invoice "+d.Number, true)
	p.SetAuthor("Hack Club", true)
	p.SetCreationDate(d.Date)
	p.SetModificationDate(d.Date)
	p.AliasNbPages("")
	pageW, pageH := p.GetPageSize()
	width := pageW - 2*margin

	color := func(c [3]int) { p.SetTextColor(c[0], c[1], c[2]) }
	label := func(s string) {
		p.SetFont("mono", "", 7.5)
		color(muted)
		p.CellFormat(0, 4.5, strings.ToUpper(s), "", 1, "L", false, 0, "")
	}
	hr := func(y float64) {
		p.SetDrawColor(rule[0], rule[1], rule[2])
		p.SetLineWidth(0.25)
		p.Line(margin, y, pageW-margin, y)
	}
	p.SetFooterFunc(func() {
		p.SetY(-16)
		p.SetFont("mono", "", 7)
		color(muted)
		p.CellFormat(width/2+30, 4, footer, "", 0, "L", false, 0, "")
		p.CellFormat(0, 4, fmt.Sprintf("%s · PAGE %d OF {nb}", d.Number, p.PageNo()), "", 0, "R", false, 0, "")
	})

	p.AddPage()

	// Header: the wordmark, and what this is.
	p.SetFont("go", "B", 22)
	color(red)
	p.CellFormat(width/2, 10, "Hack Club", "", 0, "L", false, 0, "")
	color(ink)
	p.CellFormat(0, 10, "Customs Invoice", "", 1, "R", false, 0, "")
	p.SetFont("mono", "", 8)
	color(muted)
	p.CellFormat(0, 5, "PRO FORMA INVOICE · GIFT · NO COMMERCIAL SALE", "", 1, "R", false, 0, "")
	p.Ln(4)
	hr(p.GetY())
	p.Ln(5)

	// Shipper, consignee and the invoice's details, side by side.
	details := []string{
		"Invoice no.: " + d.Number,
		"Date: " + d.Date.Format("2 January 2006"),
		"Reason for export: Gift",
		"Terms: Free of charge",
		"Currency: USD",
	}
	if d.Shipment != "" {
		details = append(details, "Tracking: "+d.Shipment)
	}
	blocks := []struct {
		title string
		lines []string
	}{{"Shipper / exporter", Shipper}, {"Consignee / recipient", d.To}, {"Details", details}}
	colW := width / 3
	top := p.GetY()
	end := top
	for i, b := range blocks {
		x := margin + float64(i)*colW
		p.SetXY(x, top)
		p.SetLeftMargin(x)
		label(b.title)
		p.Ln(0.5)
		for j, l := range b.lines {
			if j == 0 && i < 2 {
				p.SetFont("go", "B", 10)
			} else {
				p.SetFont("go", "", 10)
			}
			color(ink)
			p.MultiCell(colW-4, 5, l, "", "L", false)
		}
		end = math.Max(end, p.GetY())
	}
	p.SetLeftMargin(margin)
	p.SetXY(margin, end+6)

	// Contents. HS code and origin columns only when the warehouse has them.
	var hasHS, hasOrigin bool
	for _, l := range d.Invoice.Lines {
		hasHS = hasHS || l.HSCode != ""
		hasOrigin = hasOrigin || l.Origin != ""
	}
	cols := []column{{label: "Description", align: "L", cell: func(l swag.InvoiceLine) string { return l.Description }}}
	if hasHS {
		cols = append(cols, column{"HS code", 24, "L", func(l swag.InvoiceLine) string { return l.HSCode }})
	}
	if hasOrigin {
		cols = append(cols, column{"Origin", 22, "L", func(l swag.InvoiceLine) string { return l.Origin }})
	}
	cols = append(cols,
		column{"Qty", 14, "R", func(l swag.InvoiceLine) string { return strconv.Itoa(l.Quantity) }},
		column{"Unit value", 26, "R", func(l swag.InvoiceLine) string { return Dollars(l.UnitValue) }},
		column{"Value", 26, "R", func(l swag.InvoiceLine) string { return Cents(l.ValueCents) }},
	)
	cols[0].w = width
	for _, c := range cols[1:] {
		cols[0].w -= c.w
	}
	header := func() {
		p.SetFont("mono", "", 7.5)
		color(muted)
		for _, c := range cols {
			p.CellFormat(c.w, rowH, strings.ToUpper(c.label), "", 0, c.align, false, 0, "")
		}
		p.Ln(rowH)
		hr(p.GetY())
	}
	header()
	for _, l := range d.Invoice.Lines {
		if p.GetY()+rowH > pageH-bottom {
			p.AddPage()
			header()
		}
		p.SetFont("go", "", 10)
		color(ink)
		for _, c := range cols {
			p.CellFormat(c.w, rowH, fit(p, c.cell(l), c.w-2), "", 0, c.align, false, 0, "")
		}
		p.Ln(rowH)
		hr(p.GetY())
	}
	if p.GetY()+rowH+40 > pageH-bottom {
		p.AddPage()
	}
	p.Ln(1)
	p.SetFont("go", "B", 11)
	color(ink)
	p.CellFormat(width-26, rowH+1, "Total value (USD)", "", 0, "R", false, 0, "")
	p.CellFormat(26, rowH+1, Cents(d.Invoice.TotalCents), "", 1, "R", false, 0, "")
	p.Ln(8)

	label("Declaration")
	p.Ln(0.5)
	p.SetFont("go", "", 10)
	color(ink)
	p.MultiCell(0, 5, declaration, "", "L", false)
	p.Ln(2)
	p.MultiCell(0, 5, "Hack Club declares that the information on this invoice is true and correct.", "", "L", false)

	var out bytes.Buffer
	if err := p.Output(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// fit trims s with an ellipsis to fit w millimeters in the current font.
func fit(p *fpdf.Fpdf, s string, w float64) string {
	if p.GetStringWidth(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && p.GetStringWidth(string(r)+"…") > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}
