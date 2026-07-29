# gossg — Spec Sheet

**License: MIT.** Applies to the whole project, including `starter/` —
templates are meant to be copied and edited freely with no attribution
burden beyond keeping the license header.


A minimal, no-magic static site generator library for Go + templ. Generalizes the
patterns already used in `lukasderbaum42.github.io` (main.go, build_projects.go,
builders.go) into a reusable module. No Node.js, no JS build tooling required;
optional client-side scripting via Go→WASM.

Design law for every piece below: **abstract only the repeating boilerplate.**
If a project needs something the tree/registry can't express, it falls back to
plain Go — never a wall it can't get past.

---

## 1. Module layout

```
gossg/
  ssg/          // core: tree walker, Node, Build()
  content/      // markdown + front matter parsing
  wasmwrap/     // syscall/js ergonomics wrapper
  starter/      // copyable starter .templ files (not imported, copied)
```

Consumers `go get github.com/LukasDerBaum42/gossg` and import `ssg`, `content`,
and optionally `wasmwrap`. `starter/` is not a package to import — it's
boilerplate a new project copies in and edits, same spirit as `create-react-app`
but without hiding what the copied files do.

---

## 2. Core types (`ssg` package)

```go
package ssg

// Node is one entry in the output filesystem tree. Every field is optional;
// zero value means "nothing to do here except recurse into Children."
type Node struct {
    Page        func() templ.Component  // renders this dir's index.html
    Generate    func() map[string]Node  // returns children built at build time
                                         // (e.g. one per content file); merged
                                         // into Children before recursing
    CopyFrom    string                   // copy a directory's contents as-is
    CompileFrom string                   // compile scripts dir (.go->wasm or .ts) here
    Children    map[string]Node          // static subdirectories
}

type BuildOptions struct {
    OutDir string // default "build"
    Dev    bool   // mirrors current --dev flag: skip full clean, in-place copies
}

// Build walks the tree from root and writes the whole site to OutDir.
func Build(root Node, opts BuildOptions) error
```

### 2.1 `Build` walker behavior (replaces `build_pages` + manual `os.MkdirAll` calls)

For each node, at its resolved output path `p`:

1. If `Page != nil`: `os.MkdirAll(p)`, then `devCreate(p+"/index.html", opts.Dev)`
   and `page.Render(ctx, f)` — same as today's `build_pages`.
2. If `CopyFrom != ""`: `devCopyFS(p, os.DirFS(CopyFrom), opts.Dev)` — same as
   today's calls for `style/` and `public/`.
3. If `CompileFrom != ""`: run the script compiler (§5) into `p`.
4. If `Generate != nil`: call it, merge the returned map into `Children`
   (generated keys win on collision — this is how `projects/foo/` siblings get
   created without hand-written entries).
5. Recurse into `Children`, appending each key to the path.

This one function replaces `build_pages`, the per-page `Render*` wrapper
functions, and the manual `os.MkdirAll` calls sprinkled through `main.go`.

### 2.2 `devCreate` / `devCopyFS`

Carried over unchanged from `builders.go` — they already do exactly what's
needed (skip full removal in dev mode, in-place overwrite). Move them into
`ssg` as unexported helpers used by `Build`.

---

## 3. Content & front matter (`content` package)

Generalizes `build_projects.go`'s `splitFrontMatter` + `md_to_HTML`.

```go
package content

// FrontMatter is intentionally generic: TOML into a map, not a fixed struct,
// so a layout function can pull whatever fields it defined in its own
// front-matter struct via a decode step.
type Page struct {
    Meta map[string]any   // TOML front matter between +++ markers
    HTML string           // parsed markdown body
}

// Load reads one markdown file, splits front matter, converts the body via
// goldmark (unsafe HTML enabled, same as current md_to_HTML), and returns it.
func Load(path string) (Page, error)

// LoadDir reads every *.md file in a directory (non-recursive, matching
// today's os.ReadDir usage in buildProjects) and returns them keyed by
// filename without extension (the current "slug" behavior).
func LoadDir(dir string) (map[string]Page, error)

// Decode unmarshals p.Meta into a caller-defined front-matter struct
// (e.g. the current FrontMatter{Title, Description}).
func Decode[T any](p Page) (T, error)
```

This is the piece a project's `Generate` function calls: read a content dir,
decode front matter per project's own struct, build a `Node` per file.

Example — `build_projects.go`'s logic re-expressed as a `Generate` func:

