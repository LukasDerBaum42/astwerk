// Package ssg is a minimal, no-magic static site generator for Go + templ.
//
// The whole mechanism is one tree walker: describe the output filesystem as a
// [Node] tree and call [Build]. Every field of Node is optional, and anything
// the tree can't express falls back to plain Go — there is no wall you can't
// get past.
package ssg

import (
	"strings"

	"github.com/a-h/templ"
)

// Ctx is what the walker already knows about a page by the time it renders it.
// It is passed to every page function so a layout never has to be told its own
// location by hand.
type Ctx struct {
	// Title is the node's Title field.
	Title string

	// Path is the page's site path, relative to the site root, with a trailing
	// slash: "" for the home page, "about/", "de/projects/dice-dungeon/".
	Path string

	// Prefix is the locale's URL prefix: "" for the base tree, "/de" inside a
	// locale subtree. See [Locale].
	Prefix string

	// Locale is the locale code: "" for the base tree, "de" inside a locale
	// subtree.
	Locale string

	// Base is the site's base path, from [BuildOptions.BaseURL]. Empty when the
	// site is served from a domain root, "/astwerk" when it is served from a
	// subdirectory — as GitHub project Pages are.
	Base string

	// Relative makes [Ctx.Asset] and [Ctx.Link] emit URLs relative to this page
	// instead of absolute ones, from [BuildOptions.RelativeURLs]. [Ctx.URL] is
	// unaffected and stays absolute.
	Relative bool
}

// URL is this page's absolute URL, always base-prefixed and never relative:
// "/about/", or "/astwerk/about/" under a base path.
//
// Use it where a URL has to be absolute — canonical links, Open Graph tags, feed
// entries. For links between pages use [Ctx.Link], and for assets [Ctx.Asset].
func (c Ctx) URL() string {
	return c.Base + "/" + c.Path
}

// Asset resolves a path relative to the site root into a URL for this page:
// Asset("style/style.css") gives "/astwerk/style/style.css" by default, or
// "../style/style.css" from a one-deep page when relative URLs are on.
//
// Never write an absolute path as a literal in a layout. A site served from a
// subdirectory — every GitHub project Pages site — resolves "/style/style.css"
// against the domain root, which is a different site entirely.
func (c Ctx) Asset(path string) string {
	return c.resolveURL(path)
}

// Link resolves a site path into a URL for this page, applying the current locale
// prefix. Link("about/") is "/about/" in the base tree and "/de/about/" inside a
// German subtree.
func (c Ctx) Link(path string) string {
	return c.resolveURL(joinPrefix(c.Prefix, path))
}

// resolveURL turns a root-relative site path into a URL this page can use:
// either absolute with the base path applied, or relative to the page's own
// depth.
//
// Relative output is what makes a build portable — the same files work opened
// from disk, served at a localhost root, and deployed under /astwerk/, with no
// rebuild. The cost is that URLs are only meaningful from the page that produced
// them, which is why [Ctx.URL] stays absolute.
func (c Ctx) resolveURL(path string) string {
	path = strings.TrimPrefix(path, "/")
	if !c.Relative {
		return c.Base + "/" + path
	}
	if out := strings.Repeat("../", c.depth()) + path; out != "" {
		return out
	}
	// The site root linking to itself: "" is not a usable href.
	return "./"
}

// depth is how many directories deep this page sits, which is how many "../"
// steps reach the site root.
func (c Ctx) depth() int {
	trimmed := strings.Trim(c.Path, "/")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "/") + 1
}

// joinPrefix puts a locale prefix in front of a site path, with no leading slash.
func joinPrefix(prefix, path string) string {
	prefix = strings.Trim(prefix, "/")
	path = strings.TrimPrefix(path, "/")
	if prefix == "" {
		return path
	}
	return prefix + "/" + path
}

// Rel is Path with the locale segment removed, so it names the same page
// independently of language: "about/" both at /about/ and at /de/about/.
func (c Ctx) Rel() string {
	if c.Locale == "" {
		return c.Path
	}
	return strings.TrimPrefix(c.Path, c.Locale+"/")
}

