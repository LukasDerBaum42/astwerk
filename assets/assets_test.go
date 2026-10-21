package assets

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type meta struct {
	Credit  string   `json:"credit" toml:"credit"`
	License string   `json:"license" toml:"license"`
	Tags    []string `json:"tags" toml:"tags"`
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func paths[T any](entries []Entry[T]) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.File.Path
	}
	return out
}

func TestLoadTree(t *testing.T) {
	root := writeTree(t, map[string]string{
		"img/meta.json": `{
			"a.png": {"credit": "anzeal", "license": "CC-BY", "tags": ["logo"]},
			"b.jpg": {"credit": "ollie"}
		}`,
		"img/a.png":         "a",
		"img/b.jpg":         "b",
		"img/notes.txt":     "no metadata",
		"img/sub/meta.toml": "[\"c.png\"]\ncredit = \"viann\"\nlicense = \"L2\"\n",
		"img/sub/c.png":     "c",
		"img/sub/d.png":     "d",
	})

	entries, err := LoadTree[meta](root, Options[meta]{})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"img/a.png", "img/b.jpg", "img/notes.txt", "img/sub/c.png", "img/sub/d.png"}
	if got := paths(entries); !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}

	if got := entries[0].Meta; !reflect.DeepEqual(got, meta{Credit: "anzeal", License: "CC-BY", Tags: []string{"logo"}}) {
		t.Errorf("a.png meta = %+v", got)
	}
	if !entries[0].HasMeta {
		t.Error("a.png has no metadata")
	}
	if entries[2].HasMeta {
		t.Error("notes.txt should not have metadata")
	}
	if got := entries[3].Meta; got.Credit != "viann" || got.License != "L2" {
		t.Errorf("c.png meta = %+v", got)
	}
	if got := entries[0].File.Ext; got != ".png" {
		t.Errorf("Ext = %q", got)
	}
	if entries[0].File.Size == 0 {
		t.Error("Size is zero")
	}
}

func TestRequireMeta(t *testing.T) {
	root := writeTree(t, map[string]string{
		"meta.json": `{"a.png": {"credit": "x"}}`,
		"a.png":     "a",
		"b.png":     "b",
	})

	entries, err := LoadTree[meta](root, Options[meta]{RequireMeta: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(entries); !reflect.DeepEqual(got, []string{"a.png"}) {
		t.Errorf("paths = %v", got)
	}
}

func TestIncludeAndSkipDirs(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.png":          "a",
		"b.jpg":          "b",
		".git/meta.json": "{}",
		".git/c.png":     "c",
	})

	entries, err := LoadTree[meta](root, Options[meta]{
		Include:  []string{"*.png"},
		SkipDirs: []string{".git"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(entries); !reflect.DeepEqual(got, []string{"a.png"}) {
		t.Errorf("paths = %v", got)
	}
}

func TestLoadDirNonRecursive(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.png":     "a",
		"sub/b.png": "b",
	})

	entries, err := LoadDir[meta](root, Options[meta]{})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(entries); !reflect.DeepEqual(got, []string{"a.png"}) {
		t.Errorf("paths = %v", got)
	}
}

func TestBothSidecars(t *testing.T) {
	root := writeTree(t, map[string]string{
		"meta.json": "{}",
		"meta.toml": "",
		"a.png":     "a",
	})
	if _, err := LoadTree[meta](root, Options[meta]{}); err == nil {
		t.Fatal("expected an error for two sidecars")
	}
}

func TestCustomSidecar(t *testing.T) {
	root := writeTree(t, map[string]string{
		"info.json": `{"a.png": {"credit": "x"}}`,
		"a.png":     "a",
	})
	entries, err := LoadTree[meta](root, Options[meta]{SidecarName: "info"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Meta.Credit != "x" {
		t.Errorf("entries = %+v", entries)
	}
}

// A custom Sidecar lets a project use a per-directory object instead of a map
// keyed by file name, or any other layout, without changing the package.
func TestCustomSidecarFunc(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.png": "a",
		"b.png": "b",
	})
	opts := Options[meta]{
		Sidecar: func(dir string) (map[string]meta, error) {
			value := meta{Credit: "per-directory"}
			return map[string]meta{"a.png": value, "b.png": value}, nil
		},
	}
	entries, err := LoadTree[meta](root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Meta.Credit != "per-directory" {
		t.Errorf("entries = %+v", entries)
	}
}

func TestReadSidecar(t *testing.T) {
	root := writeTree(t, map[string]string{
		"credits/a.json": `{"a.png": {"credit": "outside"}}`,
		"a.png":          "a",
	})
	opts := Options[meta]{
		SkipDirs: []string{"credits"},
		Sidecar: func(dir string) (map[string]meta, error) {
			return ReadSidecar[meta](filepath.Join(root, "credits", "a.json"))
		},
	}
	entries, err := LoadTree[meta](root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Meta.Credit != "outside" {
		t.Errorf("entries = %+v", entries)
	}
}

func TestWalk(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.png":     "a",
		"sub/b.png": "b",
	})
	var dirs []string
	err := Walk(root, WalkOptions{Include: []string{"*.png"}}, func(dir string, files []File) error {
		dirs = append(dirs, filepath.Base(dir)+":"+strings.Join(pathsOf(files), ","))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Base(root) + ":a.png", "sub:b.png"}
	if !reflect.DeepEqual(dirs, want) {
		t.Errorf("dirs = %v, want %v", dirs, want)
	}
}

func pathsOf(files []File) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Name
	}
	return out
}