```go
Generate: func() map[string]ssg.Node {
    pages, _ := content.LoadDir("content/projects")
    out := map[string]ssg.Node{}
    for slug, p := range pages {
        fm, _ := content.Decode[FrontMatter](p)
        out[slug] = ssg.Node{Page: func() templ.Component {
            return templates.BaseLayout(fm.Title, ..., src.ProjectPage(p.HTML))
        }}
    }
    return out
},
```

---

## 4. i18n

Current approach: a parallel `src/i18n-de` package plus a hand-written
`build_german` function that duplicates every page entry under `de/` with a
different prefix and content source. Generalize as:

```go
type Locale struct {
    Code   string           // "de"
    Prefix string           // "/de" — passed through to BaseLayout as today
    Tree   func() ssg.Node  // returns that locale's subtree
}

// BuildLocales merges each locale's Tree() under Children[locale.Code],
// same shape as build_german's (*pages)["de/..."] = ... assignments,
// but data-driven instead of one function per locale.
func BuildLocales(base ssg.Node, locales []Locale) ssg.Node
```

This keeps the current pattern (separate content package per locale, explicit
prefix threading through layouts) — it just removes the need to hand-write a
new `build_<lang>` function for every additional language.

---

## 5. Scripts: TS/WASM compile step (`ssg` package, replaces `compilTS`)

```go
// CompileScripts glob-compiles a scripts directory into outDir.
// Each top-level .go file (or subdirectory, project's choice) with
// //go:build js && wasm becomes GOOS=js GOARCH=wasm go build -o <name>.wasm.
// .ts files still route through tsc for projects not ready to drop them.
func CompileScripts(srcDir, outDir string) error
```

Behavior mirrors current `compilTS`: glob the directory, invoke the right
compiler per extension, write output, return combined stderr on failure.
`Node.CompileFrom` in §2 calls this.

The generated `wasm_exec.js` glue file should be copied into `outDir`
alongside the `.wasm` output every build (Go ships it under
`$(go env GOROOT)/lib/wasm/wasm_exec.js`).

---

## 6. `wasmwrap` package

Goal: `syscall/js` should feel like Go, not like reading JS through a keyhole.
Covers the things you actually do in a browser script — query/create
elements, edit style and attributes, bind events, drive a canvas — with
normal Go types and no promise/callback juggling exposed at the call site.

```go
package wasmwrap

// --- Querying & creation ---

func Query(selector string) Element        // document.querySelector
func QueryAll(selector string) []Element    // querySelectorAll
func Create(tag string) Element             // document.createElement(tag)
func Body() Element
func Doc() Element

type Element struct{ v js.Value }

func (e Element) Value() js.Value  // escape hatch — raw syscall/js, no dead end

// --- Tree manipulation ---

func (e Element) Append(children ...Element) Element  // returns e, chainable
func (e Element) Remove()
func (e Element) Clone() Element

// --- Content & attributes ---

func (e Element) Text() string
func (e Element) SetText(s string) Element
func (e Element) HTML() string
func (e Element) SetHTML(s string) Element
func (e Element) Attr(name string) string
func (e Element) SetAttr(name, value string) Element
func (e Element) Class() ClassList          // .classList-style add/remove/toggle/has

// --- Style ---
// A struct instead of string-key/value pairs, so common properties get
// autocomplete and type-checking; anything not modeled falls through Set.

type Style struct{ e Element }
func (e Element) Style() Style
func (s Style) Set(prop, value string) Style   // fallback for anything not below
func (s Style) Display(v string) Style
func (s Style) Color(v string) Style
func (s Style) Background(v string) Style
func (s Style) Size(width, height string) Style
func (s Style) Position(top, left string) Style

// --- Events ---

func (e Element) On(event string, fn func(Event)) (unsubscribe func())

type Event struct{ v js.Value }
func (ev Event) PreventDefault()
func (ev Event) StopPropagation()
func (ev Event) Target() Element
func (ev Event) Key() string      // for keyboard events
func (ev Event) Pos() (x, y int)  // for mouse/pointer events

// --- Timers / async, wrapping the awkward JS-callback and Promise patterns ---

func SetTimeout(fn func(), ms int) (cancel func())
func SetInterval(fn func(), ms int) (cancel func())
func Fetch(url string) ([]byte, error)  // blocks the goroutine, hides the Promise dance

// --- Canvas ---
// The roughest part of raw syscall/js today — every draw call is a
// stringly-typed js.Value.Call. Wrap it as a typed 2D-context API.

type Canvas struct{ e Element }
func (e Element) AsCanvas() Canvas
func (c Canvas) Context2D() Ctx2D

type Ctx2D struct{ v js.Value }
func (c Ctx2D) FillStyle(color string) Ctx2D
func (c Ctx2D) StrokeStyle(color string) Ctx2D
func (c Ctx2D) FillRect(x, y, w, h float64)
func (c Ctx2D) StrokeRect(x, y, w, h float64)
func (c Ctx2D) Clear(x, y, w, h float64)
func (c Ctx2D) BeginPath() Ctx2D
func (c Ctx2D) MoveTo(x, y float64) Ctx2D
func (c Ctx2D) LineTo(x, y float64) Ctx2D
func (c Ctx2D) Arc(x, y, radius, startAngle, endAngle float64) Ctx2D
func (c Ctx2D) Fill() Ctx2D
func (c Ctx2D) Stroke() Ctx2D
func (c Ctx2D) RequestAnimationFrame(fn func(dt float64)) (cancel func())
```

