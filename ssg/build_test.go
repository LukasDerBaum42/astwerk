package ssg

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
