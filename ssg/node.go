// Package ssg is a minimal, no-magic static site generator for Go + templ.
//
// The whole mechanism is one tree walker: describe the output filesystem as a
// [Node] tree and call [Build]. Every field of Node is optional, and anything
// the tree can't express falls back to plain Go — there is no wall you can't
// get past.
package ssg

import "github.com/a-h/templ"

// Node is one entry in the output filesystem tree. Every field is optional;
// the zero value means "nothing to do here except recurse into Children."
type Node struct {
	// Page renders this directory's index.html.
	Page func() templ.Component

	// Files renders additional files into this directory, keyed by file name
	// ("404.html", "rss.xml"). Keys may contain slashes to nest ("feed/rss.xml");
	// intermediate directories are created. Written after Page, so a key of
	// "index.html" overwrites Page's output.
	Files map[string]func() templ.Component

	// Generate returns children built at build time (e.g. one per content
	// file). The returned map is merged into Children before recursing, and
	// generated keys win on collision.
	Generate func() map[string]Node

	// CopyFrom copies a directory's contents into this node's directory as-is.
	CopyFrom string

	// CompileFrom compiles a scripts directory (.go -> wasm, or .ts) into this
	// node's directory. See [CompileScripts].
	CompileFrom string

	// Children are static subdirectories, keyed by directory name.
	Children map[string]Node
}

// BuildOptions configures a [Build] run.
type BuildOptions struct {
	// OutDir is the output directory. Defaults to "build".
	OutDir string

	// Dev skips the full clean of OutDir and overwrites files in place,
	// so a running dev server keeps its inodes and open handles.
	Dev bool
}
