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

// UploadStream sends the file as it reads it, with an exact Content-Length when
// the size is known, so a big print file never sits in memory.
func TestUploadStream(t *testing.T) {
	payload := strings.Repeat("png!", 1<<18) // 1 MB
	var gotLen int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLen = r.ContentLength
		f, hdr, err := r.FormFile("file")
		if err != nil { // a cut-off body
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(f)
		if hdr.Filename != "poster.png" || string(data) != payload || hdr.Header.Get("Content-Type") != "image/png" {
			t.Errorf("file = %s, %d bytes, %s", hdr.Filename, len(data), hdr.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"url":"https://cdn.hackclub.com/u2/poster.png"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "k", nil)
	url, err := c.UploadStream(context.Background(), "poster.png", "image/png", strings.NewReader(payload), int64(len(payload)))
	if err != nil || url != "https://cdn.hackclub.com/u2/poster.png" {
		t.Fatalf("UploadStream = %q, %v", url, err)
	}
	if gotLen <= int64(len(payload)) {
		t.Fatalf("Content-Length = %d, want the payload plus its multipart framing", gotLen)
	}
	// Unknown size: sent chunked.
	if _, err := c.UploadStream(context.Background(), "poster.png", "image/png", strings.NewReader(payload), -1); err != nil || gotLen != -1 {
		t.Fatalf("chunked = %v, Content-Length %d", err, gotLen)
	}
	// A body that ends early (the requester went away) fails rather than storing half a file.
	if _, err := c.UploadStream(context.Background(), "poster.png", "image/png", strings.NewReader(payload[:100]), int64(len(payload))); err == nil {
		t.Fatal("short body uploaded")
	}
}
