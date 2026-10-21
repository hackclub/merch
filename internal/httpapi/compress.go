package httpapi

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// gzipJSON gzips JSON from /api/* when the browser takes it. Static files come precompressed
// (serveStatic), but API answers are built per request, and the catalog is about 65 KB raw:
// several round trips to the server in Finland before the shop can draw. Gzipped (14 KB) it
// fits in the first one. BestSpeed costs well under a millisecond of CPU for the catalog.
func gzipJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || !acceptedEncodings(r.Header.Get("Accept-Encoding"))["gzip"] {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

var gzipPool = sync.Pool{New: func() any {
	zw, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
	return zw
}}

type gzipWriter struct {
	http.ResponseWriter
	zw          *gzip.Writer
	wroteHeader bool
}

func (g *gzipWriter) WriteHeader(code int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	h := g.Header()
	if strings.HasPrefix(h.Get("Content-Type"), "application/json") && h.Get("Content-Encoding") == "" &&
		code != http.StatusNoContent && code != http.StatusNotModified {
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
		h.Add("Vary", "Accept-Encoding")
		g.zw = gzipPool.Get().(*gzip.Writer)
		g.zw.Reset(g.ResponseWriter)
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	if g.zw != nil {
		return g.zw.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipWriter) close() {
	if g.zw != nil {
		g.zw.Close()
		gzipPool.Put(g.zw)
	}
}
