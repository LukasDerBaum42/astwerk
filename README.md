# astwerk

A minimal, no-magic static site generator library for Go + [templ](https://templ.guide).
No Node.js, no JS build tooling; optional client-side scripting via Go→WASM.

Design law: **abstract only the repeating boilerplate.** If a project needs
something the tree can't express, it falls back to plain Go — never a wall it
can't get past.

```
go get github.com/LukasDerBaum42/astwerk
```

## Status

All of §2–§7 is implemented.

```
ssg/       Node, Build, BuildLocales, CompileScripts
content/   Load, LoadDir, Decode, Slugs
wasmwrap/  DOM, style, events, timers, fetch, canvas   (js/wasm only)
reactive/  signals, bindings, keyed lists, router, resources, templ interop
starter/   13 copyable .templ files — see starter/README.md
```

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
	Title  string // the node's Title
	Path   string // "", "about/", "de/projects/dice-dungeon/"
	Prefix string // "" or "/de" — the locale's URL prefix
	Locale string // "" or "de"
}

func (c Ctx) URL() string // "/", "/about/", "/de/about/"
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
	OutDir: "build", // default
	Dev:    false,   // mirrors the usual --dev flag
}
```

`Dev` skips the full clean of `OutDir` and overwrites files in place, so a
running dev server keeps its inodes and open file handles.

---

## `content` — markdown with TOML front matter

```go
type Page struct {
	Meta map[string]any // front matter between +++ markers
	HTML string         // rendered markdown body
}

func Load(path string) (Page, error)
func Parse(src string) (Page, error)
func LoadDir(dir string) (map[string]Page, error) // non-recursive, *.md, keyed by slug
func Slugs(pages map[string]Page) []string        // sorted keys
func Decode[T any](p Page) (T, error)             // front matter into your struct
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

---

## i18n — locales are derived, not rewritten

A locale's subtree is generated **from the base tree**. You do not write it out
again. Every page in the base tree is mirrored under the locale's directory
with `Ctx.Prefix` and `Ctx.Locale` set; you list only what actually differs.

```go
root = ssg.BuildLocales(root, []ssg.Locale{{
	Code:   "de",
	Prefix: "/de", // defaults to "/" + Code
	Override: map[string]ssg.Node{
		"":         {Title: "Meine Seite", Page: ssg.Templ(src_de.HomePage)},
		"projects": projects("content/i18n-de/projects"),
	},
}})
```

Everything not named in `Override` — `about`, `goals`, `links`, and any
generated children — appears under `/de/` on its own, rendered by the same
components with `prefix = "/de"`.

| | |
| --- | --- |
| `Code` | language code and mount directory (`de` → `/de/...`) |
| `Prefix` | URL prefix passed to pages; defaults to `"/" + Code` |
| `Override` | nodes that differ, keyed by path relative to the locale root (`""` is its home page) |

Two rules worth knowing:

- **Assets are not inherited.** `CopyFrom` and `CompileFrom` are dropped from
  derived subtrees, because stylesheets and scripts are shared across languages
  and served from the site root. Without this you'd get `build/de/style/`.
- **An override replaces its node wholesale**, not field by field. Overriding
  the locale root (`""`) is the exception: it keeps the inherited children
  unless the replacement brings its own.

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
