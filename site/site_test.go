package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// buildOnce generates the site into a temp directory and returns its path.
func buildOnce(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "build")
	if _, err := buildSite(out, "/astwerk", false); err != nil {
		t.Fatal(err)
	}
	return out
}

func htmlFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".html") {
			out = append(out, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

var urlAttr = regexp.MustCompile(`(?:href|src)="([^"]+)"`)

// Every internal link must resolve to a real file from the directory of the page
// that emitted it. This is the property that makes one build work at a localhost
// root, under /astwerk/, and opened straight off disk.
func TestInternalLinksResolveFromEachPage(t *testing.T) {
	out := buildOnce(t)
	pages := htmlFiles(t, out)
	if len(pages) < 10 {
		t.Fatalf("only %d pages built; the site is probably not being generated", len(pages))
	}

	for _, page := range pages {
		body, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Dir(page)
		rel, _ := filepath.Rel(out, page)

		for _, m := range urlAttr.FindAllStringSubmatch(string(body), -1) {
			u := m[1]
			if strings.HasPrefix(u, "http") || strings.HasPrefix(u, "#") ||
				strings.HasPrefix(u, "mailto:") || strings.HasPrefix(u, "data:") {
				continue
			}
			if strings.HasPrefix(u, "/") {
				t.Errorf("%s: %q is absolute — use Ctx.Asset or Ctx.Link", rel, u)
				continue
			}

			target := filepath.Clean(filepath.Join(dir, u))
			if !strings.HasPrefix(target, out) {
				t.Errorf("%s: %q escapes the output directory", rel, u)
				continue
			}
			_, errFile := os.Stat(target)
			_, errIndex := os.Stat(filepath.Join(target, "index.html"))
			if errFile != nil && errIndex != nil {
				t.Errorf("%s: %q does not resolve to anything", rel, u)
			}
		}
	}
}

// Canonical URLs are the one place an absolute URL is correct, and they must
// carry the base path or they point at the wrong site.
func TestCanonicalURLsAreAbsoluteAndBased(t *testing.T) {
	out := buildOnce(t)
	canonical := regexp.MustCompile(`<link rel="canonical" href="([^"]+)"`)

	for _, page := range htmlFiles(t, out) {
		body, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(out, page)
		m := canonical.FindSubmatch(body)
		if m == nil {
			t.Errorf("%s: no canonical link", rel)
			continue
		}
		got := string(m[1])
		if !strings.HasPrefix(got, "https://lukasderbaum42.github.io/astwerk/") {
			t.Errorf("%s: canonical %q is missing the origin or base path", rel, got)
		}
	}
}

func TestExpectedPagesExist(t *testing.T) {
	out := buildOnce(t)
	for _, want := range []string{
		"index.html",
		"404.html",
		".nojekyll",
		"docs/index.html",
		"docs/getting-started/index.html",
		"docs/the-node-tree/index.html",
		"examples/index.html",
		"style/style.css",
		"style/code.css",
		"public/favicon.svg",
		"scripts/demo.wasm",
		"scripts/wasm_exec.js",
	} {
		if _, err := os.Stat(filepath.Join(out, want)); err != nil {
			t.Errorf("missing %s", want)
		}
	}
}

// The examples page is hydrated by scripts/demo.go, which finds its markup by
// selector. If a data attribute is renamed in one place and not the other the
// demo silently does nothing, so pin the contract.
func TestExamplesMarkupMatchesDemoSelectors(t *testing.T) {
	out := buildOnce(t)
	body, err := os.ReadFile(filepath.Join(out, "examples", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)

	for _, sel := range []string{
		`id="counter"`, "data-count", "data-inc", "data-dec", "data-parity",
		`id="todo"`, "data-form", "data-new", "data-list", "data-remaining",
		`id="canvas-demo"`, "data-canvas", "data-toggle",
	} {
		if !strings.Contains(html, sel) {
			t.Errorf("examples page is missing %s, which scripts/demo.go hydrates", sel)
		}
	}
}

// Syntax highlighting runs at build time; no JS highlighter should be shipped.
func TestCodeIsHighlightedAtBuildTime(t *testing.T) {
	out := buildOnce(t)
	body, err := os.ReadFile(filepath.Join(out, "docs", "the-node-tree", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)

	if !strings.Contains(html, `class="chroma"`) {
		t.Error("no chroma output — code blocks are not highlighted")
	}
	if n := strings.Count(html, `class="chroma"`); n < 5 {
		t.Errorf("only %d highlighted blocks on the Node tree page", n)
	}
}

// Every docs page needs a Title and Description: the sidebar, index and pager
// are all built from them.
func TestDocsFrontMatterIsComplete(t *testing.T) {
	docs, err := loadDocs("content/docs")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) < 8 {
		t.Fatalf("only %d docs pages loaded", len(docs))
	}

	seen := map[int]string{}
	for _, d := range docs {
		if d.FM.Description == "" {
			t.Errorf("%s.md has no Description", d.Slug)
		}
		if d.FM.Order == 0 {
			t.Errorf("%s.md has no Order, so its sidebar position is arbitrary", d.Slug)
		}
		if other, dup := seen[d.FM.Order]; dup {
			t.Errorf("%s.md and %s.md share Order %d", d.Slug, other, d.FM.Order)
		}
		seen[d.FM.Order] = d.Slug
	}
}

// loadDocs must return pages in Order, since the sidebar and pager rely on it.
func TestDocsAreOrdered(t *testing.T) {
	docs, err := loadDocs("content/docs")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(docs); i++ {
		if docs[i].FM.Order < docs[i-1].FM.Order {
			t.Fatalf("docs out of order: %s (%d) after %s (%d)",
				docs[i].Slug, docs[i].FM.Order, docs[i-1].Slug, docs[i-1].FM.Order)
		}
	}
}

// Every chroma rule must sit inside a prefers-color-scheme media query.
//
// The two chroma styles do not cover the same token classes: github styles .nx
// and .p — plain identifiers and punctuation, most of any Go snippet — as
// near-black, and github-dark has no rule for them. An unscoped light block
// therefore survives into dark mode and renders code at roughly 1.2:1 contrast,
// which is what "dark gray on a dark background" looks like.
func TestCodeCSSIsScopedPerColorScheme(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("style", "code.css"))
	if err != nil {
		t.Fatal(err)
	}

	var depth int
	var leaked []string
	for _, line := range strings.Split(string(css), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "@media"):
			if !strings.Contains(trimmed, "prefers-color-scheme") {
				t.Errorf("unexpected media query in code.css: %s", trimmed)
			}
			depth++
		case trimmed == "}" && depth > 0:
			depth--
		case depth == 0 && strings.Contains(trimmed, ".chroma"):
			leaked = append(leaked, trimmed)
		}
	}
	if len(leaked) > 0 {
		t.Errorf("%d chroma rules sit outside a colour-scheme media query, so they "+
			"leak between light and dark:\n  %s", len(leaked), strings.Join(leaked, "\n  "))
	}

	// Both schemes must actually be present.
	for _, want := range []string{
		"@media (prefers-color-scheme: dark)",
		"@media not all and (prefers-color-scheme: dark)",
	} {
		if !strings.Contains(string(css), want) {
			t.Errorf("code.css is missing %q", want)
		}
	}
}