Every method that doesn't need to return a value returns the receiver
(`Element`, `Style`, `Ctx2D`) so calls chain naturally:
`wasmwrap.Query("#box").Style().Color("red").Display("block")`.

Anything not modeled — an obscure API, a browser-specific quirk — drops to
`.Value()` and raw `syscall/js`, so the wrapper is never a dead end, only a
shortcut for the common 90%.

### 6a. Optional reactive layer (opt-in, separate package)

Not part of `wasmwrap` itself — a separate `wasmwrap/reactive` package a
project can import if it wants component-like state management, without
forcing it on projects that just want DOM helpers:

```go
package reactive

type Signal[T any] struct{ ... }
func NewSignal[T any](initial T) *Signal[T]
func (s *Signal[T]) Get() T
func (s *Signal[T]) Set(v T)          // triggers re-render of subscribed views
func (s *Signal[T]) Subscribe(fn func(T)) (unsubscribe func())

// Render re-runs view() whenever any Signal it reads changes, and does a
// minimal DOM patch (not a virtual-DOM diff of a whole tree — just re-set
// the text/attrs that changed) rather than replacing nodes wholesale.
func Render(root wasmwrap.Element, view func() wasmwrap.Element)
```

Deliberately small — signals + targeted DOM patching, not a full component
model with lifecycle hooks, routing, or JSX-like templating. A project either
imports it for interactive widgets, or ignores it entirely and drives
`wasmwrap.Element` directly for simple scripts (the common case: a nav toggle,
a form validator, a canvas animation).

---

## 7. `starter/` templates (copied, not imported)

A folder of small, single-purpose `.templ` files a new project copies into
its own `templates/`/`src/` and edits directly. None of them are imported as
a library — the goal is a head start on boilerplate, not a base class you're
locked into. Grouped by purpose:

```
starter/
  layout/
    base_layout.templ
    nav.templ
    footer.templ
  content/
    collection_index.templ
    collection_item.templ
    tag_list.templ
  pages/
    home.templ
    about.templ
    not_found.templ
  wasm/
    wasm_script.templ
    canvas_host.templ
  meta/
    seo_head.templ
    rss_feed.templ
```

**`layout/`**

- `base_layout.templ` — generalized version of the current `BaseLayout`:
  title, path, prefix, and content passed via templ's built-in `{ children... }`
  mechanism rather than a `body templ.Component` argument — so pages wrap
  content Astro-style (`@BaseLayout(title, path, prefix) { <h1>...</h1> }`)
  instead of passing it in as a parameter. Owns `<html>`/`<head>`/`<body>`
  structure and locale-aware `<html lang>`.
- `nav.templ` — a nav bar driven by a `[]NavLink{Label, Path}` slice instead
  of hard-coded links, so adding a page doesn't mean editing markup in two
  places.
- `footer.templ` — copyright line, locale switcher links, social/contact
  links — whatever's genuinely repeated across every page.

**`content/`** — the "list + detail" pattern generalized from
`src.Project`/`src.ProjectTile`/`src.ProjectPage`, since blog posts, project
portfolios, changelogs, and photo galleries are all the same shape:

- `collection_index.templ` — `CollectionIndex(items []Tile) templ.Component`:
  renders a grid/list of tiles, each linking to its detail page. Direct
  generalization of `src.Project`.
- `collection_item.templ` — `Tile(title, description, slug string)` +
  `CollectionItem(title string, body templ.Component)`: the tile-in-a-list
  and the standalone detail page, generalized from `ProjectTile`/`ProjectPage`.
