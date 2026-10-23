# astwerk

![CI](https://github.com/LukasDerBaum42/astwerk/actions/workflows/ci.yml/badge.svg)

A minimal, no-magic static site generator library for Go + [templ](https://templ.guide).
No Node.js, no JS build tooling. Client-side code lives in the optional
[astwerk-live](https://github.com/LukasDerBaum42/astwerk-live) extension.

Design law: **abstract only the repeating boilerplate.** If a project needs
something the tree can't express, it falls back to plain Go — never a wall it
can't get past.

```
go get github.com/LukasDerBaum42/astwerk
```

**[Documentation](https://lukasderbaum42.github.io/astwerk-website/)** ·
[Examples](https://lukasderbaum42.github.io/astwerk-website/examples/) ·
[API reference](https://pkg.go.dev/github.com/LukasDerBaum42/astwerk)

The documentation site is itself built with astwerk — source at
[astwerk-website](https://github.com/LukasDerBaum42/astwerk-website), in its own
repository so its dependencies stay out of this module.

## Status

All of §2–§7 is implemented, plus §11.1–§11.3 of the follow-up list.

```
ssg/        Node, Build, BuildLocales, CompileScripts, URLs, Sitemap
content/    Load, LoadDir, LoadTree, Decode, Slugs, Chunk, Page.Render, components
assets/     sidecar metadata (meta.json / meta.toml) for asset directories
devserver/  watch, rebuild, live reload
starter/    13 copyable .templ files — see starter/README.md
```

Client-side packages (`x`, `reactive`, `wasmwrap`, `jsdom`) live in the
opt-in [astwerk-live](https://github.com/LukasDerBaum42/astwerk-live)
extension, which builds on the `ssg.Steps` seam described below.

`astwerk-spec.md` is the original design sheet. Sections 2, 4, 6a and 9 describe
API that has since changed — the README, the docs site and the source are
current; the spec is a historical record. §11 is the forward-looking half of
that original design; §12 (`astwerk/x`, compiled reactive markup) is **shipped**
— the spec's §12 is the historical proposal, and the current API lives in
astwerk-live's `x/doc.go` and the docs site.

---

## The idea

You describe the output directory as a tree of `Node`s. `Build` walks it and
writes the site. That's the entire mechanism — there is no config file, no
convention scanner, no code generation.

```go
package main

import (
	"log"
	"os"

	"github.com/LukasDerBaum42/astwerk/ssg"
	"myproject/src"
)

func main() {
	dev := len(os.Args) > 1 && os.Args[1] == "--dev"

	root := ssg.Node{
		Title: "My Site",
		Page:  ssg.Templ(src.HomePage),
		Children: map[string]ssg.Node{
			"about":  {Title: "About", Page: ssg.Templ(src.About)},
			"style":  {CopyFrom: "style"},
			"public": {CopyFrom: "public"},
		},
	}

	if err := ssg.Build(root, ssg.BuildOptions{Dev: dev, RelativeURLs: true}); err != nil {
		log.Fatal(err)
	}
}
```

That writes `build/index.html`, `build/about/index.html`, and copies `style/`
and `public/` in.

---

## `ssg.Node`

Every field is optional. The zero value does nothing but recurse into
`Children`, so a node can exist purely to group.

| Field | Type | Effect at this node's directory |
| --- | --- | --- |
| `Title` | `string` | passed to pages as `Ctx.Title` |
| `Data` | `any` | passed to pages as `Ctx.Data`, inherited by children |
| `Page` | `PageFunc` | renders `index.html` |
| `Files` | `map[string]PageFunc` | renders extra files by name (`404.html`, `rss.xml`, `feed/rss.xml`) |
| `Generate` | `func() map[string]Node` | children computed at build time, merged into `Children`; generated keys win |
| `CopyFrom` | `string` | copies a directory's contents in as-is |
| `CompileFrom` | `string` | compiles a scripts directory in |
| `Children` | `map[string]Node` | subdirectories, keyed by name |

Children are visited in sorted key order, so output is reproducible and a
failure always reports the same node first. Keys may contain slashes
(`"assets/style"`) but may not escape the output directory.

### `Ctx` — what the walker tells your page

The walker already knows where a page lives, so it tells the page instead of
making you retype it:

```go
type Ctx struct {
	Title    string // the node's Title
	Path     string // "", "about/", "de/projects/dice-dungeon/"
	Prefix   string // "" or "/de" — the locale's URL prefix
	Locale   string // "" or "de"
	Base     string // "" or "/astwerk" — the site's base path
	Relative bool   // in-page URLs are relative to this page
	Data     any    // Node.Data for this subtree
	Item     any    // the current content.Item, when rendering a collection page
}

func DataOf[T any](c Ctx) (T, bool)        // Ctx.Data with your type
func (c Ctx) URL() string               // absolute: canonical tags, feeds
func (c Ctx) Asset(path string) string  // an asset, from this page
func (c Ctx) Link(path string) string   // a page, locale-aware
func (c Ctx) Rel() string               // Path without the locale segment
func (c Ctx) InLocale(prefix string) string // this page in another language
```

Never write an absolute URL as a literal in a layout. A site served from a
subdirectory — every GitHub project Pages site — resolves `/style/style.css`
against the domain root, which is a different site. Set `RelativeURLs: true` and
use `Asset`/`Link`, and one build then works off disk, at a localhost root, and
under a subpath with no rebuild:

```go
ssg.Build(root, ssg.BuildOptions{
	RelativeURLs: true,
	BaseURL:      "/astwerk", // only Ctx.URL uses this
})
```

`Data` is a typed escape hatch: `ssg.DataOf[T](ctx)` returns it with the type
you stored. Set it once on a subtree (`Node.Data`); children inherit it unless
they override it. `Item.Content(ctx)` also puts the current collection item on
the context, available through `content.ItemOf[T](ctx)`. Markdown components
receive the same ctx, so they read either without props:

```go
content.Register(cs, "post_list", func(ctx ssg.Ctx, _ struct{}) templ.Component {
	data, _ := ssg.DataOf[SiteData](ctx)
	return widgets.PostList(ctx, data.Posts)
})
```

`Page` and every entry in `Files` is a `PageFunc`:

```go
type PageFunc func(Ctx) templ.Component
```

You rarely write one by hand. If your components take the usual
`(title, path, prefix)` triple:

```templ
templ About(title string, path string, prefix string) {
	@templates.BaseLayout(title, path, prefix) {
		<h2>About me</h2>
	}
}
```

then `ssg.Templ` adapts them, and the walker fills in `path` and `prefix`:

```go
"about": {Title: "About", Page: ssg.Templ(src.About)},
```

For a component that takes nothing, use `ssg.Static(src.Home)`. For anything
else — extra arguments, computed values — write the closure yourself:

```go
Page: func(c ssg.Ctx) templ.Component {
	return src.Project(c.Title, c.Path, c.Prefix, tiles)
},
```

### `BuildOptions`

```go
ssg.BuildOptions{
	OutDir:       "build", // default
	Dev:          false,   // mirrors the usual --dev flag
	BaseURL:      "",      // "/astwerk" when served from a subdirectory
	RelativeURLs: false,   // emit URLs relative to each page
	Parallel:     false,   // render sibling nodes concurrently
}
```

`Dev` skips the full clean of `OutDir` and overwrites files in place, so a
running dev server keeps its inodes and open file handles.

`Parallel` fans sibling nodes out across goroutines, bounded by `GOMAXPROCS`.
Output and error reporting are identical either way — errors are collected by
index and returned in sorted order — so the only thing that changes is wall
time. It's opt-in because it runs your `Page`, `Files` and `Generate` functions
concurrently: rendering templ components is safe, a `Generate` closure appending
to a captured slice is not. `CompileScripts` is always parallel; each `.wasm` is
built by its own `go build` process, so there's nothing of yours to race.

---

## `content` — markdown with TOML or JSON front matter

```go
type Page struct {
	Meta map[string]any // front matter between +++ markers
}

func (p Page) Render(ctx ssg.Ctx) templ.Component // body, with components and URLs resolved

func Load(path string) (Page, error)
func Parse(src string) (Page, error)
func LoadDir(dir string) (map[string]Page, error)  // non-recursive, *.md, keyed by slug
func LoadTree(dir string) (map[string]Page, error) // recursive, keyed by "2024/hello"
func Slugs(pages map[string]Page) []string         // sorted keys
func Decode[T any](p Page) (T, error)              // front matter into your struct
func Chunk[T any](items []T, size int) [][]T       // for paging a long collection

func MustLoadCollection[T any](dir string) Collection[T]      // panics; package-level value
func MustLoadCollectionTree[T any](dir string) Collection[T]
func LoadData[T any](path string) (T, error)                  // one .json/.toml file
func MustLoadData[T any](path string) T
func ParseData[T any](data []byte, format string) (T, error)
```

Front matter stays a generic map so each project decodes it into whatever
struct its own layouts need. A `+++` block whose first non-space character is
`{` is JSON; otherwise it is TOML. Markdown renders through goldmark with raw
HTML passed through, so a page can drop into hand-written markup.

Render the body with the page's `ssg.Ctx`. That is what makes root-relative
URLs (`/img/x.png`, `/about/`) work from any page under `RelativeURLs`, and it
is where markdown components run:

```md
A note: {{@callout(kind="warning", text="Careful")}}
```

Register components on a loader (`content.NewLoader(nil).WithComponents(cs)`);
`content.Register` takes one props struct, and `Register2` / `Register3` /
`Register4` take two to four positional arguments:

```md
{{@figure("img/a.png", "Caption")}}
```

For data that is not content, `LoadData` is the whole API. A typed package
variable is the global-state pattern — loaded once, used from any template:

```go
var Site = content.MustLoadData[SiteConfig]("site.json")
var Posts = content.MustLoadCollectionTree[Post]("content/posts")
```

`MustLoad*` panics at start-up; the non-`Must` loaders return an error.
`Collection` also has `Len`, `Each`, `First` and `Get` for the common loops.

Combined with `Generate`, that's a collection — one index page plus one page
per file, with no path arithmetic anywhere:

```go
type FrontMatter struct {
	Title       string
	Description string
}

func projects(dir string) ssg.Node {
	pages, err := content.LoadDir(dir)
	if err != nil {
		log.Fatal(err)
	}

	var tiles []templ.Component
	children := map[string]ssg.Node{}

	// Slugs, not a map range: map order would reshuffle the index every build.
	for _, slug := range content.Slugs(pages) {
		fm, _ := content.Decode[FrontMatter](pages[slug])

		children[slug] = ssg.Node{
			Title: fm.Title,
			Page: func(c ssg.Ctx) templ.Component {
				return src.ProjectPage(c.Title, c.Path, c.Prefix, pages[slug].Render(c))
			},
		}
		tiles = append(tiles, src.ProjectTile(fm.Title, fm.Description, slug))
	}

	return ssg.Node{
		Title:    "Projects",
		Page:     func(c ssg.Ctx) templ.Component { return src.Project(c.Title, c.Path, c.Prefix, tiles) },
		Children: children,
	}
```

Use it as `"projects": projects("content/projects")`.

`LoadTree` is the recursive variant, keyed by path relative to `dir`:
`content/blog/2024/hello.md` becomes `2024/hello`. `Node.Children` keys may
contain slashes, so those keys nest on their own. `Chunk` splits a slice into
groups of at most `size` for a paginated index — the arithmetic only, since
turning groups into nodes is three lines and every site wants them differently.

---

## `assets` — sidecar metadata for asset directories

A directory of images, audio or video often needs licence and credit data.
`assets` scans a tree and pairs each regular file with an entry from a
`meta.json` or `meta.toml` in the same directory:

```go
type Media struct {
	Credit  string `json:"credit" toml:"credit"`
	License string `json:"license" toml:"license"`
}

entries, err := assets.LoadTree[Media]("public/img", assets.Options[Media]{
	Include: []string{"*.png", "*.jpg", "*.webp"},
})

for _, e := range entries {
	// e.File.Path, e.Meta.Credit, e.Meta.License
}
```

- `LoadTree` is recursive; `LoadDir` reads one directory.
- The sidecar is a map from file name to metadata; the sidecar files are not
  returned as entries.
- Files without an entry are included with the zero `T` and `HasMeta == false`;
  `RequireMeta` drops them.
- `Options` also covers a custom sidecar name, include/exclude globs and skipped
  directories.

Two seams keep it adaptable: `Options.Sidecar` replaces the sidecar lookup
(per-directory metadata, inheritance, a different file, a remote source), and
`assets.Walk` yields directories and files with no metadata logic at all.
`assets.ReadSidecar` and `assets.MetaSidecar` are the built-in decoder and
lookup, exposed for reuse. See `design/assets.md` for examples.

`CopyFrom` copies the sidecar along with the assets. Keep a sidecar outside
the copied directory, or exclude it, if it should not be published.

---

## i18n — locales are derived, not rewritten

A locale's subtree is generated **from the base tree**. You do not write it out
again. Every page in the base tree is mirrored under the locale's directory
with `Ctx.Prefix` and `Ctx.Locale` set; you list only what actually differs.

```go
root = ssg.BuildLocales(root, []ssg.Locale{{
	Code:   "de",
	Prefix: "/de", // defaults to "/" + Code
	Override: map[string]func(ssg.Node) ssg.Node{
		"": func(n ssg.Node) ssg.Node {
			n.Title, n.Page = "Meine Seite", ssg.Templ(src_de.HomePage)
			return n
		},
		"about": func(n ssg.Node) ssg.Node {
			n.Title = "Über mich" // Page and Children survive untouched
			return n
		},
	},
}})
```

Everything not named in `Override` — `goals`, `links`, and any generated
children — appears under `/de/` on its own, rendered by the same components with
`prefix = "/de"`.

| | |
| --- | --- |
| `Code` | language code and mount directory (`de` → `/de/...`) |
| `Prefix` | URL prefix passed to pages; defaults to `"/" + Code` |
| `Override` | patches for the nodes that differ, keyed by path relative to the locale root (`""` is its home page) |

Two rules worth knowing:

- **Assets are not inherited.** `CopyFrom` and `CompileFrom` are dropped from
  derived subtrees, because stylesheets and scripts are shared across languages
  and served from the site root. Without this you'd get `build/de/style/`. A
  locale that genuinely needs its own asset sets the field back in an override.
- **An override is a function over the node, not a replacement for it.** It
  receives the node `BuildLocales` would otherwise have derived, so changing one
  field is one line and nothing you don't touch is lost. A key with no
  counterpart in the base tree gets the zero `Node`, which is how a page that
  exists in only one language gets written.

---

## Client-side code — `astwerk-live`

Everything that runs in the browser lives in the opt-in
[astwerk-live](https://github.com/LukasDerBaum42/astwerk-live) extension:

| | |
| --- | --- |
| `x` | compiled reactive markup — JS emitted at build time, no WASM |
| `reactive` | signals, bindings, keyed lists, router, resources, templ interop |
| `wasmwrap` | `syscall/js` made to feel like Go (js/wasm only) |
| `jsdom` | fake DOM for testing bindings without a browser (js/wasm only) |

```sh
go get github.com/LukasDerBaum42/astwerk-live
```

Core keeps the build side: `ssg.CompileScripts` (below) still compiles
`js && wasm` scripts, and the build-step seam (`ssg.Steps`) is what the
extension hooks into.

## Scripts

`CompileFrom` runs `ssg.CompileScripts(srcDir, outDir)`:

- a top-level `.go` file carrying `//go:build js && wasm` → `<name>.wasm`
- a subdirectory containing at least one such file → `<dirname>.wasm`
- `.ts` files → `tsc`, in one batch

Anything else is ignored, so helper packages can live alongside. The build runs
from the entry's parent directory, so a script can import the project's own
packages. When a `.wasm` is produced, Go's `wasm_exec.js` glue is copied in
beside it.

The build constraint is required: without one, `go build ./...` at the project
root would also try to compile the script for the host platform.

---

## `devserver` — watch, rebuild, reload

The loop you leave running while you work. Only `Build` is required:

```go
devserver.Run(context.Background(), devserver.Config{
	Build: devserver.Steps(
		devserver.Command("templ", "generate"),
		devserver.Command("go", "run", ".", "--dev"),
	),
})
```

```
2026/07/30 23:28:03 serving build/ on http://127.0.0.1:8080
2026/07/30 23:28:12 changed: templates/layout.templ, rebuilding
2026/07/30 23:28:15 rebuilt in 3.701s, reloading
```

**`Build` has to run out of process.** A running Go program cannot load new
code, so a `Build` closure that calls your render functions directly keeps
rendering the components compiled into the binary that started the server: you
edit a `.templ`, the build "succeeds", the browser reloads, and nothing changed
— which is worse than nothing happening, because it looks like it worked.
`Command` and `Steps` shell out, which recompiles. `Command` puts the failed
command's output in the error, so a compiler message reaches the browser
overlay with file and line. An in-process closure is right only when the build
reads nothing but data — markdown, CSS, images.

It serves the build directory the way a static host will — a directory means its
`index.html`, a missing path gets your own `404.html` with a 404 status — and
injects a live-reload script into every HTML response on the way out, so nothing
is written into the build output. A failed build doesn't stop the server: the
error is logged and shown in the browser as an overlay, and the next successful
build clears it.

Pass `--dev` to the inner build, so a rebuild overwrites in place instead of
removing the directory out from under a browser mid-request.

The watcher polls file size and modification time (300ms by default, configurable
via `Interval`) rather than using OS file events. That keeps the dependency list
at templ, goldmark and toml — which is the whole pitch — and costs a sub-second
delay before a rebuild starts. `devserver` imports nothing from `ssg`: you hand
it a `func() error`, so a build that does more than call `ssg.Build` gets the
same loop with no extra plumbing.

---



## `starter/`

13 copyable `.templ` files — layouts, a list+detail collection, pages, wasm
hosts, SEO and RSS. Not importable and not wired into `Build`: you copy them in
and they become your code. See [starter/README.md](starter/README.md). The
reactive page and widgets live in astwerk-live's `starter/`.

## Non-goals

No config file format, no reflection magic, no enforced project structure
beyond the `Node` tree. Directory names, file layout and template structure stay
entirely up to the consuming project.

## License

MIT — see [LICENSE](LICENSE).
