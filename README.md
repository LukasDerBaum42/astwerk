# astwerk

A minimal, no-magic static site generator library for Go + [templ](https://templ.guide).
No Node.js, no JS build tooling; optional client-side scripting via Go→WASM.

Design law: **abstract only the repeating boilerplate.** If a project needs
something the tree can't express, it falls back to plain Go — never a wall it
can't get past.

```
go get github.com/LukasDerBaum42/astwerk
```

**[Documentation](https://lukasderbaum42.github.io/astwerk/)** ·
[Examples](https://lukasderbaum42.github.io/astwerk/examples/) ·
[API reference](https://pkg.go.dev/github.com/LukasDerBaum42/astwerk)

The documentation site is itself built with astwerk — source at
[astwerk-website](https://github.com/LukasDerBaum42/astwerk-website), in its own
repository so its dependencies stay out of this module.

## Status

All of §2–§7 is implemented, plus §11.1–§11.3 of the follow-up list.

```
ssg/        Node, Build, BuildLocales, CompileScripts
content/    Load, LoadDir, LoadTree, Decode, Slugs, Chunk
devserver/  watch, rebuild, live reload
wasmwrap/   DOM, style, events, timers, fetch, canvas   (js/wasm only)
reactive/   signals, bindings, keyed lists, router, resources, templ interop
jsdom/      fake DOM for testing bindings without a browser  (js/wasm only)
starter/    13 copyable .templ files — see starter/README.md
```

`astwerk-spec.md` is the original design sheet. Sections 2, 4, 6a and 9 describe
API that has since changed — the README, the docs site and the source are
current; the spec is a historical record. §11 and §12 are the forward-looking
half: §11.4 (client-side runtime size) and §12 (`astwerk/x`, compiled reactive
markup) are not started.

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

	if err := ssg.Build(root, ssg.BuildOptions{Dev: dev}); err != nil {
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
}

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

## `content` — markdown with TOML front matter

```go
type Page struct {
	Meta map[string]any // front matter between +++ markers
	HTML string         // rendered markdown body
}

func Load(path string) (Page, error)
func Parse(src string) (Page, error)
func LoadDir(dir string) (map[string]Page, error)  // non-recursive, *.md, keyed by slug
func LoadTree(dir string) (map[string]Page, error) // recursive, keyed by "2024/hello"
func Slugs(pages map[string]Page) []string         // sorted keys
func Decode[T any](p Page) (T, error)              // front matter into your struct
func Chunk[T any](items []T, size int) [][]T       // for paging a long collection
```

Front matter stays a generic map so each project decodes it into whatever
struct its own layouts need. Markdown renders through goldmark with raw HTML
passed through, so a page can drop into hand-written markup.

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
		body := pages[slug].HTML

		children[slug] = ssg.Node{
			Title: fm.Title,
			Page: func(c ssg.Ctx) templ.Component {
				return src.ProjectPage(c.Title, c.Path, c.Prefix, body)
			},
		}
		tiles = append(tiles, src.ProjectTile(fm.Title, fm.Description, slug))
	}

	return ssg.Node{
		Title:    "Projects",
		Page:     func(c ssg.Ctx) templ.Component { return src.Project(c.Title, c.Path, c.Prefix, tiles) },
		Children: children,
	}
}
```

Use it as `"projects": projects("content/projects")`.

`LoadTree` is the recursive variant, keyed by path relative to `dir`:
`content/blog/2024/hello.md` becomes `2024/hello`. `Node.Children` keys may
contain slashes, so those keys nest on their own. `Chunk` splits a slice into
groups of at most `size` for a paginated index — the arithmetic only, since
turning groups into nodes is three lines and every site wants them differently.

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

## `wasmwrap` — client-side scripting

Optional. `syscall/js` made to feel like Go, for the browser half of a site.
It exists only under `GOOS=js GOARCH=wasm`, so a script needs the matching
constraint:

```go
//go:build js && wasm

package main

import "github.com/LukasDerBaum42/astwerk/wasmwrap"

func main() {
	nav := wasmwrap.Query("#nav")
	wasmwrap.Query("#toggle").On("click", func(e wasmwrap.Event) {
		e.PreventDefault()
		nav.Class().Toggle("open")
	})
	select {} // keep the module alive for the handlers
}
```

Everything that has nothing to return returns its receiver, so calls chain:

```go
wasmwrap.Query("#box").Style().Color("red").Display("block")
```

| | |
| --- | --- |
| `Query`, `QueryAll`, `Create`, `Body`, `Doc` | finding and making elements |
| `Element` | `Append`, `Remove`, `Clone`, `Find`, `Parent`, `Children` |
| | `Text`/`SetText`, `HTML`/`SetHTML`, `Attr`/`SetAttr`, `InputValue` |
| | `Class()` → `Add`/`Remove`/`Toggle`/`Has` |
| `Style()` | `Color`, `Display`, `Background`, `Size`, `Position`, `Hide`/`Show`, `Set` for anything else |
| `On`, `Once` | event binding; both return an unsubscribe func |
| `Event` | `PreventDefault`, `StopPropagation`, `Target`, `Key`, `Pos`, `PosIn` |
| `SetTimeout`, `SetInterval` | return a cancel func |
| `Fetch`, `FetchString` | blocking; no promise at the call site |
| `AsCanvas()` | `Context2D`, `SetSize`, `FitToDisplay` |
| `Ctx2D` | typed 2D drawing, plus `RequestAnimationFrame` for an animation loop |

Two things worth knowing:

- **A failed `Query` is inert, not a panic.** Check `Element.Exists()`; readers
  like `Text()` return zero values rather than blowing up on a null node.
- **`Fetch` blocks its goroutine**, which is fine from `main` — when every
  goroutine blocks, the runtime returns control to the JS event loop. It is
  *not* safe directly inside an event handler, which runs on a callback
  goroutine the event loop is waiting on. Use `go func() { ... }()` there.

Anything not modeled drops to `.Value()` and raw `syscall/js`, so the wrapper is
a shortcut, never a dead end.

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

## `reactive` — interactive UI

Opt-in, on top of `wasmwrap`. Fine-grained rather than virtual-DOM: a binding
updates exactly the property it owns, and a keyed list moves nodes rather than
rebuilding them.

```go
count := reactive.NewSignal(0)
doubled := reactive.Computed(func() int { return count.Get() * 2 })

reactive.BindText(out, func() string { return strconv.Itoa(doubled.Get()) })
reactive.On(btn, "click", func(wasmwrap.Event) {
	count.Update(func(v int) int { return v + 1 })
})
```

Dependencies are tracked, not declared: `Signal.Get` records a dependency on
whatever effect is running, so an effect subscribes to exactly what it read last
time. A branch not taken creates no dependency.

| | |
| --- | --- |
| **State** | `Signal` (`Get`/`Set`/`Peek`/`Update`/`Subscribe`), `Computed` → `Memo`, `Effect`, `Batch`, `Untracked` |
| **Lifecycle** | `OnCleanup`, `Scope`; effects nest and dispose their children |
| **DOM** | `BindText`, `BindHTML`, `BindAttr`, `BindClass`, `BindStyle`, `BindShow` |
| **Forms** | `BindValue`, `BindNumber`, `BindChecked` — two-way |
| **Structure** | `BindWhen` (conditional subtree), `BindList` (keyed) |
| **Routing** | `Router`, `Route`, `Params`, `Navigate`, `Replace`, `Path`, `InterceptLinks` |
| **Async** | `Resource` with `Data`/`Err`/`Loading` signals, `FetchJSON[T]` |
| **templ** | `Hydrate`, `HydrateAll`, `BindTempl`, `TemplElement`, `RenderTempl` |

Routes match segment by segment — `/projects/:slug` captures, `/docs/*` catches
the rest — and each view gets its own scope, so navigating away disposes its
effects.

### Using templ in the browser

templ is pure Go, so it compiles to wasm and runs client-side unchanged. There
are two ways to use it, and they are not equally good:

**Bind to the markup astwerk already generated** — the recommended default.
`ssg.Build` renders your templ at build time; a script attaches behaviour to it.
Markup stays in one place, updates stay surgical, nothing renders twice.

```go
reactive.Hydrate("#counter", func(root wasmwrap.Element) {
	reactive.BindText(root.Find("[data-count]"), func() string {
		return strconv.Itoa(count.Get())
	})
	reactive.On(root.Find("[data-inc]"), "click", func(wasmwrap.Event) {
		count.Update(func(v int) int { return v + 1 })
	})
})
```

**Re-render fragments client-side** — `BindTempl` runs a component in the browser
and assigns the output as innerHTML. Convenient, but templ produces a *string*,
so there is nothing to patch against: everything inside the target is destroyed
and rebuilt, losing input focus, scroll position, selection and any handler bound
inside. Fine for a display-only panel, wrong for anything interactive.

```go
reactive.BindTempl(preview, func() templ.Component {
	return views.Markdown(source.Get())
})
```

### Testing bindings without a browser

`jsdom` is a small fake DOM — the one astwerk's own tests run against, exported
so yours can too:

```go
//go:build js && wasm

func init() { jsdom.Install() }

func TestCounter(t *testing.T) {
	jsdom.Reset()
	root := wasmwrap.Wrap(jsdom.NewElement("div"))
	// mount your bindings into root, then assert against it
}
```

```sh
export PATH="$PATH:$(go env GOROOT)/lib/wasm"
GOOS=js GOARCH=wasm go test ./...
```

It's not a browser and won't become one — layout, the CSS cascade, focus and
paint are out of scope, and selectors are limited to `#id`, `.class`, `[attr]`
and a bare tag name. It answers "did my Go code reach for the right property
with the right arguments", which is what a binding test is actually asking.

## `starter/`

13 copyable `.templ` files — layouts, a list+detail collection, pages, wasm
hosts, SEO and RSS. Not importable and not wired into `Build`: you copy them in
and they become your code. See [starter/README.md](starter/README.md).

## Non-goals

No config file format, no reflection magic, no enforced project structure
beyond the `Node` tree. Directory names, file layout and template structure stay
entirely up to the consuming project.

## License

MIT — see [LICENSE](LICENSE).
