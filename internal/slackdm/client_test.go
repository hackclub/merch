package slackdm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOnlyDirectMessagesAndStableKeys(t *testing.T) {
	calls := 0
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fake-token" {
			t.Error("missing auth")
		}
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		if r.URL.Path == "/conversations.open" {
			if b["users"] != "UDEMO" {
				t.Error("not the verified identity")
			}
			w.Write([]byte(`{"ok":true,"channel":{"id":"DDEMO"}}`))
			return
		}
		if b["channel"] != "DDEMO" || b["unfurl_links"] != false {
			t.Errorf("unsafe message %+v", b)
		}
		keys = append(keys, b["client_msg_id"].(string))
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "fake-token", nil)
	for i := 0; i < 2; i++ {
		if err := c.Send(context.Background(), "UDEMO", "Approved. View your print request.", "event-12"); err != nil {
			t.Fatal(err)
		}
	}
	if len(keys) != 2 || keys[0] != keys[1] {
		t.Fatal("retry key changed")
	}
	if err := c.Send(context.Background(), "CDEMO", "No channel posts", "event-13"); err == nil {
		t.Fatal("accepted channel")
	}
	if calls != 4 {
		t.Fatalf("calls %d", calls)
	}
}
