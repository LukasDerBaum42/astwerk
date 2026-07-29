package ssg

import "maps"

// Locale describes one language of a site.
type Locale struct {
	// Code is the language code and the directory the locale is mounted at
	// ("de" -> /de/...).
	Code string

	// Prefix is the URL prefix for that directory ("/de"). BuildLocales does
	// not read it — it is here so a project can keep the locale's code, path
	// and tree in one literal and thread Prefix into its own layouts, exactly
	// as a hand-written build_<lang> function does today.
	Prefix string

	// Tree returns that locale's subtree.
	Tree func() Node
}

// BuildLocales returns a copy of base with each locale's Tree() mounted under
// Children[locale.Code]. Later locales win on a duplicate Code, and a locale
// whose Tree is nil is skipped.
//
// base is not modified.
func BuildLocales(base Node, locales []Locale) Node {
	children := make(map[string]Node, len(base.Children)+len(locales))
	maps.Copy(children, base.Children)

	for _, l := range locales {
		if l.Tree == nil || l.Code == "" {
			continue
		}
		children[l.Code] = l.Tree()
	}

	base.Children = children
	return base
}
