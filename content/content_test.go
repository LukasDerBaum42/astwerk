package content

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type frontMatter struct {
	Title       string
	Description string
	Tags        []string
	Draft       bool
}

const sample = `+++
Title = "Dice Dungeon"
Description = "A roguelike"
Tags = ["game", "go"]
Draft = true
+++

# Heading

Some *markdown* and <span class="raw">inline HTML</span>.
`

func TestParseSplitsFrontMatterAndBody(t *testing.T) {
	p, err := Parse(sample)
	if err != nil {
		t.Fatal(err)
	}

	if p.Meta["Title"] != "Dice Dungeon" {
		t.Errorf("Meta[Title] = %v", p.Meta["Title"])
	}
	if !strings.Contains(p.HTML, "<h1>Heading</h1>") {
		t.Errorf("HTML missing rendered heading:\n%s", p.HTML)
	}
	if !strings.Contains(p.HTML, "<em>markdown</em>") {
		t.Errorf("HTML missing emphasis:\n%s", p.HTML)
	}
	if strings.Contains(p.HTML, "+++") {
		t.Errorf("front matter leaked into HTML:\n%s", p.HTML)
	}
}

func TestParseKeepsRawHTML(t *testing.T) {
	p, err := Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.HTML, `<span class="raw">inline HTML</span>`) {
		t.Errorf("raw HTML was escaped:\n%s", p.HTML)
	}
}

func TestParseWithoutFrontMatter(t *testing.T) {
	p, err := Parse("just *text*\n")
	if err != nil {
		t.Fatal(err)
	}
	if p.Meta != nil {
		t.Errorf("Meta = %v, want nil", p.Meta)
	}
	if !strings.Contains(p.HTML, "<em>text</em>") {
		t.Errorf("HTML = %q", p.HTML)
	}
}

func TestParseUnterminatedFrontMatterIsBody(t *testing.T) {
	p, err := Parse("+++\nTitle = \"x\"\n\nbody text\n")
	if err != nil {
		t.Fatal(err)
	}
	if p.Meta != nil {
		t.Errorf("Meta = %v, want nil", p.Meta)
	}
	if !strings.Contains(p.HTML, "body text") {
		t.Errorf("body was dropped: %q", p.HTML)
	}
}

func TestParseInvalidTOML(t *testing.T) {
	if _, err := Parse("+++\nthis is not toml\n+++\nbody\n"); err == nil {
		t.Fatal("expected an error for malformed front matter")
	}
}

func TestDecode(t *testing.T) {
	p, err := Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	fm, err := Decode[frontMatter](p)
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != "Dice Dungeon" || fm.Description != "A roguelike" || !fm.Draft {
		t.Errorf("fm = %+v", fm)
	}
	if len(fm.Tags) != 2 || fm.Tags[0] != "game" {
		t.Errorf("fm.Tags = %v", fm.Tags)
	}
}

func TestDecodeNoFrontMatter(t *testing.T) {
	p, err := Parse("body only")
	if err != nil {
		t.Fatal(err)
	}
	fm, err := Decode[frontMatter](p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fm, frontMatter{}) {
		t.Errorf("fm = %+v, want zero value", fm)
	}
}

// A Page assembled in code, not parsed, still decodes through Meta.
func TestDecodeHandBuiltPage(t *testing.T) {
	p := Page{Meta: map[string]any{"Title": "Made up", "Draft": false}}
	fm, err := Decode[frontMatter](p)
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != "Made up" {
		t.Errorf("fm = %+v", fm)
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("website.md", "+++\nTitle = \"Website\"\n+++\nbody\n")
	write("dice-dungeon.md", "+++\nTitle = \"Dice Dungeon\"\n+++\nbody\n")
	write("notes.txt", "ignored")
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	write("nested/deep.md", "+++\nTitle = \"Deep\"\n+++\nbody\n")

	pages, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("got %d pages, want 2 (non-recursive, *.md only): %v", len(pages), keys(pages))
	}
	p, ok := pages["dice-dungeon"]
	if !ok {
		t.Fatalf("missing slug key, got %v", keys(pages))
	}
	if p.Meta["Title"] != "Dice Dungeon" {
		t.Errorf("Meta = %v", p.Meta)
	}
}

func TestLoadTree(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("hello.md", "+++\nTitle = \"Hello\"\n+++\nbody\n")
	write("2024/first.md", "+++\nTitle = \"First\"\n+++\nbody\n")
	write("2024/03/deep.md", "+++\nTitle = \"Deep\"\n+++\nbody\n")
	write("2024/index.md", "+++\nTitle = \"Index\"\n+++\nbody\n")
	write("2024/notes.txt", "ignored")

	pages, err := LoadTree(dir)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"2024/03/deep", "2024/first", "2024/index", "hello"}
	if got := Slugs(pages); !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	if got := pages["2024/03/deep"].Meta["Title"]; got != "Deep" {
		t.Errorf("nested page did not parse: Meta[Title] = %v", got)
	}
}

func TestLoadTreeMissing(t *testing.T) {
	if _, err := LoadTree(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

func TestLoadTreeReportsAParseError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.md"), []byte("+++\nTitle = \n+++\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTree(dir); err == nil {
		t.Fatal("expected the front matter error to propagate")
	}
}

func TestChunk(t *testing.T) {
	cases := []struct {
		name  string
		items []int
		size  int
		want  [][]int
	}{
		{"even", []int{1, 2, 3, 4}, 2, [][]int{{1, 2}, {3, 4}}},
		{"remainder", []int{1, 2, 3, 4, 5}, 2, [][]int{{1, 2}, {3, 4}, {5}}},
		{"larger than input", []int{1, 2}, 10, [][]int{{1, 2}}},
		{"one per page", []int{1, 2}, 1, [][]int{{1}, {2}}},
		{"empty", nil, 3, nil},
		{"zero size means one page", []int{1, 2}, 0, [][]int{{1, 2}}},
		{"negative size means one page", []int{1, 2}, -4, [][]int{{1, 2}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Chunk(c.items, c.size); !reflect.DeepEqual(got, c.want) {
				t.Errorf("Chunk(%v, %d) = %v, want %v", c.items, c.size, got, c.want)
			}
		})
	}
}

func TestLoadDirMissing(t *testing.T) {
	if _, err := LoadDir(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

func TestLoadPropagatesFileError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.md")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func keys(m map[string]Page) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestSlugsAreSorted(t *testing.T) {
	pages := map[string]Page{"zebra": {}, "alpha": {}, "middle": {}}
	got := Slugs(pages)
	want := []string{"alpha", "middle", "zebra"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Slugs = %v, want %v", got, want)
	}
	if Slugs(nil) != nil && len(Slugs(nil)) != 0 {
		t.Errorf("Slugs(nil) = %v", Slugs(nil))
	}
}
