package loops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSend(t *testing.T) {
	var got struct {
		TransactionalID string            `json:"transactionalId"`
		Email           string            `json:"email"`
		DataVariables   map[string]string `json:"dataVariables"`
	}
	var auth, key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/transactional" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		auth, key = r.Header.Get("Authorization"), r.Header.Get("Idempotency-Key")
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "key_x", "tx_1", nil)
	err := c.Send(context.Background(), Email{To: "orpheus@hackclub.com", Subject: "Hi", Preview: "Pre", Body: "Hi <b>&\n\nBody", IdempotencyKey: "merch-request-1-received"})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer key_x" || key != "merch-request-1-received" || got.TransactionalID != "tx_1" || got.Email != "orpheus@hackclub.com" {
		t.Fatalf("auth=%q key=%q got=%+v", auth, key, got)
	}
	if v := got.DataVariables; v["subject_line"] != "Hi" || v["preview_text"] != "Pre" || v["body_text"] != "Hi &lt;b&gt;&amp;<br><br>Body" {
		t.Fatalf("variables = %v", v)
	}
}

func TestSendError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"success":false,"message":"Missing required fields"}`))
	}))
	defer srv.Close()
	err := New(srv.URL, "k", "tx", nil).Send(context.Background(), Email{To: "a@x.com"})
	if err == nil || !strings.Contains(err.Error(), "Missing required fields") {
		t.Fatalf("err = %v", err)
	}
}

// A repeated idempotency key means Loops already sent this email.
func TestSendDuplicateIsSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		w.Write([]byte(`{"success":false,"message":"Idempotency key already used"}`))
	}))
	defer srv.Close()
	if err := New(srv.URL, "k", "tx", nil).Send(context.Background(), Email{To: "a@x.com", IdempotencyKey: "x"}); err != nil {
		t.Fatalf("err = %v", err)
	}
}
