package ssg

import (
	"os"
	"path/filepath"
	"testing"
)

// baseSite is the shape a real site has: pages, a generated collection, and
// asset nodes that must not be duplicated per locale.
func baseSite() Node {
	return Node{
		Title: "My Site",
		Page:  ctxText(),
		Children: map[string]Node{
			"about": {Title: "About", Page: ctxText()},
			"goals": {Title: "Goals", Page: ctxText()},
			"projects": {
				Title: "Projects",
				Page:  ctxText(),
				Generate: func() map[string]Node {
					return map[string]Node{"alpha": {Title: "Alpha", Page: ctxText()}}
				},
			},
			"style":   {CopyFrom: "style"},
			"scripts": {CompileFrom: "scripts"},
		},
	}
}

func TestBuildLocalesDerivesSubtreeFromBase(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "style"), 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "style", "site.css"), []byte("body{}"), 0644)
	t.Chdir(dir)

	base := baseSite()
	base.Children["scripts"] = Node{} // no scripts dir in this fixture

	root := BuildLocales(base, []Locale{{Code: "de"}})

	out := filepath.Join(dir, "build")
	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}

	// Every page is mirrored under de/, including the generated collection —
	// without a hand-written German tree.
	want := []string{
		"about/index.html",
		"de/about/index.html",
		"de/goals/index.html",
		"de/index.html",
		"de/projects/alpha/index.html",
		"de/projects/index.html",
		"goals/index.html",
		"index.html",
		"projects/alpha/index.html",
		"projects/index.html",
		"style/site.css",
	}
	if got := tree(t, out); !equal(got, want) {
		t.Fatalf("files =\n%v\nwant\n%v", got, want)
	}
}

func TestBuildLocalesDoesNotDuplicateAssets(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "style"), 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "style", "site.css"), []byte("body{}"), 0644)
	t.Chdir(dir)

	base := Node{
		Page: text("home"),
		Children: map[string]Node{
			"style": {CopyFrom: "style"},
		},
	}
	root := BuildLocales(base, []Locale{{Code: "de"}})

	out := filepath.Join(dir, "build")
	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "de", "style")); !os.IsNotExist(err) {
		t.Errorf("locale subtree duplicated the style/ assets")
	}
}

func TestLocaleCtxIsThreadedToPages(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	base := Node{
		Title:    "My Site",
		Page:     ctxText(),
		Children: map[string]Node{"about": {Title: "About", Page: ctxText()}},
	}
	root := BuildLocales(base, []Locale{{Code: "de", Prefix: "/de"}})

	out := filepath.Join(dir, "build")
	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"index.html":          `title="My Site" path="" prefix="" locale="" url="/"`,
		"about/index.html":    `title="About" path="about/" prefix="" locale="" url="/about/"`,
		"de/index.html":       `title="My Site" path="de/" prefix="/de" locale="de" url="/de/"`,
		"de/about/index.html": `title="About" path="de/about/" prefix="/de" locale="de" url="/de/about/"`,
	}
	for file, want := range cases {
		if got := read(t, filepath.Join(out, file)); got != want {
			t.Errorf("%s =\n  %s\nwant\n  %s", file, got, want)
		}
	}
}

func TestLocalePrefixDefaultsToCode(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	root := BuildLocales(Node{Page: ctxText()}, []Locale{{Code: "fr"}})
	out := filepath.Join(dir, "build")
	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}
	want := `title="" path="fr/" prefix="/fr" locale="fr" url="/fr/"`
	if got := read(t, filepath.Join(out, "fr", "index.html")); got != want {
		t.Errorf("fr/index.html = %s, want %s", got, want)
	}
}

