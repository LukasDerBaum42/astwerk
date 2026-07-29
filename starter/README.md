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
| `wasm/wasm_script.templ` | loads a `.wasm` build plus Go's `wasm_exec.js` glue |
| `wasm/canvas_host.templ` | sized `<canvas>` paired with its script |
| `meta/seo_head.templ` | description, Open Graph and Twitter tags |
| `meta/rss_feed.templ` | RSS 2.0 feed over a `[]FeedItem` |

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
