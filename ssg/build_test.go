package ssg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/a-h/templ"
)

// text is a minimal templ.Component so these tests don't need generated code.
func text(s string) PageFunc {
	return func(Ctx) templ.Component {
		return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, s)
			return err
		})
	}
}

// ctxText renders the Ctx the walker handed the page, so tests can assert on it.
func ctxText() PageFunc {
	return func(c Ctx) templ.Component {
		return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, err := fmt.Fprintf(w, "title=%q path=%q prefix=%q locale=%q url=%q",
				c.Title, c.Path, c.Prefix, c.Locale, c.URL())
			return err
		})
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// tree lists every file under dir, relative and slash-separated.
func tree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(out)
	return out
}

func TestBuildWritesPagesAndChildren(t *testing.T) {
	out := filepath.Join(t.TempDir(), "build")

	root := Node{
		Page: text("home"),
		Children: map[string]Node{
			"about": {Page: text("about")},
			"projects": {
				Page:     text("projects index"),
				Children: map[string]Node{"static": {Page: text("static child")}},
			},
			"empty": {}, // zero value: nothing to do, no directory
		},
	}

	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}

	want := []string{"about/index.html", "index.html", "projects/index.html", "projects/static/index.html"}
	if got := tree(t, out); !equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
	if got := read(t, filepath.Join(out, "projects", "index.html")); got != "projects index" {
		t.Errorf("projects/index.html = %q", got)
	}
	if _, err := os.Stat(filepath.Join(out, "empty")); !os.IsNotExist(err) {
		t.Errorf("node with nothing to do created a directory")
	}
}

func TestBuildFiles(t *testing.T) {
	out := filepath.Join(t.TempDir(), "build")

	root := Node{
		Page: text("home"),
		Files: map[string]PageFunc{
			"404.html":     text("not found"),
			"feed/rss.xml": text("<rss/>"),
		},
	}
	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}

	want := []string{"404.html", "feed/rss.xml", "index.html"}
	if got := tree(t, out); !equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
	if got := read(t, filepath.Join(out, "404.html")); got != "not found" {
		t.Errorf("404.html = %q", got)
	}
}

func TestBuildGenerateMergesAndWins(t *testing.T) {
	out := filepath.Join(t.TempDir(), "build")

	root := Node{
		Children: map[string]Node{
			"projects": {
				Page: text("index"),
				Children: map[string]Node{
					"static":    {Page: text("static")},
					"collision": {Page: text("from Children")},
				},
				Generate: func() map[string]Node {
					return map[string]Node{
						"generated": {Page: text("generated")},
						"collision": {Page: text("from Generate")},
					}
				},
			},
		},
	}
	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"projects/collision/index.html",
		"projects/generated/index.html",
		"projects/index.html",
		"projects/static/index.html",
	}
	if got := tree(t, out); !equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
	if got := read(t, filepath.Join(out, "projects", "collision", "index.html")); got != "from Generate" {
		t.Errorf("collision resolved to %q, want generated key to win", got)
	}
}

func TestBuildCopyFromAndNestedKeys(t *testing.T) {
	dir := t.TempDir()
	style := filepath.Join(dir, "style")
	if err := os.MkdirAll(filepath.Join(style, "vendor"), 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(style, "site.css"), []byte("body{}"), 0644)
	os.WriteFile(filepath.Join(style, "vendor", "reset.css"), []byte("*{}"), 0644)

	out := filepath.Join(dir, "build")
	root := Node{Children: map[string]Node{
		"assets/style": {CopyFrom: style}, // slash in a key nests
	}}
	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}

	want := []string{"assets/style/site.css", "assets/style/vendor/reset.css"}
	if got := tree(t, out); !equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}

func TestBuildCleansUnlessDev(t *testing.T) {
	out := filepath.Join(t.TempDir(), "build")
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(out, "stale.txt")
	os.WriteFile(stale, []byte("old"), 0644)

	root := Node{Page: text("home")}

	if err := Build(root, BuildOptions{OutDir: out, Dev: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Errorf("dev build removed %s: %v", stale, err)
	}

	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("non-dev build kept %s", stale)
	}
}

func TestBuildDefaultOutDir(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := Build(Node{Page: text("home")}, BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(DefaultOutDir, "index.html")); got != "home" {
		t.Errorf("build/index.html = %q", got)
	}
}