func TestLocaleOverride(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	base := Node{
		Title: "My Site",
		Page:  text("english home"),
		Children: map[string]Node{
			"about": {Title: "About", Page: text("english about")},
			"goals": {Title: "Goals", Page: text("english goals")},
		},
	}

	root := BuildLocales(base, []Locale{{
		Code: "de",
		Override: map[string]func(Node) Node{
			"": func(n Node) Node {
				n.Title, n.Page = "Meine Seite", text("german home")
				return n
			},
			"about": func(n Node) Node {
				n.Title, n.Page = "Über mich", text("german about")
				return n
			},
			// No base counterpart: n is the zero Node.
			"neu": func(n Node) Node {
				n.Title, n.Page = "Neu", text("german only")
				return n
			},
		},
	}})

	out := filepath.Join(dir, "build")
	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"index.html":          "english home",
		"about/index.html":    "english about",
		"de/index.html":       "german home",
		"de/about/index.html": "german about",
		"de/goals/index.html": "english goals", // inherited, not overridden
		"de/neu/index.html":   "german only",   // locale-only page
	}
	for file, want := range cases {
		if got := read(t, filepath.Join(out, file)); got != want {
			t.Errorf("%s = %q, want %q", file, got, want)
		}
	}
}

// Overriding the locale's root must not wipe out the inherited children.
func TestLocaleRootOverrideKeepsChildren(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	base := Node{
		Page:     text("english home"),
		Children: map[string]Node{"about": {Page: text("english about")}},
	}
	root := BuildLocales(base, []Locale{{
		Code: "de",
		Override: map[string]func(Node) Node{
			"": func(n Node) Node { n.Page = text("german home"); return n },
		},
	}})

	out := filepath.Join(dir, "build")
	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(out, "de", "index.html")); got != "german home" {
		t.Errorf("de/index.html = %q", got)
	}
	if got := read(t, filepath.Join(out, "de", "about", "index.html")); got != "english about" {
		t.Errorf("de/about/index.html = %q, want the inherited page", got)
	}
}

// The point of the func form: touching one field leaves everything else — the
// page, the generated children — exactly as derived.
func TestLocaleOverridePatchesOneField(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	base := Node{
		Page: text("home"),
		Children: map[string]Node{
			"projects": {
				Title: "Projects",
				Page:  ctxText(),
				Generate: func() map[string]Node {
					return map[string]Node{"alpha": {Page: text("alpha")}}
				},
			},
		},
	}
	root := BuildLocales(base, []Locale{{
		Code: "de",
		Override: map[string]func(Node) Node{
			"projects": func(n Node) Node { n.Title = "Projekte"; return n },
		},
	}})

	out := filepath.Join(dir, "build")
	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}

	want := `title="Projekte" path="de/projects/" prefix="/de" locale="de" url="/de/projects/"`
	if got := read(t, filepath.Join(out, "de", "projects", "index.html")); got != want {
		t.Errorf("de/projects/index.html =\n  %s\nwant\n  %s", got, want)
	}
	if got := read(t, filepath.Join(out, "de", "projects", "alpha", "index.html")); got != "alpha" {
		t.Errorf("de/projects/alpha/index.html = %q, want the generated child kept", got)
	}
}

// A nil override function is skipped rather than blanking the node.
func TestLocaleOverrideIgnoresNilFunc(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	root := BuildLocales(Node{Page: text("home")}, []Locale{{
		Code:     "de",
		Override: map[string]func(Node) Node{"": nil},
	}})

	out := filepath.Join(dir, "build")
	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(out, "de", "index.html")); got != "home" {
		t.Errorf("de/index.html = %q, want the derived page", got)
	}
}

func TestBuildLocalesDoesNotMutateBase(t *testing.T) {
	base := Node{Children: map[string]Node{"about": {Page: text("about")}}}

	BuildLocales(base, []Locale{{Code: "de"}})

	if _, ok := base.Children["de"]; ok {
		t.Errorf("BuildLocales mutated the caller's Children map")
	}
	if len(base.Children) != 1 {
		t.Errorf("base.Children has %d entries, want 1", len(base.Children))
	}
}

func TestBuildLocalesSkipsEmptyCode(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	root := BuildLocales(Node{Page: text("home")}, []Locale{{Prefix: "/x"}})
	if len(root.Children) != 0 {
		t.Errorf("locale with an empty Code was mounted: %v", root.Children)
	}
}
