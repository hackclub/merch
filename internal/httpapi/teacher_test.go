package httpapi

import (
	"strings"
	"testing"

	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
)

func TestTeacherRequest(t *testing.T) {
	h := newHarness(t)
	admin, user := h.client(adminEmail), h.client(userEmail)
	var item swag.Item
	h.do(admin, "POST", "/api/admin/items", swag.Item{SKU: "Sti/A", Name: "Magazine", MaxPerRequest: 5, Visible: true, ClubOnly: true}, &item)
	body := map[string]any{"purpose": "teacher", "address": usAddr, "lines": []swag.Line{{ItemID: item.ID, Quantity: 1}}, "note": "Introducing students to Hack Club."}
	var out store.Request
	if code := h.do(user, "POST", "/api/requests", body, nil); code != 422 {
		t.Fatalf("missing classroom = %d", code)
	}
	// Refused like any other rule: same props as the rest of request_refused.
	if e := h.events("request_refused"); len(e) != 1 || props(t, e[0])["purpose"] != "teacher" || props(t, e[0])["error"] != swag.ErrClassroom.Error() || props(t, e[0])["country"] != "US" {
		t.Fatalf("refused = %+v", e)
	}
	body["teacher"] = map[string]any{"school": "Example School", "grades": "9–12", "students": 30}
	body["note"] = " "
	if code := h.do(user, "POST", "/api/requests", body, nil); code != 422 {
		t.Fatalf("classroom must not replace the plan = %d", code)
	}
	body["note"] = "Introducing students to Hack Club."
	if code := h.do(user, "POST", "/api/requests", body, &out); code != 201 {
		t.Fatalf("teacher = %d", code)
	}
	if string(out.Purpose) != "teacher" || !strings.Contains(out.Note, "School: Example School") || !strings.Contains(out.Note, "Students: 30") {
		t.Fatalf("saved = %+v", out)
	}
	var mine struct {
		Requests []store.Request `json:"requests"`
	}
	h.do(user, "GET", "/api/requests", nil, &mine)
	if len(mine.Requests) != 1 || mine.Requests[0].Note != out.Note {
		t.Fatal("classroom not persisted")
	}
	e := h.events("request_created")
	if len(e) != 1 || props(t, e[0])["purpose"] != "teacher" {
		t.Fatal("missing purpose event")
	}
	for _, event := range e {
		if strings.Contains(string(event.Props), "Example School") {
			t.Fatal("private school in analytics")
		}
	}
}