func TestBuildRejectsEscapingKeys(t *testing.T) {
	for _, key := range []string{"..", "../evil", "/etc", ""} {
		out := filepath.Join(t.TempDir(), "build")
		root := Node{Children: map[string]Node{key: {Page: text("x")}}}
		err := Build(root, BuildOptions{OutDir: out})
		if err == nil {
			t.Errorf("key %q: expected an error", key)
		}
	}
}

func TestBuildAcceptsTrailingSlashKeys(t *testing.T) {
	out := filepath.Join(t.TempDir(), "build")
	root := Node{Children: map[string]Node{"projects/": {Page: text("p")}}}
	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(out, "projects", "index.html")); got != "p" {
		t.Errorf("projects/index.html = %q", got)
	}
}

func TestBuildPropagatesRenderError(t *testing.T) {
	out := filepath.Join(t.TempDir(), "build")
	boom := func(Ctx) templ.Component {
		return templ.ComponentFunc(func(context.Context, io.Writer) error {
			return io.ErrClosedPipe
		})
	}
	err := Build(Node{Children: map[string]Node{"a": {Page: boom}}}, BuildOptions{OutDir: out})
	if err == nil || !strings.Contains(err.Error(), "render") {
		t.Fatalf("err = %v, want a render error", err)
	}
}

// parallelSite is wide and deep enough that a race, if there is one, has room
// to happen: sibling pages, extra files, a generated collection, and a locale
// subtree derived from all of it.
func parallelSite() Node {
	base := Node{
		Title: "Site",
		Page:  ctxText(),
		Files: map[string]PageFunc{"404.html": text("gone"), "feed/rss.xml": text("<rss/>")},
		Children: map[string]Node{
			"about": {Title: "About", Page: ctxText()},
			"goals": {Title: "Goals", Page: ctxText()},
			"projects": {
				Title: "Projects",
				Page:  ctxText(),
				Generate: func() map[string]Node {
					out := map[string]Node{}
					for i := range 12 {
						name := fmt.Sprintf("p%02d", i)
						out[name] = Node{Title: name, Page: ctxText()}
					}
					return out
				},
			},
		},
	}
	return BuildLocales(base, []Locale{{Code: "de"}, {Code: "fr"}})
}

func TestBuildParallelMatchesSequential(t *testing.T) {
	dir := t.TempDir()
	seq, par := filepath.Join(dir, "seq"), filepath.Join(dir, "par")

	if err := Build(parallelSite(), BuildOptions{OutDir: seq}); err != nil {
		t.Fatal(err)
	}
	if err := Build(parallelSite(), BuildOptions{OutDir: par, Parallel: true}); err != nil {
		t.Fatal(err)
	}

	files := tree(t, seq)
	if got := tree(t, par); !equal(got, files) {
		t.Fatalf("parallel build wrote\n%v\nwant\n%v", got, files)
	}
	for _, f := range files {
		if a, b := read(t, filepath.Join(seq, f)), read(t, filepath.Join(par, f)); a != b {
			t.Errorf("%s differs:\n sequential %q\n parallel   %q", f, a, b)
		}
	}
}

// Which failure surfaces must not depend on which goroutine got there first.
func TestBuildParallelReportsTheSameErrorFirst(t *testing.T) {
	boom := func(msg string) PageFunc {
		return func(Ctx) templ.Component {
			return templ.ComponentFunc(func(context.Context, io.Writer) error {
				return errors.New(msg)
			})
		}
	}
	root := Node{Children: map[string]Node{
		"a": {Page: boom("first")},
		"b": {Page: boom("second")},
		"c": {Page: boom("third")},
	}}

	for i := range 20 {
		out := filepath.Join(t.TempDir(), "build")
		err := Build(root, BuildOptions{OutDir: out, Parallel: true})
		if err == nil || !strings.Contains(err.Error(), "first") {
			t.Fatalf("run %d: err = %v, want the error from child \"a\"", i, err)
		}
	}
}

