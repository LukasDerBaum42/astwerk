+++
Title = "Content & collections"
Description = "Markdown with TOML front matter, and the list-plus-detail pattern."
Order = 30
+++

The `content` package reads markdown files with TOML front matter. Combined with
`Node.Generate`, that gives you a collection: one index page plus one page per
file.

## The API

```go
type Page struct {
	Meta map[string]any // front matter between +++ markers
	HTML string         // rendered markdown body
}

func Load(path string) (Page, error)
func Parse(src string) (Page, error)
func LoadDir(dir string) (map[string]Page, error)
func Slugs(pages map[string]Page) []string
func Decode[T any](p Page) (T, error)
```

`LoadDir` is non-recursive and reads `*.md` only, keyed by filename without the
extension — the slug.

## Front matter is yours

`Meta` is deliberately a generic map, not a fixed struct. Every site wants
different fields, so `Decode` unmarshals into whatever struct you define:

```go
type FrontMatter struct {
	Title       string
	Description string
	Tags        []string
	Draft       bool
}

fm, err := content.Decode[FrontMatter](page)
```

A file looks like this:

```markdown
+++
Title = "Dice Dungeon"
Description = "A roguelike built in a week"
Tags = ["game", "go"]
+++

# Dice Dungeon

Real markdown from here on, and <span>inline HTML</span> passes through.
```

A page with no front matter decodes to the zero value without error, so an
undecorated markdown file is still valid.

## Always range over Slugs

`LoadDir` returns a map, and Go randomises map iteration order. Range it
directly and your index page reshuffles on every build, producing a diff for no
reason:

```go
// Wrong — order changes every build:
for slug, p := range pages { … }

// Right:
for _, slug := range content.Slugs(pages) {
	p := pages[slug]
	…
}
```

This is worth the explicit call. It was found the hard way: the first site
migrated to astwerk produced byte-identical output on every page *except* the
collection index, which shuffled its tiles each run.

## A collection

`Generate` runs at build time and returns children to merge in. Point it at a
content directory:

```go
func projects(dir string) ssg.Node {
	pages, err := content.LoadDir(dir)
	if err != nil {
		log.Fatal(err)
	}

	var tiles []templ.Component
	children := map[string]ssg.Node{}

	for _, slug := range content.Slugs(pages) {
		fm, _ := content.Decode[FrontMatter](pages[slug])
		body := pages[slug].HTML

		children[slug] = ssg.Node{
			Title: fm.Title,
			Page: func(c ssg.Ctx) templ.Component {
				return views.ProjectPage(c.Title, c.Path, c.Prefix, body)
			},
		}
		tiles = append(tiles, views.Tile(fm.Title, fm.Description, slug))
	}

	return ssg.Node{
		Title: "Projects",
		Page: func(c ssg.Ctx) templ.Component {
			return views.ProjectIndex(c.Title, c.Path, c.Prefix, tiles)
		},
		Children: children,
	}
}
```

Use it as one line in the tree:

```go
"projects": projects("content/projects"),
```

Notice there is no path arithmetic. Each child gets its path from the walker, so
adding a markdown file adds a page and a tile with nothing else to update.

Tile links should be bare slugs — `href="thing/"` — so they resolve relative to
whichever index page they're on. The same tile then works at `/projects/` and
`/de/projects/` with no prefix threading.

## Markdown rendering

By default, goldmark with raw HTML enabled, so a page can drop into hand-written
markup where markdown runs out.

To add extensions, build your own goldmark and pass it to a `Loader`:

```go
md := goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		highlighting.NewHighlighting(
			highlighting.WithStyle("github"),
			highlighting.WithFormatOptions(chromahtml.WithClasses(true)),
		),
	),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

pages, err := content.NewLoader(md).LoadDir("content/docs")
```

The docs you're reading are rendered exactly that way. Highlighting happens at
build time via chroma, so no JavaScript highlighter ships to your browser — the
`<span>` classes in the code blocks above are in the HTML.
