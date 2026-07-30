package ssg

import (
	"maps"
	"strings"
)

// Locale describes one language of a site.
//
// A locale's subtree is derived from the base tree, not written out again: for
// every page in the base tree, [BuildLocales] mounts the same page under the
// locale's directory with Ctx.Prefix and Ctx.Locale set. Only the pages that
// genuinely differ — a translated home page, a different content directory —
// need an entry in Override.
type Locale struct {
	// Code is the language code and the directory the locale is mounted at
	// ("de" -> /de/...). It is passed to pages as Ctx.Locale.
	Code string

	// Prefix is the URL prefix for that directory ("/de"), passed to pages as
	// Ctx.Prefix. Defaults to "/" + Code.
	Prefix string

	// Override patches nodes in the derived subtree, keyed by path relative to
	// the locale root: "" is the locale's home page, "projects" its projects
	// node, "a/b" a nested one.
	//
	// Each function receives the node BuildLocales would otherwise have derived
	// at that path — same Title, Page, Files, Generate and Children — and
	// returns what to use instead. Changing one field is one line, and nothing
	// you don't touch is lost:
	//
	//	"about": func(n ssg.Node) ssg.Node {
	//		n.Title = "Über mich"
	//		return n
	//	},
	//
	// Replacing the page wholesale is the same shape, just assigning over more
	// fields. A key with no counterpart in the base tree gets the zero Node, so
	// a locale-only page is written out in full.
	Override map[string]func(base Node) Node
}

// BuildLocales returns a copy of base with each locale mounted under
// Children[locale.Code].
//
// The derived subtree carries over Title, Page, Files, Generate and Children
// from the base tree. It deliberately does not carry over CopyFrom or
// CompileFrom: stylesheets, images and scripts are shared by every language and
// are served from the site root, so copying them per locale would just
// duplicate the output. A locale that genuinely needs its own asset — a
// localized screenshot — can set the field back in an Override. Branches that
// end up with nothing to render are dropped.
//
// Overrides are applied shallowest-first, so an override of "projects" is in
// place before one of "projects/alpha" sees its parent.
//
// base is not modified.
func BuildLocales(base Node, locales []Locale) Node {
	children := make(map[string]Node, len(base.Children)+len(locales))
	maps.Copy(children, base.Children)

	for _, l := range locales {
		if l.Code == "" {
			continue
		}
		prefix := l.Prefix
		if prefix == "" {
			prefix = "/" + l.Code
		}

		sub, _ := derive(base)
		for _, path := range sortedKeys(l.Override) {
			fn := l.Override[path]
			if fn == nil {
				continue
			}
			sub = override(sub, splitPath(path), fn)
		}
		sub.locale = &localeCtx{code: l.Code, prefix: prefix}

		children[l.Code] = sub
	}

	base.Children = children
	base.locale = nil
	return base
}

// derive copies the page-bearing parts of n, dropping asset steps. The bool
// reports whether anything survived.
func derive(n Node) (Node, bool) {
	out := Node{
		Title:    n.Title,
		Page:     n.Page,
		Generate: n.Generate,
	}
	if len(n.Files) > 0 {
		out.Files = maps.Clone(n.Files)
	}

	for key, child := range n.Children {
		sub, ok := derive(child)
		if !ok {
			continue
		}
		if out.Children == nil {
			out.Children = map[string]Node{}
		}
		out.Children[key] = sub
	}

	live := out.Page != nil || len(out.Files) > 0 || out.Generate != nil || len(out.Children) > 0
	return out, live
}

// override applies fn to the node at path within n, creating intermediate nodes
// as needed. An empty path patches n itself.
func override(n Node, path []string, fn func(Node) Node) Node {
	if len(path) == 0 {
		return fn(n)
	}

	children := map[string]Node{}
	maps.Copy(children, n.Children)
	children[path[0]] = override(children[path[0]], path[1:], fn)
	n.Children = children
	return n
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func sortedKeys[V any](m map[string]V) []string {
	// Shallow paths first, so "projects" is applied before "projects/foo".
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && depth(keys[j]) < depth(keys[j-1]); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func depth(p string) int { return len(splitPath(p)) }
