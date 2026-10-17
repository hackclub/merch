package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hackclub/merch/internal/swag"
)

const shareShell = `<html><head>
<!-- share -->
<meta property="og:title" content="Default card" />
<!-- /share -->
</head><body>app shell</body></html>`

func get(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s = %d", url, resp.StatusCode)
	}
	return string(b)
}

// A product link pasted into Slack or iMessage unfurls with that product's
// name, description and photo, though the app itself renders in the browser.
func TestProductPagesHaveShareTags(t *testing.T) {
	h := newHarness(t, func(d *Deps) {
		d.Static = fstest.MapFS{"index.html": {Data: []byte(shareShell)}}
	})
	ctx := context.Background()
	mag, err := h.store.CreateItem(ctx, swag.Item{SKU: "Pri/Mag", Name: `Mini-Magazine "2025"`, Visible: true, MaxPerRequest: 5,
		Description: "A pocket zine about Hack Club.\n\nTwelve pages.", Images: []string{"https://cdn.example/mag.jpg"}})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := h.store.CreateItem(ctx, swag.Item{SKU: "Pri/Secret", Name: "Secret", Visible: false, MaxPerRequest: 1})
	if err != nil {
		t.Fatal(err)
	}

	b := get(t, fmt.Sprintf("%s/items/%d-mini-magazine", h.srv.URL, mag.ID))
	for _, want := range []string{
		`<meta property="og:title" content="Mini-Magazine &#34;2025&#34; · Hack Club Merch" />`,
		`<meta property="og:description" content="A pocket zine about Hack Club. Twelve pages." />`,
		`<meta property="og:image" content="https://cdn.example/mag.jpg" />`,
		`<meta name="twitter:card" content="summary_large_image" />`,
		fmt.Sprintf(`<meta property="og:url" content="http://app.test/items/%d-mini-magazine" />`, mag.ID),
		"app shell",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("product page missing %s in:\n%s", want, b)
		}
	}
	if strings.Contains(b, `content="Default card" />`) {
		t.Errorf("default share tags should be replaced:\n%s", b)
	}

	// Hidden items, unknown ids and other pages keep the site's own card.
	for _, p := range []string{fmt.Sprintf("/items/%d-secret", hidden.ID), "/items/999999-nope", "/items/x", "/", "/requests"} {
		if b := get(t, h.srv.URL+p); !strings.Contains(b, `content="Default card" />`) {
			t.Errorf("%s should keep the default card:\n%s", p, b)
		}
	}
}
