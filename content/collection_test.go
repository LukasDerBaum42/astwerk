package content

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LukasDerBaum42/astwerk/ssg"
	"github.com/a-h/templ"
)

// post is the front-matter shape used across the collection tests.
type post struct {
	Title string
	Order int
	Draft bool
}

func writeCollection(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadCollection(t *testing.T) {
	dir := writeCollection(t, map[string]string{
		"website.md":      "+++\nTitle = \"Website\"\nOrder = 2\n+++\nbody a\n",
		"dice-dungeon.md": "+++\nTitle = \"Dice Dungeon\"\nOrder = 1\n+++\nbody b\n",
		"notes.txt":       "ignored",
	})
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "deep.md"), []byte("+++\nTitle = \"Deep\"\n+++\n"), 0644); err != nil {
		t.Fatal(err)
	}

	col, err := LoadCollection[post](dir)
	if err != nil {
		t.Fatal(err)
	}

	// Sorted by slug, non-recursive, *.md only.
	want := []string{"dice-dungeon", "website"}
	if got := slugsOf(col); !reflect.DeepEqual(got, want) {
		t.Fatalf("slugs = %v, want %v", got, want)
	}
	if want := (post{Title: "Dice Dungeon", Order: 1}); !reflect.DeepEqual(col.Items[0].Meta, want) {
		t.Errorf("Meta = %+v, want %+v", col.Items[0].Meta, want)
	}
	if got := col.Items[1].HTML; !strings.Contains(got, "body a") {
		t.Errorf("HTML = %q", got)
	}
}

func TestLoadCollectionTree(t *testing.T) {
	dir := writeCollection(t, map[string]string{
		"hello.md":        "+++\nTitle = \"Hello\"\n+++\nbody\n",
		"2024/first.md":   "+++\nTitle = \"First\"\n+++\nbody\n",
		"2024/03/deep.md": "+++\nTitle = \"Deep\"\n+++\nbody\n",
	})

	col, err := LoadCollectionTree[post](dir)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"2024/03/deep", "2024/first", "hello"}
	if got := slugsOf(col); !reflect.DeepEqual(got, want) {
		t.Fatalf("slugs = %v, want %v", got, want)
	}
}

func TestLoadCollectionMissingDir(t *testing.T) {
	if _, err := LoadCollection[post](filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

func TestLoadCollectionReportsDecodeError(t *testing.T) {
	dir := writeCollection(t, map[string]string{"bad.md": "+++\nTitle = 42\n+++\nbody\n"})
	if _, err := LoadCollection[post](dir); err == nil {
		t.Fatal("expected the front matter type error to propagate")
	}
}

func TestLoadCollectionCustomLoader(t *testing.T) {
	dir := writeCollection(t, map[string]string{
		"a.md": "+++\nTitle = \"A\"\n+++\n~~struck~~\n",
	})
	col, err := LoadCollectionWith[post](NewLoader(nil), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(col.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(col.Items))
	}
}

func TestCollectionSortFilterChunk(t *testing.T) {
	col := Collection[post]{Items: []Item[post]{
		{Slug: "b", Meta: post{Title: "B", Order: 2}},
		{Slug: "a", Meta: post{Title: "A", Order: 1}},
		{Slug: "c", Meta: post{Title: "C", Order: 3, Draft: true}},
	}}

	sorted := col.Sort(func(a, b Item[post]) bool { return a.Meta.Order < b.Meta.Order })
	if got := slugsOf(sorted); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("sorted = %v", got)
	}
	// The original is untouched.
	if got := slugsOf(col); !reflect.DeepEqual(got, []string{"b", "a", "c"}) {
		t.Errorf("original mutated: %v", got)
	}

	filtered := col.Filter(func(it Item[post]) bool { return !it.Meta.Draft })
	if got := slugsOf(filtered); !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Errorf("filtered = %v", got)
	}

	chunks := col.Chunk(2)
	if len(chunks) != 2 || len(chunks[0]) != 2 || len(chunks[1]) != 1 {
		t.Errorf("Chunk(2) = %v", chunks)
	}
	if got := chunks[0][0].Slug; got != "b" {
		t.Errorf("first chunk starts with %q, want b", got)
	}
}

func TestCollectionChunkEmpty(t *testing.T) {
	var col Collection[post]
	if got := col.Chunk(3); got != nil {
		t.Errorf("Chunk on empty collection = %v, want nil", got)
	}
}

func TestCollectionNodes(t *testing.T) {
	col := Collection[post]{Items: []Item[post]{
		{Slug: "hello", Meta: post{Title: "Hello"}},
		{Slug: "2024/first", Meta: post{Title: "First"}},
	}}

	nodes := col.Nodes(func(c ssg.Ctx, it Item[post]) templ.Component {
		return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
			_, err := io.WriteString(w, c.Title+"|"+it.Meta.Title+"|"+it.Slug)
			return err
		})
	})

	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}
	if _, ok := nodes["2024/first"]; !ok {
		t.Errorf("missing nested slug key, got %v", slugsOfKeys(nodes))
	}

	// The page receives the item; Nodes leaves Node.Title empty.
	if got, want := renderNode(t, nodes["hello"]), "|Hello|hello"; got != want {
		t.Errorf("page output = %q, want %q", got, want)
	}
}

func renderNode(t *testing.T, n ssg.Node) string {
	t.Helper()
	var sb strings.Builder
	if err := n.Page(ssg.Ctx{}).Render(context.Background(), &sb); err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

func slugsOf[T any](c Collection[T]) []string {
	out := make([]string, len(c.Items))
	for i, it := range c.Items {
		out[i] = it.Slug
	}
	return out
}

func slugsOfKeys(m map[string]ssg.Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
