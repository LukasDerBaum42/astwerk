package ssg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildLocalesMountsSubtrees(t *testing.T) {
	base := Node{
		Page:     text("home"),
		Children: map[string]Node{"about": {Page: text("about")}},
	}

	root := BuildLocales(base, []Locale{
		{Code: "de", Prefix: "/de", Tree: func() Node {
			return Node{Page: text("startseite"), Children: map[string]Node{"about": {Page: text("ueber")}}}
		}},
		{Code: "fr", Prefix: "/fr", Tree: func() Node { return Node{Page: text("accueil")} }},
		{Code: "es", Prefix: "/es"}, // nil Tree: skipped
	})

	out := filepath.Join(t.TempDir(), "build")
	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"about/index.html",
		"de/about/index.html",
		"de/index.html",
		"fr/index.html",
		"index.html",
	}
	if got := tree(t, out); !equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
	if got := read(t, filepath.Join(out, "de", "about", "index.html")); got != "ueber" {
		t.Errorf("de/about = %q", got)
	}
	if _, err := os.Stat(filepath.Join(out, "es")); !os.IsNotExist(err) {
		t.Errorf("locale with a nil Tree was mounted")
	}
}

func TestBuildLocalesDoesNotMutateBase(t *testing.T) {
	base := Node{Children: map[string]Node{"about": {Page: text("about")}}}

	BuildLocales(base, []Locale{
		{Code: "de", Tree: func() Node { return Node{Page: text("de")} }},
	})

	if _, ok := base.Children["de"]; ok {
		t.Errorf("BuildLocales mutated the caller's Children map")
	}
	if len(base.Children) != 1 {
		t.Errorf("base.Children has %d entries, want 1", len(base.Children))
	}
}