func TestInParallelRunsEveryIndexOnce(t *testing.T) {
	const n = 64
	var mu sync.Mutex
	seen := make([]int, n)

	if err := inParallel(n, func(i int) error {
		mu.Lock()
		defer mu.Unlock()
		seen[i]++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i, c := range seen {
		if c != 1 {
			t.Errorf("index %d ran %d times, want 1", i, c)
		}
	}
	if err := inParallel(0, func(int) error { return errors.New("never") }); err != nil {
		t.Errorf("inParallel(0) = %v, want nil", err)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCtxURLHelpers(t *testing.T) {
	cases := []struct {
		name             string
		ctx              Ctx
		url, asset, link string
	}{
		{"root", Ctx{Path: "about/"}, "/about/", "/style/x.css", "/docs/"},
		{"home at root", Ctx{Path: ""}, "/", "/style/x.css", "/docs/"},
		{"base path", Ctx{Path: "about/", Base: "/astwerk"},
			"/astwerk/about/", "/astwerk/style/x.css", "/astwerk/docs/"},
		{"locale", Ctx{Path: "de/about/", Prefix: "/de"},
			"/de/about/", "/style/x.css", "/de/docs/"},
		{"base and locale", Ctx{Path: "de/about/", Prefix: "/de", Base: "/astwerk"},
			"/astwerk/de/about/", "/astwerk/style/x.css", "/astwerk/de/docs/"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.ctx.URL(); got != c.url {
				t.Errorf("URL() = %q, want %q", got, c.url)
			}
			// A leading slash on the argument must not double up.
			for _, arg := range []string{"style/x.css", "/style/x.css"} {
				if got := c.ctx.Asset(arg); got != c.asset {
					t.Errorf("Asset(%q) = %q, want %q", arg, got, c.asset)
				}
			}
			if got := c.ctx.Link("docs/"); got != c.link {
				t.Errorf("Link() = %q, want %q", got, c.link)
			}
		})
	}
}

func TestBaseURLNormalization(t *testing.T) {
	for _, in := range []string{"astwerk", "/astwerk", "astwerk/", "/astwerk/"} {
		if got := (BuildOptions{BaseURL: in}).normalizeBase(); got != "/astwerk" {
			t.Errorf("normalizeBase(%q) = %q, want /astwerk", in, got)
		}
	}
	if got := (BuildOptions{}).normalizeBase(); got != "" {
		t.Errorf("empty BaseURL normalized to %q", got)
	}
}

func TestBuildThreadsBaseURLToPages(t *testing.T) {
	out := filepath.Join(t.TempDir(), "build")
	root := Node{Page: ctxText(), Children: map[string]Node{"docs": {Page: ctxText()}}}

	if err := Build(root, BuildOptions{OutDir: out, BaseURL: "astwerk/"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(out, "docs", "index.html")); !strings.Contains(got, `url="/astwerk/docs/"`) {
		t.Errorf("page got %s, want a base-prefixed URL", got)
	}
}

func TestCtxRelAndInLocale(t *testing.T) {
	cases := []struct {
		name          string
		ctx           Ctx
		rel, base, de string
	}{
		{"base tree", Ctx{Path: "about/"}, "about/", "/about/", "/de/about/"},
		{"locale subtree", Ctx{Path: "de/about/", Prefix: "/de", Locale: "de"},
			"about/", "/about/", "/de/about/"},
		{"home in locale", Ctx{Path: "de/", Prefix: "/de", Locale: "de"},
			"", "/", "/de/"},
		{"nested in locale", Ctx{Path: "de/projects/thing/", Prefix: "/de", Locale: "de"},
			"projects/thing/", "/projects/thing/", "/de/projects/thing/"},
		{"with base path", Ctx{Path: "de/about/", Prefix: "/de", Locale: "de", Base: "/astwerk"},
			"about/", "/astwerk/about/", "/astwerk/de/about/"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.ctx.Rel(); got != c.rel {
				t.Errorf("Rel() = %q, want %q", got, c.rel)
			}
			if got := c.ctx.InLocale(""); got != c.base {
				t.Errorf("InLocale(\"\") = %q, want %q", got, c.base)
			}
			if got := c.ctx.InLocale("/de"); got != c.de {
				t.Errorf("InLocale(\"/de\") = %q, want %q", got, c.de)
			}
		})
	}
}

// The switcher must round-trip: from a locale page, InLocale of the same locale
// gives back the page's own URL.
func TestInLocaleRoundTrips(t *testing.T) {
	c := Ctx{Path: "de/projects/thing/", Prefix: "/de", Locale: "de", Base: "/astwerk"}
	if got, want := c.InLocale(c.Prefix), c.URL(); got != want {
		t.Errorf("InLocale(own prefix) = %q, want %q", got, want)
	}
}

func TestCtxRelativeURLs(t *testing.T) {
	cases := []struct {
		name             string
		ctx              Ctx
		asset, linkAbout string
	}{
		{"root", Ctx{Path: "", Relative: true}, "style/x.css", "about/"},
		{"one deep", Ctx{Path: "docs/", Relative: true}, "../style/x.css", "../about/"},
		{"two deep", Ctx{Path: "docs/intro/", Relative: true}, "../../style/x.css", "../../about/"},
		{"three deep", Ctx{Path: "a/b/c/", Relative: true}, "../../../style/x.css", "../../../about/"},
		{"locale one deep", Ctx{Path: "de/", Prefix: "/de", Locale: "de", Relative: true},
			"../style/x.css", "../de/about/"},
		{"locale two deep", Ctx{Path: "de/docs/", Prefix: "/de", Locale: "de", Relative: true},
			"../../style/x.css", "../../de/about/"},
		// Base is ignored for in-page URLs once relative is on.
		{"base ignored", Ctx{Path: "docs/", Base: "/astwerk", Relative: true},
			"../style/x.css", "../about/"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.ctx.Asset("style/x.css"); got != c.asset {
				t.Errorf("Asset() = %q, want %q", got, c.asset)
			}
			if got := c.ctx.Link("about/"); got != c.linkAbout {
				t.Errorf("Link() = %q, want %q", got, c.linkAbout)
			}
		})
	}
}

// The root page linking to itself must not produce an empty href.
func TestRelativeSelfLinkIsDot(t *testing.T) {
	c := Ctx{Path: "", Relative: true}
	if got := c.Link(""); got != "./" {
		t.Errorf("Link(\"\") at the root = %q, want ./", got)
	}
	if got := (Ctx{Path: "docs/", Relative: true}).Link(""); got != "../" {
		t.Errorf("Link(\"\") one deep = %q, want ../", got)
	}
}

// URL stays absolute even in relative mode — canonical tags need it.
func TestURLIgnoresRelative(t *testing.T) {
	c := Ctx{Path: "docs/intro/", Base: "/astwerk", Relative: true}
	if got := c.URL(); got != "/astwerk/docs/intro/" {
		t.Errorf("URL() = %q, want the absolute form", got)
	}
}

func TestInLocaleRelative(t *testing.T) {
	c := Ctx{Path: "de/docs/", Prefix: "/de", Locale: "de", Relative: true}
	if got := c.InLocale(""); got != "../../docs/" {
		t.Errorf("InLocale(\"\") = %q, want ../../docs/", got)
	}
	if got := c.InLocale("/de"); got != "../../de/docs/" {
		t.Errorf("InLocale(\"/de\") = %q, want ../../de/docs/", got)
	}
}

func TestDepth(t *testing.T) {
	for path, want := range map[string]int{
		"": 0, "/": 0, "docs/": 1, "docs": 1,
		"docs/intro/": 2, "a/b/c/": 3, "a/b/c/d/": 4,
	} {
		if got := (Ctx{Path: path}).depth(); got != want {
			t.Errorf("depth(%q) = %d, want %d", path, got, want)
		}
	}
}

// A relative build must resolve to real files from each page's own directory —
// this is the property that makes the output portable.
func TestRelativeBuildLinksResolveFromDisk(t *testing.T) {
	out := filepath.Join(t.TempDir(), "build")

	linkTo := func(target string) PageFunc {
		return func(c Ctx) templ.Component {
			return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
				_, err := fmt.Fprintf(w, `<a href="%s">x</a><link href="%s">`,
					c.Link(target), c.Asset("style/site.css"))
				return err
			})
		}
	}

	root := Node{
		Page:  linkTo("docs/intro/"),
		Files: map[string]PageFunc{"style/site.css": text("body{}")},
		Children: map[string]Node{
			"docs": {
				Page:     linkTo(""),
				Children: map[string]Node{"intro": {Page: linkTo("docs/")}},
			},
		},
	}
	if err := Build(root, BuildOptions{OutDir: out, RelativeURLs: true, BaseURL: "/ignored"}); err != nil {
		t.Fatal(err)
	}

	// Resolve each page's hrefs against its own directory, as a browser would.
	pages := []string{"index.html", "docs/index.html", "docs/intro/index.html"}
	for _, page := range pages {
		html := read(t, filepath.Join(out, page))
		dir := filepath.Dir(filepath.Join(out, page))
		for _, m := range regexp.MustCompile(`(?:href)="([^"]+)"`).FindAllStringSubmatch(html, -1) {
			target := filepath.Clean(filepath.Join(dir, m[1]))
			if !strings.HasPrefix(target, out) {
				t.Errorf("%s: %q escapes the output directory", page, m[1])
				continue
			}
			_, errFile := os.Stat(target)
			_, errIndex := os.Stat(filepath.Join(target, "index.html"))
			if errFile != nil && errIndex != nil {
				t.Errorf("%s: %q resolves to %s, which does not exist", page, m[1], target)
			}
		}
	}
}
