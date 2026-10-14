package cdn

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v4/upload" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("auth header = %q", got)
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(f)
		if hdr.Filename != "front.jpg" || string(data) != "jpegbytes" || hdr.Header.Get("Content-Type") != "image/jpeg" {
			t.Errorf("file = %s %q %s", hdr.Filename, data, hdr.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"u1","filename":"front.jpg","size":9,"content_type":"image/jpeg","url":"https://cdn.hackclub.com/u1/front.jpg","created_at":"2026-10-14T00:00:00Z"}`))
	}))
	defer srv.Close()

	url, err := New(srv.URL, "k", nil).Upload(context.Background(), "front.jpg", "image/jpeg", []byte("jpegbytes"))
	if err != nil || url != "https://cdn.hackclub.com/u1/front.jpg" {
		t.Fatalf("Upload = %q, %v", url, err)
	}
}

func TestUploadErrorIncludesMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"error":"quota","code":"quota_exceeded","message":"Storage quota exceeded","status":402}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "k", nil).Upload(context.Background(), "a.jpg", "image/jpeg", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "Storage quota exceeded") {
		t.Fatalf("err = %v", err)
	}
}