// The dark block must set a base colour on .chroma, since unstyled tokens
// inherit it rather than having a rule of their own.
func TestDarkCodeHasReadableBaseColor(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("style", "code.css"))
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(css), "@media (prefers-color-scheme: dark)")
	if i < 0 {
		t.Fatal("no dark block")
	}
	dark := string(css)[i:]

	m := regexp.MustCompile(`\.chroma \{([^}]*)\}`).FindStringSubmatch(dark)
	if m == nil {
		t.Fatal("dark block has no .chroma rule")
	}

	// Split into declarations rather than regexping for "color:", which also
	// matches inside "background-color:".
	var base string
	for _, decl := range strings.Split(m[1], ";") {
		prop, value, ok := strings.Cut(decl, ":")
		if ok && strings.TrimSpace(prop) == "color" {
			base = strings.TrimSpace(value)
		}
	}
	if base == "" {
		t.Fatal("dark .chroma sets no base colour; unstyled tokens would inherit the page colour")
	}

	var r, g, b int
	if _, err := fmt.Sscanf(base, "#%02x%02x%02x", &r, &g, &b); err != nil {
		t.Fatalf("unparseable base colour %q: %v", base, err)
	}
	if r+g+b < 3*128 {
		t.Errorf("dark base code colour %s is dark; unstyled tokens like .nx would "+
			"be unreadable on the dark background", base)
	}
}