- `tag_list.templ` — optional small component for a `[]string` of tags/
  categories, since front matter commonly carries these and most content
  sites end up wanting a filter or tag-cloud eventually.

**`pages/`** — plain one-off pages that don't need a collection:

- `home.templ`, `about.templ` — direct starting points, near-empty bodies
  meant to be filled in immediately.
- `not_found.templ` — a 404 page; GitHub Pages and most static hosts look
  for a specific filename (`404.html`) at the root, so this doubles as a
  reminder to wire that up in `ssg.Node` (`Page: NotFound` at the tree root
  alongside a `CopyFrom`/`Page` sibling, output named accordingly).

**`wasm/`**

- `wasm_script.templ` — the `WasmScript(name string)` component from earlier:
  emits the `wasm_exec.js` `<script>` tag plus the `instantiateStreaming`
  boilerplate for a named `.wasm` build.
- `canvas_host.templ` — a `<canvas>` element with sensible defaults (fixed
  or responsive sizing attributes) paired with a `WasmScript` call, since
  "canvas + wasm script" is common enough (games, visualizations, drawing
  tools) to warrant its own starter rather than assembling it from the two
  pieces every time.

**`meta/`**

- `seo_head.templ` — `<meta>` tags (description, `og:*`, `twitter:*`) driven
  by the same front-matter fields `content.Page` already parses, so a
  project gets reasonable social-preview cards without hand-writing them
  per page.
- `rss_feed.templ` — an XML feed template over a `[]FeedItem`, for projects
  whose collection pages (blog, changelog) benefit from a feed; entirely
  optional and easy to delete if unused.

None of these are wired into `ssg.Build` — a project's `Node` tree calls them
directly (`Page: func() templ.Component { return layout.BaseLayout(...) }`),
same as today's `RenderHome`/`RenderAbout` functions. The library's only job
is getting them in front of a new project fast; from that point they're
regular project code.

---

## 8. What `main.go` looks like after migration

```go
func main() {
    dev := len(os.Args) > 1 && os.Args[1] == "--dev"

    root := ssg.Node{
        Page: RenderHome,
        Children: map[string]ssg.Node{
            "links":    {Page: RenderLinks},
            "about":    {Page: RenderAbout},
            "goals":    {Page: RenderGoals},
            "projects": {Page: RenderProjectsIndex, Generate: GenerateProjects},
            "style":    {CopyFrom: "style"},
            "public":   {CopyFrom: "public"},
            "scripts":  {CompileFrom: "scripts"},
        },
    }
    root = ssg.BuildLocales(root, []ssg.Locale{
        {Code: "de", Prefix: "/de", Tree: GermanTree},
    })

    ssg.Build(root, ssg.BuildOptions{Dev: dev})
}
```

Everything else (the actual `templ` components, the front-matter struct, the
per-page render functions) stays exactly as it is today — this only replaces
the orchestration in `main.go` / `build_projects.go` / `builders.go`.

---

## 9. Non-goals (explicitly out of scope)

- No virtual DOM, no client-side routing, no full component lifecycle model
  in `wasmwrap` core. The optional `reactive` package (§6a) is a deliberately
  small opt-in exception — targeted DOM patching, not a framework.
- No config file format required — Go struct literals are the primary and
  recommended config; JSON/YAML loading is optional sugar, not core.
- No hidden network calls, code generation, or reflection-based magic in
  `Build`'s hot path — the walker in §2.1 is the entire mechanism.
- No enforced project structure beyond the `Node` tree itself — directory
  names, file layout, and template structure remain entirely up to the
  consuming project.

---

## 10. Build order for implementation

1. `ssg.Node` + `ssg.Build` (§2) — get the existing site rebuilding through
   the tree walker with zero behavior change first.
2. `content.Load`/`LoadDir`/`Decode` (§3) — swap `build_projects.go`'s
   hand-rolled parsing for the generalized version.
3. `ssg.BuildLocales` (§4) — fold `build_german` into the data-driven form.
4. `ssg.CompileScripts` + `wasm_exec.js` copy (§5) — re-enable the currently
   commented-out script step, targeting wasm instead of tsc.
5. `wasmwrap` (§6) — only needed once an actual script is being written.
6. `starter/` templates (§7) — extract last, once the shape has stabilized
   across at least one migration.
7. `wasmwrap/reactive` (§6a) — optional, only if/when a project needs
   interactive widgets beyond what plain `wasmwrap` calls handle cleanly.
