package httpapi

// Release gates for usage analytics (AGENTS.md, "Usage analytics"). They run
// in `make test`, so nothing lands that people can do on the site without it
// being logged, or that logs events no agent can find out about.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// untracked lists the actions that don't record their own event, and why.
// Every call is still logged as an `api` event (route, status, time taken).
// Don't add to this to get a build through; track the action instead.
var untracked = map[string]string{
	"POST /api/events": "it is the tracker",
}

// TestEveryActionIsTracked: every route that changes something must call
// s.track (directly or through a server method it calls) with what happened.
func TestEveryActionIsTracked(t *testing.T) {
	fset := token.NewFileSet()
	methods := map[string]*ast.FuncDecl{}
	var newFn *ast.FuncDecl
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Recv != nil && len(fn.Recv.List) == 1 {
				if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok && star.X.(*ast.Ident).Name == "server" {
					methods[fn.Name.Name] = fn
				}
			} else if fn.Name.Name == "New" {
				newFn = fn
			}
		}
	}
	if newFn == nil {
		t.Fatal("no New in package httpapi")
	}

	// tracks reports whether a method calls s.track, itself or through other methods.
	var tracks func(name string, seen map[string]bool) bool
	tracks = func(name string, seen map[string]bool) bool {
		fn, ok := methods[name]
		if !ok || seen[name] {
			return false
		}
		seen[name] = true
		found := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "s" && (sel.Sel.Name == "track" || tracks(sel.Sel.Name, seen)) {
						found = true
					}
				}
			}
			return !found
		})
		return found
	}

	routes := 0
	ast.Inspect(newFn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			return true
		}
		pattern, _ := strconv.Unquote(lit.Value)
		method, _, _ := strings.Cut(pattern, " ")
		if !slices.Contains([]string{"POST", "PUT", "PATCH", "DELETE"}, method) {
			return true
		}
		routes++
		// Admin actions count too: how fast requests ship and when the catalog changed is evidence.
		if _, ok := untracked[pattern]; ok {
			return true
		}
		// The handler is the s.<method> among the arguments (maybe inside s.requireUser(…)).
		var handler string
		ast.Inspect(call.Args[1], func(n ast.Node) bool {
			if s, ok := n.(*ast.SelectorExpr); ok {
				if _, isMethod := methods[s.Sel.Name]; isMethod && !strings.HasPrefix(s.Sel.Name, "require") {
					handler = s.Sel.Name
				}
			}
			return true
		})
		if !tracks(handler, map[string]bool{}) {
			t.Errorf("%s (s.%s) records no usage event: call s.track with what happened (AGENTS.md, \"Usage analytics\")", pattern, handler)
		}
		return true
	})
	if routes < 5 {
		t.Fatalf("found only %d mutating routes; is the route parsing broken?", routes)
	}
}

var (
	goEvent  = regexp.MustCompile(`s\.track(?:Request|Admin)?\(r, [^,]+, "([a-z0-9_]+)"`)
	webEvent = regexp.MustCompile(`\btrack\(\s*'([a-z0-9_]+)'`)
)

// TestEventsAreDocumented: every event the code records is listed in AGENTS.md,
// which is how agents find out what they can query.
func TestEventsAreDocumented(t *testing.T) {
	names := map[string]string{} // event -> where it's recorded
	scan := func(root string, re *regexp.Regexp, exts ...string) {
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.Contains(p, "_test.go") || strings.Contains(p, ".test.ts") || !slices.Contains(exts, filepath.Ext(p)) {
				return err
			}
			b, err := os.ReadFile(p)
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				names[m[1]] = p
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	scan("../../internal", goEvent, ".go")
	scan("../../web/src", webEvent, ".ts", ".svelte")
	for _, must := range []string{"api", "shipping_estimate", "page_view", "cart", "click"} {
		if names[must] == "" {
			t.Fatalf("didn't find the %q event in the code; is the scan broken? found %v", must, names)
		}
	}
	doc, err := os.ReadFile("../../AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, _ := strings.Cut(string(doc), "## Usage analytics\n")
	section, _, _ = strings.Cut(section, "\n## ")
	for name, where := range names {
		if !eventName.MatchString(name) {
			t.Errorf("event %q (%s) isn't a valid name (%s)", name, where, eventName)
		}
		if !strings.Contains(section, "`"+name+"`") {
			t.Errorf("event `%s` (recorded in %s) isn't listed in AGENTS.md's \"Usage analytics\"", name, where)
		}
	}
}
