// Command site builds the astwerk documentation site.
//
// It is a nested module with a replace directive pointing at the parent, so it
// always builds against the working tree rather than a published tag. That makes
// it a dogfooding check: if a change breaks the library's own site, the site
// stops building.
//
//	go run .              # build into build/
//	go run . --dev        # keep the existing output, overwrite in place
//	go run . --base ""    # build for a domain root instead of /astwerk
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"

	"github.com/LukasDerBaum42/astwerk/content"
	"github.com/LukasDerBaum42/astwerk/site/pages"
	"github.com/LukasDerBaum42/astwerk/site/templates"
	"github.com/LukasDerBaum42/astwerk/ssg"
	"github.com/a-h/templ"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)

// DefaultBase is where GitHub project Pages serve this repo from. Override it
// with -base "" if the site ever moves to a domain root.
const DefaultBase = "/astwerk"

// FrontMatter is the shape of the TOML block in content/docs/*.md.
type FrontMatter struct {
	Title       string
	Description string
	Order       int
}

// doc is one loaded documentation page.
type doc struct {
	Slug string
	FM   FrontMatter
	HTML string
}

func main() {
	dev := flag.Bool("dev", false, "keep the existing build directory and overwrite in place")
	base := flag.String("base", DefaultBase, "path the site is served from")
	out := flag.String("out", "build", "output directory")
	flag.Parse()

	n, err := buildSite(*out, *base, *dev)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("built %d pages into %s/ (relative URLs, canonical base %q)", n, *out, *base)
}

// buildSite generates the site and returns how many pages it wrote. Separated
// from main so tests can build into a temp directory and inspect the output.
func buildSite(outDir, base string, dev bool) (int, error) {
	docs, err := loadDocs("content/docs")
	if err != nil {
		return 0, err
	}
	sidebar := make([]templates.NavItem, 0, len(docs))
	summaries := make([]pages.DocSummary, 0, len(docs))
	for _, d := range docs {
		sidebar = append(sidebar, templates.NavItem{Label: d.FM.Title, Path: d.Path()})
		summaries = append(summaries, pages.DocSummary{
			Title: d.FM.Title, Description: d.FM.Description, Path: d.Path(),
		})
	}

	root := ssg.Node{
		Title: "astwerk",
		Page:  func(c ssg.Ctx) templ.Component { return pages.Home(c) },
		Files: map[string]ssg.PageFunc{
			// GitHub Pages runs Jekyll unless told not to, and Jekyll drops files
			// and directories beginning with an underscore.
			".nojekyll": empty,
			"404.html":  func(c ssg.Ctx) templ.Component { return pages.NotFound(c) },
		},
		Children: map[string]ssg.Node{
			"docs": {
				Title: "Documentation",
				Page: func(c ssg.Ctx) templ.Component {
					return pages.DocsIndex(c, sidebar, summaries)
				},
				Generate: func() map[string]ssg.Node { return docNodes(docs, sidebar) },
			},
			"examples": {
				Title: "Examples",
				Page:  func(c ssg.Ctx) templ.Component { return pages.Examples(c) },
			},
			"style":   {CopyFrom: "style"},
			"public":  {CopyFrom: "public"},
			"scripts": {CompileFrom: "scripts"},
		},
	}

	if err := ssg.Build(root, ssg.BuildOptions{
		OutDir: outDir,
		Dev:    dev,
		// BaseURL only feeds Ctx.URL, for canonical tags. Every in-page link and
		// asset is relative, so this output works opened from disk, served at a
		// localhost root, and deployed under /astwerk/ — without a rebuild.
		BaseURL:      base,
		RelativeURLs: true,
	}); err != nil {
		return 0, err
	}
	return len(docs) + 3, nil
}

// docNodes turns loaded markdown into one Node per page, wiring up the
// previous/next pager from the sidebar order.
func docNodes(docs []doc, sidebar []templates.NavItem) map[string]ssg.Node {
	out := make(map[string]ssg.Node, len(docs))

	for i, d := range docs {
		var prev, next *templates.NavItem
		if i > 0 {
			prev = &sidebar[i-1]
		}
		if i < len(docs)-1 {
			next = &sidebar[i+1]
		}

		body, desc := d.HTML, d.FM.Description
		out[d.Slug] = ssg.Node{
			Title: d.FM.Title,
			Page: func(c ssg.Ctx) templ.Component {
				return pages.DocPage(c, sidebar, desc, body, prev, next)
			},
		}
	}
	return out
}

// loadDocs reads the docs directory and returns pages in sidebar order.
func loadDocs(dir string) ([]doc, error) {
	pages, err := content.NewLoader(markdown()).LoadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("no markdown found in %s", dir)
	}

	// Slugs gives a stable alphabetical base; Order then decides the reading
	// sequence, so two pages sharing an Order still come out deterministically.
	docs := make([]doc, 0, len(pages))
	for _, slug := range content.Slugs(pages) {
		fm, err := content.Decode[FrontMatter](pages[slug])
		if err != nil {
			return nil, fmt.Errorf("front matter in %s/%s.md: %w", dir, slug, err)
		}
		if fm.Title == "" {
			return nil, fmt.Errorf("%s/%s.md has no Title in its front matter", dir, slug)
		}
		docs = append(docs, doc{Slug: slug, FM: fm, HTML: pages[slug].HTML})
	}

	for i := 1; i < len(docs); i++ {
		for j := i; j > 0 && docs[j].FM.Order < docs[j-1].FM.Order; j-- {
			docs[j], docs[j-1] = docs[j-1], docs[j]
		}
	}
	return docs, nil
}

// Path is the doc's site path, matching where the tree puts it.
func (d doc) Path() string { return "docs/" + d.Slug + "/" }

// markdown is the renderer for docs pages: syntax highlighting via chroma,
// GitHub-flavoured tables and strikethrough, anchored headings, and raw HTML
// passed through so a page can drop into markup.
func markdown() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			highlighting.NewHighlighting(
				highlighting.WithStyle(templates.StyleName),
				// Classes, not inline styles, so style/code.css drives both these
				// blocks and the ones views.HighlightGo emits.
				highlighting.WithFormatOptions(chromahtml.WithClasses(true)),
			),
		),
		goldmark.WithRendererOptions(goldmarkhtml.WithUnsafe()),
	)
}

// empty writes nothing, for marker files like .nojekyll that must exist but
// stay empty.
func empty(ssg.Ctx) templ.Component {
	return templ.ComponentFunc(func(context.Context, io.Writer) error { return nil })
}
