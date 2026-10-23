package ssg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/a-h/templ"
)

func TestNodeDataInherited(t *testing.T) {
	out := t.TempDir()
	page := func(label string) PageFunc {
		return func(c Ctx) templ.Component {
			data, ok := DataOf[string](c)
			if !ok {
				return templ.Raw(label + ":missing")
			}
			return templ.Raw(label + ":" + data)
		}
	}

	root := Node{
		Data: "root",
		Page: page("root"),
		Children: map[string]Node{
			"a": {Page: page("a")},
			"b": {Data: "child", Page: page("b")},
			"c": {
				Children: map[string]Node{
					"d": {Page: page("d")},
				},
			},
		},
	}

	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"index.html":     "root:root",
		"a/index.html":   "a:root",
		"b/index.html":   "b:child",
		"c/d/index.html": "d:root",
	}
	for name, want := range cases {
		body, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := string(body); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestDataOfWrongType(t *testing.T) {
	if _, ok := DataOf[int](Ctx{Data: "string"}); ok {
		t.Error("DataOf[int] on a string should be false")
	}
	if _, ok := DataOf[string](Ctx{}); ok {
		t.Error("DataOf on an empty context should be false")
	}
}
