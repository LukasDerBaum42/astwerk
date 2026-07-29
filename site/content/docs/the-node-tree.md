+++
Title = "The Node tree"
Description = "Node, Build, Ctx — the entire mechanism for generating a site."
Order = 20
+++

A `Node` describes one directory in the output. `Build` walks the tree from the
root and writes it. That is the whole mechanism — there is no second system.

## Node

```go
type Node struct {
	Title       string
	Page        PageFunc
	Files       map[string]PageFunc
	Generate    func() map[string]Node
	CopyFrom    string
	CompileFrom string
	Children    map[string]Node
}
```

Every field is optional. The zero value does nothing except recurse into
`Children`, so a node can exist purely to group.

| Field | Effect at this node's directory |
| --- | --- |
| `Title` | passed to pages as `Ctx.Title` |
| `Page` | renders `index.html` |
| `Files` | renders extra files by name — `404.html`, `rss.xml`, `feed/rss.xml` |
| `Generate` | children computed at build time, merged into `Children` |
| `CopyFrom` | copies a directory's contents in as-is |
| `CompileFrom` | compiles a scripts directory in |
| `Children` | subdirectories, keyed by name |

## What Build does

For each node, at its output path:

1. `Page` renders to `index.html`, then each entry in `Files` renders to its key.
2. `CopyFrom` is copied in.
3. `CompileFrom` is compiled in.
4. `Generate` is called and merged into `Children` — **generated keys win** on
   collision, which is how a content directory adds siblings without you writing
   entries by hand.
5. `Children` are walked, each key appended to the path.

Children are visited in sorted key order, so a build is reproducible and a
failure always reports the same node first.

Keys may contain slashes to nest without an intermediate node
(`"assets/style"`), but may not escape the output directory — `..` and absolute
paths are rejected with an error rather than silently writing outside.

A node with nothing to do creates no directory. That matters: an empty
`Children` entry doesn't litter the output with empty folders.

## Ctx — the walker tells the page where it is

The walker already computed each page's path. Rather than making you retype it,
it hands it over:

```go
type Ctx struct {
	Title  string // the node's Title
	Path   string // "", "about/", "de/projects/thing/"
	Prefix string // "" or "/de" — the locale's URL prefix
	Locale string // "" or "de"
	Base   string // "" or "/astwerk" — the site's base path
}

func (c Ctx) URL() string              // this page's absolute URL
func (c Ctx) Asset(path string) string // base-aware asset URL
func (c Ctx) Link(path string) string  // base- and locale-aware link
```

`Page` and every entry in `Files` is a `PageFunc`:

```go
type PageFunc func(Ctx) templ.Component
```

This is the difference between astwerk and a hand-written build loop. Compare:

```go
// Without Ctx — the path is typed twice and the prefix threaded by hand:
"about": {Page: page(views.About, "About", "about/", prefix)},

// With Ctx — the walker supplies both:
"about": {Title: "About", Page: ssg.Templ(views.About)},
```

### Adapting your components

If your components take the common `(title, path, prefix)` triple, `ssg.Templ`
adapts them:

```go
"about": {Title: "About", Page: ssg.Templ(views.About)},
```

For a component that needs nothing, `ssg.Static`. For anything else — extra
arguments, computed values — write the closure and take what you need from `Ctx`:

```go
Page: func(c ssg.Ctx) templ.Component {
	return views.Projects(c.Title, c.Path, c.Prefix, tiles)
},
```

That escape hatch is the design law in action: the helpers cover the common
shape, and when they don't fit you drop to a plain function with no ceremony.

## Always build URLs through Ctx

Write `/style/style.css` as a literal and it breaks the moment the site is served
from a subdirectory — which is exactly what GitHub project Pages do. A project
site lives at `user.github.io/repo/`, and Pages does **no** URL rewriting: an
absolute `/style/style.css` resolves against the domain root, which is a
different site. If you also have a user Pages site at `user.github.io`, you get
its files instead of a 404 — wrong, and silently so.

Use `Ctx.Asset` and `Ctx.Link`:

```templ
templ Layout(c ssg.Ctx) {
	<link rel="stylesheet" href={ c.Asset("style/style.css") }/>
	<a href={ c.Link("docs/") }>Docs</a>
}
```

Note the parameter is named `c`, not `ctx`. templ's generated code declares its
own `ctx context.Context`, so a parameter called `ctx` collides with it and the
generated file won't compile. It's an easy half hour to lose.

`Asset` resolves a path from the site root. `Link` does the same but also applies
the current locale prefix, so the same nav markup stays inside whatever language
it's rendered in.

### Relative URLs make the output portable

There are two ways to resolve those. The better default is relative:

```go
ssg.Build(root, ssg.BuildOptions{
	RelativeURLs: true,
	BaseURL:      "/astwerk", // only used by Ctx.URL, for canonical tags
})
```

`Asset` and `Link` then emit URLs relative to each page's own depth —
`style/style.css` from the root, `../../style/style.css` from two levels down.
One build works everywhere: opened straight off disk with `file://`, served at a
localhost root, and deployed under `/astwerk/`, with no rebuild and no flag to
remember. Previewing is just:

```
cd build && python3 -m http.server 8000
```

The alternative is absolute URLs with a base path — leave `RelativeURLs` off and
set `BaseURL`. Everything then starts with `/astwerk/`, which is correct in
production and awkward everywhere else, because a local preview has to be served
from a matching subdirectory.

`Ctx.URL` is always absolute regardless, because canonical links, Open Graph tags
and feed entries have to be. That is what `BaseURL` is still for.

This site uses relative URLs, and a test walks every generated page resolving
each link against the directory it was emitted from.

## Files, for outputs that aren't directory indexes

`Page` always writes `index.html`, because pretty URLs want a directory per page.
Some files can't work that way: hosts look for `404.html` at the root, feed
readers want `rss.xml`, and GitHub Pages needs `.nojekyll` or it runs the output
through Jekyll and eats anything starting with an underscore.

```go
root := ssg.Node{
	Page: ssg.Templ(views.Home),
	Files: map[string]ssg.PageFunc{
		"404.html":  ssg.Templ(views.NotFound),
		"rss.xml":   feed,
		".nojekyll": empty,
	},
}
```

`Files` entries render after `Page`, so a key of `"index.html"` overwrites it.

## BuildOptions

```go
type BuildOptions struct {
	OutDir  string // default "build"
	Dev     bool   // skip the clean, overwrite in place
	BaseURL string // "/astwerk" when served from a subdirectory
}
```

`Dev` exists so a running dev server doesn't have the ground pulled out from
under it. Without it, `Build` removes `OutDir` first, so a renamed page can't
leave a stale file behind.

## When the tree isn't enough

It won't always be. `Build` returns an error and nothing stops you doing more
work afterwards:

```go
if err := ssg.Build(root, opts); err != nil {
	log.Fatal(err)
}
if err := writeSitemap("build/sitemap.xml", pages); err != nil {
	log.Fatal(err)
}
```

That's the intended escape hatch, not a workaround. The tree covers the
repeating shape; anything genuinely one-off stays plain Go.
