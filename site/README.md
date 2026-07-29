# site/

The astwerk documentation site, built with astwerk.

It is a **nested Go module** with a `replace` pointing at the parent, so it always
builds against the working tree rather than a published tag. That makes it a
dogfooding check: if a change breaks the library's own documentation site, the
site stops building. It also keeps the site's dependencies — chroma, goldmark
extensions — out of the library's `go.mod`, so consumers never see them.

## Building

```bash
templ generate          # or: go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate
go run .                # into build/, base path /astwerk
go run . -dev           # keep existing output, overwrite in place
go run . -base ""       # for a domain root instead of /astwerk
```

## Previewing

Every in-page URL is relative (`RelativeURLs: true`), so the output is portable —
serve it any way you like:

```bash
cd build && python3 -m http.server 8000   # http://localhost:8000/
```

Opening `build/index.html` straight off disk works too. So does serving it under
`/astwerk/`. Same build, no flags.

`BaseURL` is still set, but only feeds `Ctx.URL` for canonical tags and Open
Graph, which have to be absolute.

## Layout

Following the same split as the original site: `templates/` for the shared shell,
and page bodies in their own package.

| | |
| --- | --- |
| `main.go` | the Node tree, markdown config, docs loading |
| `templates/` | the shell — `Layout`, `CodeBlock`, `WasmScript`, `NavItem` |
| `pages/` | page bodies — `Home`, `DocPage`, `DocsIndex`, `Examples`, `NotFound` |
| `content/docs/*.md` | the documentation, ordered by `Order` in front matter |
| `scripts/demo.go` | the live demos, compiled to wasm by `CompileFrom` |
| `style/style.css` | hand-written, no preprocessor |
| `style/code.css` | **generated** — see below |
| `tools/gencss` | regenerates `style/code.css` from chroma |
| `site_test.go` | builds the site and validates every link resolves |

`pages` imports `templates`; nothing goes the other way.

## Regenerating the syntax highlighting CSS

```bash
go run tools/gencss/main.go > style/code.css
```

The highlighter emits classes rather than inline styles, so one stylesheet covers
both markdown code blocks and templ-embedded snippets. Generating it means the
colours come from the same chroma styles the formatter uses instead of being
copied by hand.

## Two things that will bite

**Don't name a templ parameter `ctx`.** templ's generated code declares its own
`ctx context.Context`; a parameter with that name collides and the generated file
won't compile. Every component here takes `c ssg.Ctx`.

**Don't write absolute URLs as literals.** Use `c.Asset` for assets and `c.Link`
for pages. `site_test.go` walks every generated page and resolves each link
against the directory it came from, so an absolute or broken URL fails the build.

## Adding a page

Drop a markdown file in `content/docs/` with front matter:

```markdown
+++
Title = "My page"
Description = "One line for the index and the sidebar."
Order = 45
+++
```

`Order` decides the sidebar and pager sequence. Nothing else to wire up — the
tree generates the node, the sidebar, the index entry and the prev/next links.