// InLocale returns this page's URL in another locale, given that locale's prefix
// ("" for the base tree, "/de" for German).
//
// This is what a language switcher needs, and it works because a locale subtree
// mirrors the base tree — the same page exists at both paths. Building it by hand
// from Path is a trap: Path already contains the current locale segment, so
// re-prefixing it yields "/de/de/about/".
func (c Ctx) InLocale(prefix string) string {
	return c.resolveURL(joinPrefix(prefix, c.Rel()))
}

// PageFunc renders one page. The walker supplies the Ctx.
type PageFunc func(Ctx) templ.Component

// Templ adapts the common (title, path, prefix) component signature to a
// [PageFunc], so a templ component written as
//
//	templ About(title, path, prefix string) { ... }
//
// can be used directly as a page:
//
//	"about": {Title: "About", Page: ssg.Templ(src.About)}
//
// The walker fills in path and prefix; only the title is yours to give.
func Templ(fn func(title, path, prefix string) templ.Component) PageFunc {
	return func(c Ctx) templ.Component { return fn(c.Title, c.Path, c.Prefix) }
}

// Static adapts a component that needs nothing from the walker.
func Static(fn func() templ.Component) PageFunc {
	return func(Ctx) templ.Component { return fn() }
}

// Node is one entry in the output filesystem tree. Every field is optional;
// the zero value means "nothing to do here except recurse into Children."
type Node struct {
	// Title is passed to Page and Files as Ctx.Title.
	Title string

	// Page renders this directory's index.html.
	Page PageFunc

	// Files renders additional files into this directory, keyed by file name
	// ("404.html", "rss.xml"). Keys may contain slashes to nest ("feed/rss.xml");
	// intermediate directories are created. Written after Page, so a key of
	// "index.html" overwrites Page's output.
	Files map[string]PageFunc

	// Generate returns children built at build time (e.g. one per content
	// file). The returned map is merged into Children before recursing, and
	// generated keys win on collision.
	Generate func() map[string]Node

	// CopyFrom copies a directory's contents into this node's directory as-is.
	// Locale subtrees do not inherit it — assets are shared, not translated.
	CopyFrom string

	// CompileFrom compiles a scripts directory (.go -> wasm, or .ts) into this
	// node's directory. See [CompileScripts]. Locale subtrees do not inherit it.
	CompileFrom string

	// Children are static subdirectories, keyed by directory name.
	Children map[string]Node

	// locale is set by BuildLocales on a locale's root node and inherited by
	// everything below it. nil means "the base tree".
	locale *localeCtx
}

type localeCtx struct {
	code   string
	prefix string
}

// BuildOptions configures a [Build] run.
type BuildOptions struct {
	// OutDir is the output directory. Defaults to "build".
	OutDir string

	// Dev skips the full clean of OutDir and overwrites files in place,
	// so a running dev server keeps its inodes and open handles.
	Dev bool

	// BaseURL is the path the site is served from, when that is not a domain
	// root: "/astwerk" for a GitHub project Pages site at
	// user.github.io/astwerk. It reaches pages as Ctx.Base and is applied by
	// Ctx.URL, Ctx.Asset and Ctx.Link.
	//
	// It does not change the output layout — only the URLs pages generate.
	// A leading slash is added and a trailing one removed, so "astwerk",
	// "/astwerk" and "/astwerk/" all mean the same thing.
	BaseURL string

	// Parallel builds sibling nodes concurrently, at most GOMAXPROCS at a time.
	// Output is identical either way — nodes write to disjoint paths, and a
	// failure still reports the same node first — so the only thing that changes
	// is wall time, and only on a site with enough pages to notice.
	//
	// It is opt-in because it moves your code onto other goroutines: Page, Files
	// and Generate functions must be safe to call concurrently. Rendering templ
	// components is; a Generate closure appending to a captured slice is not.
	Parallel bool

	// RelativeURLs makes Ctx.Asset, Ctx.Link and Ctx.InLocale emit URLs relative
	// to the page rather than absolute ones, so the output works unchanged
	// wherever it is served: opened from disk, at a localhost root, or under a
	// subdirectory. BaseURL is then only used for Ctx.URL.
	//
	// Prefer it unless something needs absolute in-page URLs.
	RelativeURLs bool
}

// normalize returns BaseURL in the one form Ctx.Base uses: either empty, or a
// leading slash with no trailing slash.
func (o BuildOptions) normalizeBase() string {
	b := strings.Trim(o.BaseURL, "/")
	if b == "" {
		return ""
	}
	return "/" + b
}
