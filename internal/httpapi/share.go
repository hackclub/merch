package httpapi

import (
	"bytes"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The app renders in the browser, but link unfurlers (Slack, iMessage, X)
// read only the HTML. So a product page's index.html gets that product's
// share tags in place of the site's own, between these markers in app.html.
var (
	shareBlock = regexp.MustCompile(`(?s)<!-- share -->.*?<!-- /share -->`)
	itemPath   = regexp.MustCompile(`^/items/(\d+)(?:-[^/]*)?$`)
)

// serveIndex serves the SPA shell, with product share tags on /items/<id>.
func (s *server) serveIndex(w http.ResponseWriter, r *http.Request) {
	tags := s.itemShareTags(r)
	if tags == "" {
		http.ServeFileFS(w, r, s.Static, "index.html")
		return
	}
	b, err := fs.ReadFile(s.Static, "index.html")
	if err != nil {
		http.ServeFileFS(w, r, s.Static, "index.html")
		return
	}
	b = shareBlock.ReplaceAllLiteral(b, []byte(tags))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(b))
}

func (s *server) itemShareTags(r *http.Request) string {
	m := itemPath.FindStringSubmatch(r.URL.Path)
	if m == nil {
		return ""
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return ""
	}
	it, err := s.Store.GetItem(r.Context(), id)
	if err != nil || !it.Visible {
		return ""
	}
	title := it.Name + " · Hack Club Merch"
	desc := shareText(it.Description, 200)
	if desc == "" {
		desc = "Free Hack Club swag, mailed to you. Request " + it.Name + " for your club, hackathon or booth."
	}
	tag := func(attr, key, val string) string {
		return fmt.Sprintf(`<meta %s="%s" content="%s" />`+"\n", attr, key, html.EscapeString(val))
	}
	var b strings.Builder
	b.WriteString("<!-- share -->\n")
	b.WriteString(tag("property", "og:type", "product"))
	b.WriteString(tag("property", "og:site_name", "Hack Club Merch"))
	b.WriteString(tag("property", "og:title", title))
	b.WriteString(tag("property", "og:description", desc))
	b.WriteString(tag("property", "og:url", s.origin(r)+r.URL.Path))
	if len(it.Images) > 0 {
		b.WriteString(tag("property", "og:image", it.Images[0]))
		b.WriteString(tag("property", "og:image:alt", it.Name))
	}
	b.WriteString(tag("name", "twitter:card", "summary_large_image"))
	b.WriteString(tag("name", "twitter:title", title))
	b.WriteString(tag("name", "twitter:description", desc))
	if len(it.Images) > 0 {
		b.WriteString(tag("name", "twitter:image", it.Images[0]))
	}
	b.WriteString(tag("name", "description", desc))
	b.WriteString("<!-- /share -->")
	return b.String()
}

// shareText flattens a description to one line of at most max characters,
// cut at a word.
func shareText(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := string(r[:max])
	if i := strings.LastIndex(cut, " "); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}
