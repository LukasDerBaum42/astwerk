package ssg

import (
	"context"
	"encoding/xml"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/a-h/templ"
)

// URLs returns the site path of every node with a Page, in the order Build
// visits them: "" for the home page, "about/", "de/about/" inside a locale
// subtree. Generated children are included, so URLs reflects the tree Build
// actually writes.
//
// URLs calls Generate. If Generate does expensive work (loading a collection,
// for example), that work runs once here and again in Build.
func URLs(root Node) []string {
	var paths []string
	var walk func(Node, string)
	walk = func(n Node, path string) {
		if n.Page != nil {
			paths = append(paths, path)
		}
		children := make(map[string]Node, len(n.Children))
		maps.Copy(children, n.Children)
		if n.Generate != nil {
			maps.Copy(children, n.Generate())
		}
		for _, key := range slices.Sorted(maps.Keys(children)) {
			walk(children[key], path+key+"/")
		}
	}
	walk(root, "")
	return paths
}

// SitemapOptions configures [Sitemap].
type SitemapOptions struct {
	// BaseURL is the production site root, without a trailing slash:
	// "https://example.com" or "https://user.github.io/astwerk". It is the
	// full origin plus base path, not BuildOptions.BaseURL (which is only the
	// deployment path).
	BaseURL string
}

// Sitemap renders a sitemap.xml for the given site paths, usually
// [URLs](root). Put it in the node's Files:
//
//	tree.Files = map[string]ssg.PageFunc{
//		"sitemap.xml": ssg.Static(func() templ.Component {
//			return ssg.Sitemap(ssg.URLs(tree), ssg.SitemapOptions{
//				BaseURL: "https://example.com",
//			})
//		}),
//	}
//
// Only pages (nodes with Page) belong in a sitemap, so a sitemap stored in
// Files does not list itself. v1 emits <loc> only: no lastmod, changefreq,
// priority or locale alternates.
func Sitemap(urls []string, opts SitemapOptions) templ.Component {
	base := strings.TrimSuffix(opts.BaseURL, "/")
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`+"\n"); err != nil {
			return err
		}
		for _, path := range urls {
			loc := base + "/" + strings.TrimPrefix(path, "/")
			if _, err := io.WriteString(w, "  <url><loc>"); err != nil {
				return err
			}
			if err := xml.EscapeText(w, []byte(loc)); err != nil {
				return err
			}
			if _, err := io.WriteString(w, "</loc></url>\n"); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, "</urlset>\n")
		return err
	})
}
