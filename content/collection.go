package content

import (
	"fmt"
	"slices"
	"sort"

	"github.com/LukasDerBaum42/astwerk/ssg"
	"github.com/a-h/templ"
)

// Item is one entry in a [Collection]: a page's slug, its front matter decoded
// into T, and the page body. Call [Item.Content] with the page's ssg.Ctx to
// render the body; that is where markdown components run and root-relative
// URLs are resolved.
type Item[T any] struct {
	// Slug is the page's key: the file name without the extension for
	// [LoadCollection], or the path relative to the root directory for
	// [LoadCollectionTree] — "2024/hello" for a nested key.
	Slug string

	// Meta is the front matter decoded into T. A page without front matter
	// yields the zero value of T.
	Meta T

	page Page
}

// Content renders the page body. Pass the page's ssg.Ctx so relative URLs and
// markdown components resolve against the page that shows them.
//
// The item is placed on the context as ctx.Item, so a markdown component can
// read it with [ItemOf] without the call passing the slug or front matter.
func (it Item[T]) Content(ctx ssg.Ctx) templ.Component {
	ctx.Item = it
	return it.page.Render(ctx)
}

// ItemOf returns the content item being rendered, when the page came from a
// collection. Components in that page's markdown can use it to reach the
// current slug and front matter without props:
//
//	it, ok := content.ItemOf[Post](ctx)
func ItemOf[T any](ctx ssg.Ctx) (Item[T], bool) {
	item, ok := ctx.Item.(Item[T])
	return item, ok
}

// Collection is a loaded set of pages as a slice, built by calling
// [LoadCollection] or [LoadCollectionTree] — never by convention, never by
// auto-discovery. The methods return new collections rather than mutating,
// so they chain:
//
//	posts, err := content.LoadCollection[Post]("content/posts")
//	latest := posts.Filter(func(it content.Item[Post]) bool {
//		return !it.Meta.Draft
//	}).Sort(func(a, b content.Item[Post]) bool {
//		return a.Meta.Date.After(b.Meta.Date)
//	})
type Collection[T any] struct {
	Items []Item[T]
}

// LoadCollection reads every *.md file directly inside dir (non-recursive) as a
// [Collection], sorted by slug.
func LoadCollection[T any](dir string) (Collection[T], error) {
	return LoadCollectionWith[T](defaultLoader, dir)
}

// LoadCollectionTree reads every *.md file under dir recursively as a
// [Collection], sorted by slug.
func LoadCollectionTree[T any](dir string) (Collection[T], error) {
	return LoadCollectionTreeWith[T](defaultLoader, dir)
}

// LoadCollectionWith is [LoadCollection] using a specific loader, for custom
// goldmark configurations:
//
//	content.LoadCollectionWith[Post](content.NewLoader(md), "content/posts")
func LoadCollectionWith[T any](l *Loader, dir string) (Collection[T], error) {
	pages, err := l.LoadDir(dir)
	if err != nil {
		return Collection[T]{}, err
	}
	return collectionFrom[T](pages)
}

// LoadCollectionTreeWith is [LoadCollectionTree] using a specific loader, for
// custom goldmark configurations.
func LoadCollectionTreeWith[T any](l *Loader, dir string) (Collection[T], error) {
	pages, err := l.LoadTree(dir)
	if err != nil {
		return Collection[T]{}, err
	}
	return collectionFrom[T](pages)
}

// collectionFrom turns a page map into a collection in sorted slug order,
// decoding each page's front matter into T.
func collectionFrom[T any](pages map[string]Page) (Collection[T], error) {
	items := make([]Item[T], 0, len(pages))
	for _, slug := range Slugs(pages) {
		p := pages[slug]
		meta, err := Decode[T](p)
		if err != nil {
			return Collection[T]{}, fmt.Errorf("content: %s: %w", slug, err)
		}
		items = append(items, Item[T]{Slug: slug, Meta: meta, page: p})
	}
	return Collection[T]{Items: items}, nil
}

// MustLoadCollection is [LoadCollection] for a package-level collection. It
// panics on error, like [MustLoadData]:
//
//	var Posts = content.MustLoadCollectionTree[Post]("content/posts")
func MustLoadCollection[T any](dir string) Collection[T] {
	c, err := LoadCollection[T](dir)
	if err != nil {
		panic(err)
	}
	return c
}

// MustLoadCollectionTree is [LoadCollectionTree] for a package-level collection.
func MustLoadCollectionTree[T any](dir string) Collection[T] {
	c, err := LoadCollectionTree[T](dir)
	if err != nil {
		panic(err)
	}
	return c
}

// Len returns the number of items.
func (c Collection[T]) Len() int { return len(c.Items) }

// Each calls fn for every item in load order.
func (c Collection[T]) Each(fn func(Item[T])) {
	for _, item := range c.Items {
		fn(item)
	}
}

// First returns the first item, or false when the collection is empty.
func (c Collection[T]) First() (Item[T], bool) {
	if len(c.Items) == 0 {
		var zero Item[T]
		return zero, false
	}
	return c.Items[0], true
}

// Get returns the item with the given slug.
func (c Collection[T]) Get(slug string) (Item[T], bool) {
	for _, item := range c.Items {
		if item.Slug == slug {
			return item, true
		}
	}
	var zero Item[T]
	return zero, false
}

// Sort returns the collection ordered by less, leaving the original untouched.
// The sort is stable, so items the comparator treats as equal keep their load
// order — the alphabetical slug order [LoadCollection] produces.
func (c Collection[T]) Sort(less func(a, b Item[T]) bool) Collection[T] {
	out := slices.Clone(c.Items)
	sort.SliceStable(out, func(i, j int) bool { return less(out[i], out[j]) })
	return Collection[T]{Items: out}
}

// Filter returns the items keep accepts, in their original order.
func (c Collection[T]) Filter(keep func(Item[T]) bool) Collection[T] {
	var out []Item[T]
	for _, item := range c.Items {
		if keep(item) {
			out = append(out, item)
		}
	}
	return Collection[T]{Items: out}
}

// Chunk splits the collection into groups of at most size, for paginating a
// list of posts. See [Chunk] for the grouping semantics.
func (c Collection[T]) Chunk(size int) [][]Item[T] { return Chunk(c.Items, size) }

// Nodes turns the collection into one [ssg.Node] per item, keyed by slug, for
// use as a [ssg.Node.Generate] result:
//
//	"blog": {Generate: func() map[string]ssg.Node {
//		return posts.Nodes(func(c ssg.Ctx, it content.Item[Post]) templ.Component {
//			return src.PostPage(c, it)
//		})
//	}},
//
// Slugs with slashes nest, exactly as [LoadTree] keys do. Node.Title is left
// empty: the page function receives the item, so it reads the title — and
// anything else — from the item's Meta. A slug with a slash still nests, and
// one odd item can still be built by hand and merged into the result map.
func (c Collection[T]) Nodes(page func(ssg.Ctx, Item[T]) templ.Component) map[string]ssg.Node {
	out := make(map[string]ssg.Node, len(c.Items))
	for _, item := range c.Items {
		out[item.Slug] = ssg.Node{
			Page: func(ctx ssg.Ctx) templ.Component { return page(ctx, item) },
		}
	}
	return out
}
