# astwerk — Spec Sheet

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
astwerk/
  ssg/          // core: tree walker, Node, Build()
  content/      // markdown + front matter parsing
  wasmwrap/     // syscall/js ergonomics wrapper
  starter/      // copyable starter .templ files (not imported, copied)
```

Consumers `go get github.com/LukasDerBaum42/astwerk` and import `ssg`, `content`,
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
- No asset fingerprinting, cache headers, or deploy pipeline. `Build` stops at
  `OutDir`; what serves it and how long it caches is a hosting decision, not
  the walker's.

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

---

## 11. Future directions (post-implementation)

§1–§10 describe the original design; the module has since been built and its
API has moved on in places (see the README for what's current — `Node.Files`,
`Ctx`, `Locale.Override` rather than `Tree`, `reactive` as a full package
rather than the §6a sketch). This section is the next layer: gaps found by
actually using the library, filtered against the same design law as
everything above — an addition earns its place only if it removes real
repetition, not if it just adds a knob.

**Status.** §11.1 and §11.2 are built, as are three of §11.3's items —
recursive content loading with a pagination helper, the exported DOM test
harness, and parallel builds. What each shipped as is recorded inline below.
Still open: the remaining §11.3 items, §11.4 (measure first), and §12.

### 11.1 Locale overrides should patch a field, not replace a node — **built**

The current `Locale.Override map[string]Node` (`ssg/locale.go`) replaces the
whole node at that path. That was a deliberate call at the time — the
alternative on the table was a reflection-based field merge, which is exactly
the kind of magic §9 rules out. But it has a real cost in practice: changing
just a `Title` for one locale means re-specifying `Page` (and `Children`, if
any) alongside it, or the override silently drops them. Nothing about that is
wrong, but it's boilerplate the override map exists specifically to avoid, and
it will make Override a recurring papercut every time a locale needs one
different string.

The fix that doesn't reintroduce reflection: make `Override`'s value a
function over the base node instead of the node itself, so the override sees
what it would otherwise have to restate and only changes what it means to
change.

```go
type Locale struct {
    Code     string
    Prefix   string
    Override map[string]func(base Node) Node
}
```

```go
root = ssg.BuildLocales(root, []ssg.Locale{{
    Code: "de",
    Override: map[string]func(ssg.Node) ssg.Node{
        "about": func(n ssg.Node) ssg.Node {
            n.Title = "Über mich"
            return n
        },
    },
}})
```

`base` is the node `BuildLocales` would otherwise have derived at that path —
same `Page`, `Children` and `Generate` it has today. Changing one field is one
line; replacing the page function or dropping children wholesale is still
possible (assign over the field), it's just no longer the *only* option. No
reflection, no field-name matching, no merge semantics to document — it's a
plain Go function doing exactly what it says. The locale root's current
special case (`""` keeps inherited children unless the replacement brings its
own) becomes the ordinary case instead of an exception, since `base` already
carries those children into every override.

This is the one item in this section worth treating as a real target rather
than a maybe — it's a small, mechanical change to `ssg/locale.go` and the call
sites in the docs site's locale usage (currently none, but the docs and
README examples need updating alongside it). The docs site has since moved to
its own repository, `astwerk-website`.

**Shipped as written.** `Override` is now `map[string]func(base Node) Node`, a
clean break with no compatibility field. The locale-root special case is gone
as predicted — `base` carries the inherited children into every override, so
there was nothing left for the exception to do. One unplanned bonus: because an
override can now set `CopyFrom`/`CompileFrom` back onto a derived node, the
"locale asset overrides are all-or-nothing" gap in §11.3 has an answer without
any code of its own.

### 11.2 A dev server, generalized from working prior art — **built**

`BuildOptions.Dev` only skips the destructive clean; there's still no
watch-rebuild-reload loop, so every other change is a manual `go run .` plus a
manual browser refresh. This doesn't need inventing from scratch —
`lukasderbaum42.github.io/devserver.py` already does it (hash-based file
watching over a configurable extension set, a rebuild via shell command, and
live reload over SSE with a script injected before `</body>`) and has been
running against a real astwerk-shaped site. The work here is porting that
approach into Go — so a consumer doesn't need Python on the machine, matching
the "one toolchain" pitch the rest of the library makes — not redesigning the
mechanism. `fsnotify` for the watch side, an `http.Server` for the rest;
`ssg.Build` with `Dev: true` is already the right build call to run on each
change.

**Shipped as `devserver/`, with one deviation: no `fsnotify`.** Polling size
and mtime keeps `go.mod` at its three dependencies, which is the same argument
this section makes for porting the thing into Go at all — and `fsnotify` would
have needed a debounce and a polling fallback regardless, since editors that
save by rename don't emit the event you'd want. The cost is a sub-second delay,
tunable via `Config.Interval`.

Two things the Python original didn't have, both earned by using it:
serving `404.html` for a miss with a 404 status, so the page a visitor really
gets is the page you develop against; and a `builderror` SSE event rendered as
an overlay, because a failed build otherwise leaves stale HTML on screen with
no signal at all.

The package deliberately imports nothing from `ssg` — `Config.Build` is a
`func() error`. That keeps it a general watch-rebuild-reload loop rather than a
second entry point into the walker, and a build that also generates CSS or runs
`templ generate` gets the same loop for free.

**Correction, found immediately by using it.** The first version documented
`Build` as an in-process closure calling `ssg.Build`. That is wrong for the
case this library exists to serve: a running Go program cannot load new code,
so editing a `.templ` re-rendered the components compiled into the binary that
started the server. The watcher fired, the build reported success, the browser
reloaded — and the page was unchanged. Silent and confidently wrong, which is
worse than not detecting the change at all.

The fix is `devserver.Command` and `devserver.Steps`: the build runs as a
subprocess, so it recompiles. `Command` turns a non-zero exit into a build
failure carrying the command's combined output, which means a compiler error
reaches the browser overlay with file and line rather than only the terminal.
The in-process form remains valid, but only when the build reads nothing but
data — markdown, CSS, images — and the doc comments now say so at every point
where someone would reach for it.

This also forced a second fix: the watcher now re-baselines after every build,
so output a build writes inside a watched directory — `templ generate`
producing `*_templ.go` is the obvious case — cannot be mistaken for the next
change and loop forever.

### 11.3 Smaller, more speculative gaps

Lower priority than the two above, and each needs a concrete use case before
it's worth building rather than just naming. Three have since been built; the
rest still want a use case first.

- **`reactive.Router` has no query-string or hash access.** `Params` only
  captures path segments; reading `?page=2` today means parsing
  `location.search` by hand against raw `syscall/js`.
- **`wasmwrap.Fetch`/`FetchJSON` are GET-only.** No method, headers, or body —
  a form that submits data has nowhere to go but raw `syscall/js`.
- **`content.LoadDir` is non-recursive with no pagination helper.** — **built.**
  `LoadTree` walks recursively, keyed by slash-separated relative path
  (`2024/hello`), which drops straight into a `Node` tree because `Children`
  keys already nest on slashes. Pagination shipped as `Chunk[T]` and nothing
  more: turning groups into nodes is three lines that differ per site — where
  page one lives, what the pager looks like — so there was no repetition left
  to abstract past the arithmetic. A pagination *type* would also have had to
  import `ssg`, which `content` must not do.
- **No draft/publish convention.** Every project re-derives
  `if fm.Draft && !dev { continue }` by hand. This may not clear the bar for
  an actual API — it's one line already — but it keeps coming up, which is
  worth a documented pattern even if it never becomes a function.
- **`Signal[T]` has no opt-in custom equality.** `Set` compares via
  `any(a) == any(b)`, recovering to "not equal" for slices/maps/funcs — so a
  signal holding a slice re-runs every subscriber on every `Set`, even when the
  new slice is content-identical. A `NewSignalFunc(initial T, equal func(a, b
  T) bool)` would let a caller opt into cheaper comparisons where it matters.
- **No public DOM test harness for `reactive`.** — **built.** `internal/jsdom`
  moved to a top-level `jsdom` package unchanged, and gained tests of its own
  now that it's API rather than a helper. Its package doc now states the
  compatibility promise explicitly — which selectors it supports, and that
  layout, cascade, focus and paint are permanently out of scope — because the
  risk in exporting a stub is people expecting it to grow into a browser.
- **Builds are single-threaded, with no parallelism and no incremental
  cache.** — **built, in the half that was safe to build.** `CompileScripts`
  is now unconditionally parallel: each `.wasm` is a separate `go build`
  process writing its own file, so no consumer code races. `Build` got
  `BuildOptions.Parallel` as an opt-in instead, because the analysis above is
  right about the walker's own state and silent about the user's: a `Generate`
  closure appending to a captured slice is not safe to move onto another
  goroutine, and turning that into a data race on a routine `go get` would be
  the wrong default. Both collect errors by index and return the lowest one, so
  a parallel failure names the same node a sequential one would. Incremental
  caching remains unbuilt and unwanted.
- **Locale asset overrides are all-or-nothing.** — **resolved by §11.1**, with
  no code of its own: an override function can set `CopyFrom`/`CompileFrom`
  back onto the node it receives, so a locale that needs one localized asset
  says so in one line. The default is still "assets are not inherited".
- **No scaffolding command.** `starter/` (§7) requires a manual
  `grep -rl example.com/yoursite | xargs sed -i` to land in a new project. A
  small `astwerk-new` that copies `starter/` and rewrites the module path in
  one step would remove the one place a new project still needs a shell
  one-liner instead of `go run`.

### 11.4 Client-side runtime size

The complaint that actually matters here isn't a byte count in isolation —
it's that astwerk's whole pitch is a framework *without* the bloat and
abstraction Node-based frameworks carry. The bar is comparative: whatever a
page costs to become interactive should not exceed what an equivalent React,
Vue, Svelte or Astro-island component would cost for the same job. A ~2 MB
floor per script (closer to 5 MB with `reactive`) is suspicious next to that
bar and hasn't been measured against it yet.

**Measure before building anything below.** None of the ideas in this section
have been tried. Before picking one: build a size harness that reports (a) the
compiled size of a bare `wasmwrap` script, (b) the same script pulling in
`reactive`, and (c) each of those again once a candidate fix is applied —
compared side by side with a comparably-featured Svelte/Astro-island bundle
doing the same counter-and-list job. `go tool nm -size` (or an equivalent
weight-by-package pass) on the current binaries would also settle whether the
2→5 MB jump when adding `reactive` is runtime/GC/scheduler cost inherent to
Go, or something surprising getting pulled in through one call site — that
answer changes which fix below is worth pursuing at all.

**TinyGo, gated behind an explicit opt-in.** The most promising lever for the
existing `wasmwrap`/`reactive` code as-is, since it changes the compiler, not
the API — same `syscall/js`-based source either way. It has not been tried
against this codebase. If it works, it needs to ship as a flag a project
chooses (`CompileScripts` gaining a toolchain option, or an env var read at
build time), never a silent default swap — `reactive`'s generics-heavy API and
`Resource`'s blocking-goroutine pattern are exactly the two things TinyGo's
narrower runtime is most likely to handle differently, and a project that
hasn't verified its own scripts against that runtime shouldn't get it for
free on a routine `go get` upgrade.

**A build-time compile target that never ships the Go runtime at all.** This
was the direction under consideration before WASM was picked, and it's worth
taking seriously as a parallel track rather than the last-resort framing it
originally got here — see §12 for the concrete plan. The one-line version:
`reactive`'s actual surface is deliberately narrow — signals, a handful of
`Bind*` calls, keyed-list reconciliation — and that surface already *is* what
a compiler like Svelte's produces internally (direct, targeted DOM writes,
no VDOM). Transpiling that surface straight to calls against a small
hand-written JS runtime, instead of compiling it to WASM carrying the whole
Go runtime, is a materially bigger engineering bet than TinyGo, but it's the
only option on this list that also fixes the authoring model, not just the
byte count.

**Not pursuing right now:**

- **A custom `syscall/js`-replacement ABI.** Considered and set aside —
  it mostly duplicates what TinyGo already buys at the ABI level, costs
  giving up `go build` as the toolchain, and isn't expected to save enough
  on its own to justify hand-wiring every `wasmwrap` call.
- **Brotli pre-compression.** Not urgent. Revisit once the measurement above
  says transfer size, specifically, is the bottleneck — and confirm whatever
  host is serving the file actually negotiates the encoding first, since a
  pre-built `.br` sitting next to the `.wasm` does nothing if the server
  doesn't send `Content-Encoding` for it.
- **`wasm-opt -Oz` (Binaryen) as a build pass.** Still a plausible easy win,
  not yet tried; `-ldflags="-s -w"` alone has already been tried against the
  current binaries with minimal gain, which itself is a data point — the
  weight likely isn't in debug symbols.

### 11.5 Explicitly still not doing this

- **Asset fingerprinting / cache-busting for `CopyFrom`/`CompileFrom`
  output.** Raised and rejected: stable filenames across builds are the
  current behavior on purpose. Content hashing, cache headers and CDN
  invalidation are a deploy pipeline's job, not the walker's — astwerk stops
  at `OutDir` and stays out of how it's served.

---

## 12. `astwerk/x` — compiled reactive markup (proposed)

Not started. This is the plan for the "generate JS" direction named in §11.4,
worked out in more detail because it's the one item on that list that fixes
two separate complaints at once: the megabyte-scale WASM floor, *and* that
`reactive`/`wasmwrap` today force interactive logic into a separate file from
the markup it drives, matched by hand-picked `data-*` selectors that can
silently drift out of sync (the docs site's test suite, now in the
`astwerk-website` repository, has to pin this contract page by page precisely
because nothing catches it at build time otherwise). The design law for this feature specifically: **simple by
default, powerful when needed** — most of what an application actually does
(show a value, react to a click, list some items, show a loading state)
should read like markup with live values in it, JSX-style; WASM stays
available, in full, for the cases that need it, but stops being the only way
to do the common 90%.

### 12.1 What it looks like

Reactive bindings are ordinary-looking templ expressions, written directly
where the value or handler belongs — not a separate function matched to the
markup by name, comment, or signature:

```templ
templ Counter() {
	<div>
		@x.Text(count)
		<button onclick={ x.On(increment) }>+</button>
	</div>
}
```

`count` and `increment` are ordinary Go values in scope — a `*x.Signal[string]`
and a plain `func()` closure — declared wherever the component's other Go code
lives (a `var`/`func` in the same `.templ` file, same as any other templ
component's supporting code today). Nothing here is a new file, a new
directory convention, or a selector to keep in sync by hand; the binding sits
at the exact spot in the markup it affects, same as JSX or a Svelte template.

`x.Text`, `x.On`, and the rest of the small marker set (`x.Attr`, `x.Class`,
`x.Show`, `x.Value`/`x.Checked`/`x.Number` for two-way form fields, `x.List`
for keyed lists) are **plain functions that also work with zero tooling**:
`x.Text(count)` running through an ordinary `templ generate` + `go build`,
with no astwerk transpile step at all, still renders the current value as
static HTML — same as today's "the page reads fine with WebAssembly
disabled" guarantee, just automatic instead of requiring a hand-written
`<noscript>` fallback. The transpile step is additive: it turns those same
call sites into live bindings; skipping it costs interactivity, never
correctness.

### 12.2 How a call site becomes a live binding

Two passes, not one, because assigning a stable DOM anchor and finding the
Go logic to run when it fires are different problems:

1. **Anchor assignment happens at normal SSG render time**, with no new
   parsing. `x.Text`, `x.List` and friends own the element they render (a
   `templ.Component`, called with `@`, exactly like any other templ
   subcomponent) and stamp it with `data-x="N"`, N from a counter scoped to
   one page's render pass. Because a page's SSG render is a single
   deterministic walk, the same markup always gets the same N — no manual
   IDs, no risk of collision, and it costs nothing beyond what rendering the
   page already does.
2. **A build pass extracts the client logic.** After `templ generate`, a
   new tool walks each component's generated Go (`go/ast`/`go/types`, same
   approach as §11.4's transpiler) looking for calls into `astwerk/x` in
   render order, and for each one, transpiles the Go expression or closure
   passed to it — `increment`, `count`'s derivation, whatever a `Bind`-style
   marker's argument is — into JS calling the vendored runtime (§12.4),
   addressed to the matching `data-x="N"`. Render order from this static
   walk has to agree with the runtime counter from pass 1 for the anchors to
   line up; for straight-line markup (no marker inside an `if`/`for` in the
   template) the two are trivially the same sequence. Markers used inside
   conditional or looped markup are the open risk in this design — the
   render-time counter and the static walk can only be kept in sync there
   with real care, and it's the first thing worth a throwaway prototype
   against before committing further, rather than assumed solved.

### 12.3 v1 scope

- **In scope:** `Signal`/`Computed`, `Text`/`Attr`/`Class`/`Show` bindings,
  two-way form fields (`Value`/`Checked`/`Number`), event handlers (`On`),
  keyed lists (`List`), and `Resource` (loading/error/data signals for async
  data) — common enough on ordinary pages that leaving it WASM-only would
  undercut the point of a lightweight default path.
- **Out of scope for v1, WASM-only:** `Router`. History/URL integration is
  its own can of worms independent of the transpiler question, and nothing
  about the rest of this design depends on solving it first.
- **Escape hatch:** an `x.Ref("name")` marker for imperative access to a
  specific element (canvas, focus management, anything that isn't a
  declarative binding) — a small generated typed accessor per component
  (e.g. a `Refs` struct with a `CanvasEl` field) rather than a raw selector
  string, so the escape hatch is still compile-time checked. This sits
  alongside the declarative markers, not instead of them — most of an
  application shouldn't need it.
- **The real escape hatch is WASM, unconditionally.** Anything §12's
  transpiled subset can't express — goroutines, `reflect`, arbitrary stdlib,
  `Router`, raw `wasmwrap`/`syscall/js` access — is a normal `CompileFrom`
  script exactly as it works today. `astwerk/x` is the default because most
  interactivity is simple; nothing about adopting it locks a project out of
  the full `reactive`/`wasmwrap` path for the one part of a page that isn't.

### 12.4 The vendored runtime

A small, hand-written JS file (target: low single-digit KB, not megabytes)
implementing the same semantics as `reactive/signal.go` and `reactive/bind.go`
— signals with dependency tracking, `Effect`, `Batch`, and DOM binding
primitives — committed into the astwerk repo and copied into a site's output
once, by the same `CompileFrom`-style build step, and shared by every page
that uses `astwerk/x`. "Vendored" specifically: no npm, no `package.json`, no
build-time dependency resolution — it's a checked-in file astwerk owns and
ships, read and auditable in one sitting, same spirit as `wasm_exec.js` being
copied in today rather than fetched from anywhere.

### 12.5 Naming

`astwerk/x` is a placeholder, not a decision — a distinct package from
`reactive`/`wasmwrap` (confirmed: the transpiled subset of Go it accepts
genuinely differs from what compiles under full WASM, and pretending they're
the same API would just move the "why doesn't this compile" surprise
somewhere less obvious). Whatever it's actually called should say "the
default, lightweight path" next to `reactive` reading as "the complete,
WASM-backed path" — bikeshed later.

### 12.6 Suggested order of attack

1. **Spike the anchor/extraction mechanism alone**, against nothing more
   than the counter example: `x.Text`, `x.On`, straight-line markup only, no
   conditionals. This is the one piece of the whole design that's genuinely
   novel and unproven — everything else (the transpiled Go subset, the
   vendored runtime's signal semantics) is a smaller, more mechanical version
   of work already done for §11.4 and `reactive` respectively.
2. Once anchors work, build out the v1 transpiled subset (§12.3) against the
   existing Examples page demos as the test corpus — they're already written
   against `reactive`'s API, so porting them to `astwerk/x` is a natural
   acceptance test.
3. Only then decide whether conditional/looped marker placement is worth
   solving, based on how much v1 actually needed it in practice.
