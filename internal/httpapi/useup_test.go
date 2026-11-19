package httpapi

import (
	"context"
	"fmt"
	"testing"

	"github.com/hackclub/merch/internal/swag"
)

// A new shirt that names the old one to use up first takes only the sizes the
// old shirt is out of; the admin sets the link on the Items tab, and refusals
// are logged like any other rule.
func TestUseUpFirst(t *testing.T) {
	h := newHarness(t)
	admin := h.client(adminEmail)
	ctx := context.Background()
	// The warehouse has the old shirt in S and M and no L (fakeWarehouse).
	old, err := h.store.CreateItem(ctx, swag.Item{SKU: "Swa/Shirt/HC", Name: "Hack Club T-Shirt", Visible: true, MaxPerRequest: 15, Variants: []swag.Variant{
		{Label: "S", SKU: "Swa/Shirt/HC/S"}, {Label: "M", SKU: "Swa/Shirt/HC/M"}, {Label: "L", SKU: "Swa/Shirt/HC/L"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	heidi, err := h.store.CreateItem(ctx, swag.Item{SKU: "Swa/Shirt/HHw", Name: "Heidi T-Shirt", Visible: true, MaxPerRequest: 15, AllowBackorder: true, Variants: []swag.Variant{
		{Label: "S", SKU: "Swa/Shirt/HHw/S"}, {Label: "L", SKU: "Swa/Shirt/HHw/L"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ask := func(sku string) (int, string) {
		var out map[string]any
		code := h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr,
			"lines": []swag.Line{{ItemID: heidi.ID, Quantity: 1, SKU: sku}}}, &out)
		msg, _ := out["error"].(string)
		return code, msg
	}

	// Before the link, any size goes (it's backordered).
	if code, msg := ask("Swa/Shirt/HHw/S"); code != 201 {
		t.Fatalf("heidi S before the link = %d %q", code, msg)
	}

	heidi.UseUpFirst = &old.ID
	var upd swag.Item
	if code := h.do(admin, "PUT", fmt.Sprintf("/api/admin/items/%d", heidi.ID), heidi, &upd); code != 200 || upd.UseUpFirst == nil || *upd.UseUpFirst != old.ID {
		t.Fatalf("set use_up_first = %d %+v", code, upd.UseUpFirst)
	}
	if e := h.events("item_updated"); len(e) != 1 || props(t, e[0])["changed"].(map[string]any)["use_up_first"] == nil {
		t.Fatalf("item_updated = %+v", e)
	}
	self := upd
	self.UseUpFirst = &self.ID
	var bad map[string]any
	if code := h.do(admin, "PUT", fmt.Sprintf("/api/admin/items/%d", heidi.ID), self, &bad); code != 409 || bad["error"] != "an item can't be used up before itself" {
		t.Fatalf("use up itself first = %d %v", code, bad)
	}

	if code, msg := ask("Swa/Shirt/HHw/S"); code != 422 || msg != "use up older stock first: Hack Club T-Shirt still has size S, so request that instead" {
		t.Fatalf("heidi S while the old S is in stock = %d %q", code, msg)
	}
	if e := h.events("request_refused"); len(e) != 1 || props(t, e[0])["error"] != "use up older stock first: Hack Club T-Shirt still has size S, so request that instead" {
		t.Fatalf("request_refused = %+v", e)
	}
	if code, msg := ask("Swa/Shirt/HHw/L"); code != 201 {
		t.Fatalf("heidi L once the old L is out = %d %q", code, msg)
	}

	// The catalog carries the link and the old shirt's stock, so the shop can say so.
	var cat struct {
		Items []swag.Item
		Stock map[string]struct {
			InStock int `json:"in_stock"`
		}
	}
	h.do(h.client(""), "GET", "/api/catalog", nil, &cat)
	found := false
	for _, it := range cat.Items {
		if it.ID == heidi.ID {
			found = it.UseUpFirst != nil && *it.UseUpFirst == old.ID
		}
	}
	if !found || cat.Stock["Swa/Shirt/HC/S"].InStock != 92 {
		t.Fatalf("catalog = %+v", cat)
	}
}
