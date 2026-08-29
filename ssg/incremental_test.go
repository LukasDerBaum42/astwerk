package ssg

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/a-h/templ"
)

// countingPage returns a PageFunc that records when the walker invokes it, so a
// test can assert which nodes an incremental build actually re-rendered.
func countingPage(name string, calls *[]string) PageFunc {
	return func(Ctx) templ.Component {
		*calls = append(*calls, name)
		return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, name)
			return err
		})
	}
}

func TestIncrementalRebuildsOnlyChangedDependencies(t *testing.T) {
	src := t.TempDir()
	aPath := filepath.Join(src, "a.md")
	bPath := filepath.Join(src, "b.md")

	var renders []string
	root := Node{Children: map[string]Node{
		"a": {Title: "A", Page: countingPage("a", &renders), DependsOn: []string{aPath}},
		"b": {Title: "B", Page: countingPage("b", &renders), DependsOn: []string{bPath}},
	}}
	out := filepath.Join(t.TempDir(), "build")

	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(renders, []string{"a", "b"}) {
		t.Fatalf("full build rendered %v, want [a b]", renders)
	}

	renders = nil
	if err := Build(root, BuildOptions{OutDir: out, Changed: []string{aPath}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(renders, []string{"a"}) {
		t.Fatalf("incremental build rendered %v, want [a]", renders)
	}
}

func TestIncrementalDoesNotClean(t *testing.T) {
	out := filepath.Join(t.TempDir(), "build")
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(out, "stale.txt")
	if err := os.WriteFile(stale, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	src := t.TempDir()
	root := Node{Page: text("home"), DependsOn: []string{filepath.Join(src, "a.md")}}
	if err := Build(root, BuildOptions{OutDir: out, Changed: []string{filepath.Join(src, "a.md")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Errorf("incremental build removed %s: %v", stale, err)
	}
}

func TestIncrementalDirectoryDependencyMatchesFilesBeneath(t *testing.T) {
	src := t.TempDir()
	dir := filepath.Join(src, "content")

	var rendered bool
	root := Node{
		Page: func(Ctx) templ.Component {
			rendered = true
			return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
				_, err := io.WriteString(w, "home")
				return err
			})
		},
		DependsOn: []string{dir},
	}
	out := filepath.Join(t.TempDir(), "build")

	if err := Build(root, BuildOptions{OutDir: out, Changed: []string{filepath.Join(dir, "posts", "new.md")}}); err != nil {
		t.Fatal(err)
	}
	if !rendered {
		t.Errorf("a directory dependency did not match a file beneath it")
	}
}

func TestIncrementalSkipsNodesWithoutDependencies(t *testing.T) {
	var rendered bool
	root := Node{
		Page: func(Ctx) templ.Component {
			rendered = true
			return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
				_, err := io.WriteString(w, "home")
				return err
			})
		},
		// no DependsOn
	}
	out := filepath.Join(t.TempDir(), "build")

	if err := Build(root, BuildOptions{OutDir: out, Changed: []string{filepath.Join(t.TempDir(), "x.md")}}); err != nil {
		t.Fatal(err)
	}
	if rendered {
		t.Errorf("a node without DependsOn was rebuilt on an incremental build")
	}
}

func TestIncrementalGenerateRunsOnlyWhenAffected(t *testing.T) {
	src := t.TempDir()
	postsDir := filepath.Join(src, "posts")
	if err := os.MkdirAll(postsDir, 0755); err != nil {
		t.Fatal(err)
	}

	var generateCalls int
	root := Node{
		Children: map[string]Node{
			"static": {Page: text("static")}, // no dependencies
		},
		Generate: func() map[string]Node {
			generateCalls++
			return map[string]Node{
				"post": {Page: text("post"), DependsOn: []string{filepath.Join(postsDir, "post.md")}},
			}
		},
		DependsOn: []string{postsDir},
	}
	out := filepath.Join(t.TempDir(), "build")

	// Full build discovers the generated child and renders it.
	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}
	if generateCalls != 1 {
		t.Fatalf("full build ran Generate %d times, want 1", generateCalls)
	}
	if _, err := os.Stat(filepath.Join(out, "post", "index.html")); err != nil {
		t.Fatalf("generated page missing: %v", err)
	}

	// An unrelated change does not touch the root, so Generate must not run.
	generateCalls = 0
	if err := Build(root, BuildOptions{OutDir: out, Changed: []string{filepath.Join(t.TempDir(), "unrelated.md")}}); err != nil {
		t.Fatal(err)
	}
	if generateCalls != 0 {
		t.Errorf("unrelated change ran Generate %d times, want 0", generateCalls)
	}

	// A change beneath the declared directory re-runs Generate.
	generateCalls = 0
	if err := Build(root, BuildOptions{OutDir: out, Changed: []string{filepath.Join(postsDir, "new.md")}}); err != nil {
		t.Fatal(err)
	}
	if generateCalls != 1 {
		t.Errorf("matching change ran Generate %d times, want 1", generateCalls)
	}
}
