// Package ssg is a minimal, no-magic static site generator for Go + templ.
//
// The whole mechanism is one tree walker: describe the output filesystem as a
// [Node] tree and call [Build]. Every field of Node is optional, and anything
// the tree can't express falls back to plain Go — there is no wall you can't
// get past.
package ssg

import "github.com/a-h/templ"

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
}

// URL is Path as an absolute, prefix-aware URL: "/", "/about/", "/de/about/".
func (c Ctx) URL() string {
	return "/" + c.Path
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
}
