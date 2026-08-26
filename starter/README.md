# starter/

Boilerplate to copy, not a library to import. Nothing here is referenced by
`ssg` or `content` — once these files are in your project they're regular
project code, and you own them.

MIT licensed like the rest of astwerk: copy and edit freely, no attribution
burden beyond keeping the header.

## Using it

```bash
cp -r $(go env GOPATH)/pkg/mod/github.com/\!lukas\!der\!baum42/astwerk*/starter/* ./
# or just download the folder from GitHub
```

Then replace the placeholder import path in `pages/` and `content/`:

```bash
grep -rl example.com/yoursite . | xargs sed -i 's|example.com/yoursite|YOUR/MODULE/PATH|g'
templ generate
```

Keep the directories as separate Go packages, or flatten them into one — the
only cross-package references are `pages/` and `content/` importing `layout`.

Copy the files out before running `templ generate`. Generating in place inside
the astwerk checkout produces `_templ.go` files that import the placeholder
path, which breaks `go build ./...` there — `.gitignore` keeps them out of git,
but the local build still fails until you delete them.

## What's here

| | |
| --- | --- |
| `layout/base_layout.templ` | `<html>`/`<head>`/`<body>`, locale-aware `<html lang>`, content via `{ children... }` |
| `layout/nav.templ` | nav bar driven by a `[]NavLink` slice, marks the active link |
| `layout/footer.templ` | copyright, locale switcher, social links |
| `content/collection_index.templ` | grid of tiles — the "list" half of list + detail |
| `content/collection_item.templ` | `Tile` for the grid, `CollectionItem` for the detail page |
| `content/tag_list.templ` | tags/categories from front matter |
| `pages/home.templ` | front page, near-empty on purpose |
| `pages/about.templ` | a plain one-off page |
| `pages/not_found.templ` | 404 page; see its comment for wiring `404.html` |
| `pages/reactive.templ` | a page of `widgets/`, wrapped in `x.Document` |
| `widgets/counter.templ` | counter with increment/reset, a toggle-`When` section and a title effect |
| `widgets/todo.templ` | two-way input + keyed list, with the script escape hatch for add |
| `widgets/price.templ` | computed value over two two-way number inputs |
| `wasm/wasm_script.templ` | loads a `.wasm` build plus Go's `wasm_exec.js` glue |
| `wasm/canvas_host.templ` | sized `<canvas>` paired with its script |
| `meta/seo_head.templ` | description, Open Graph and Twitter tags |
| `meta/rss_feed.templ` | RSS 2.0 feed over a `[]FeedItem` |

## The reactive widgets

`widgets/` uses `astwerk/x` — compiled reactive markup: the Go functions in the
widgets render both the static HTML and the small JavaScript that makes it
live, with no WASM and no framework. The x package is a real import
(`github.com/LukasDerBaum42/astwerk/x`), so unlike the placeholder paths it
needs no rewriting.

Three rules to keep in mind:

- **Wrap the page content in `x.Document`.** That is what emits the runtime and
  state declarations before the markup and the bindings after it (see
  `pages/reactive.templ`). Outside a Document the widgets still render — just
  statically, which is what makes reactivity opt-in.
- **Name anything a script reads.** `x.Named("count", 0)` keeps that name in
  the generated JavaScript, so `<script>` bodies and `x.Effect` blocks can read
  the signal directly (`count.get()`). Auto-named signals (s0, s1…) are fine
  for bindings, which never need to know the name.
- **The script is the escape hatch.** Combinators cover increment, toggle,
  reset and arithmetic; anything more — appending to a list, validation,
  fetch — is a `<script>` reading the named signals (see
  `widgets/todo.templ`), never a new combinator.

## Why these shapes

**Pages take `(title, path, prefix)`.** That's exactly what `ssg.Templ` fills
in, so a page written this way drops into a tree with no adapter:

```go
"about": {Title: "About", Page: ssg.Templ(pages.About)},
```

`path` and `prefix` come from the walker. You never pass them by hand.

**Tile links are bare slugs** (`href="thing/"`), so they resolve relative to
whatever index page they're on. The same tile works at `/projects/` and
`/de/projects/` with no prefix threading.

**`rss_feed.templ` marshals with `encoding/xml`** instead of writing markup.
`<link>` is a void element in HTML, and templ's parser is HTML-aware — written
as markup it self-closes, producing a feed whose items have no links. Marshalling
avoids that and handles escaping.

**`wasm_script.templ` passes the path via `data-wasm`.** templ treats `<script>`
contents as raw text, so an interpolated `{ name }` in the script body would be
emitted literally rather than substituted.
