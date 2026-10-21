package ssg

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func testPage() PageFunc {
	return Static(func() templ.Component { return templ.Raw("") })
}

func TestURLs(t *testing.T) {
	root := Node{
		Page: testPage(),
		Children: map[string]Node{
			"about": {Page: testPage()},
			"updates": {
				Generate: func() map[string]Node {
					return map[string]Node{
						"one": {Page: testPage()},
						"two": {Page: testPage()},
					}
				},
			},
			"assets": {CopyFrom: "public/assets"},
		},
	}

	want := []string{"", "about/", "updates/one/", "updates/two/"}
	if got := URLs(root); !reflect.DeepEqual(got, want) {
		t.Errorf("URLs = %v, want %v", got, want)
	}
}

func TestSitemap(t *testing.T) {
	c := Sitemap([]string{"", "about/", "updates/one/"}, SitemapOptions{
		BaseURL: "https://example.com/",
	})

	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	got := b.String()

	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`,
		`<loc>https://example.com/</loc>`,
		`<loc>https://example.com/about/</loc>`,
		`<loc>https://example.com/updates/one/</loc>`,
		`</urlset>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("sitemap missing %q:\n%s", want, got)
		}
	}
}

func TestSitemapEscapesURLs(t *testing.T) {
	c := Sitemap([]string{"a&b/"}, SitemapOptions{BaseURL: "https://example.com"})

	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `<loc>https://example.com/a&amp;b/</loc>`) {
		t.Errorf("sitemap = %s", b.String())
	}
}
