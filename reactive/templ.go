//go:build js && wasm

package reactive

import (
	"bytes"
	"context"

	"github.com/LukasDerBaum42/astwerk/wasmwrap"
	"github.com/a-h/templ"
)

// RenderTempl renders a templ component to an HTML string.
//
// templ is pure Go, so it compiles to wasm and runs in the browser unchanged —
// the same component can render a page at build time and a fragment at runtime.
func RenderTempl(c templ.Component) (string, error) {
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// TemplElement renders a component into a detached element, for handing to
// [BindList] or [BindWhen] as a rendered item.
//
// tag is the wrapper element to render into ("div", "li", "tr").
func TemplElement(tag string, c templ.Component) wasmwrap.Element {
	el := wasmwrap.Create(tag)
	html, err := RenderTempl(c)
	if err != nil {
		return el.SetText("render error: " + err.Error())
	}
	return el.SetHTML(html)
}

// BindTempl re-renders a templ component into el whenever a signal it read
// changes.
//
// This replaces el's entire contents, which is the trade-off worth understanding
// before reaching for it: templ produces a string, so there is nothing to patch
// against. Everything inside el is destroyed and rebuilt, losing input focus,
// scroll position, selection, and any event handler bound to a node within it.
//
// Prefer it for chunks that are genuinely display-only and change as a unit — a
// rendered markdown preview, a summary panel, a stats table. For anything the
// user interacts with, write the markup in templ once, server-side, and bind to
// it with [BindText] and friends: that keeps templ as the single source of markup
// and still patches surgically.
//
// Handlers inside re-rendered content must be re-bound on every render, since the
// nodes they were attached to are gone:
//
//	reactive.BindTempl(panel, func() templ.Component {
//		return views.Summary(count.Get())
//	})
//	// Anything clickable in there: rebind after each render, or bind on `panel`
//	// itself and use Event.Target to work out what was hit.
func BindTempl(el wasmwrap.Element, fn func() templ.Component) (dispose func()) {
	return Effect(func() {
		html, err := RenderTempl(fn())
		if err != nil {
			el.SetText("render error: " + err.Error())
			return
		}
		el.SetHTML(html)
	})
}

// Hydrate binds behaviour to markup that is already in the page.
//
// This is the pattern that fits astwerk best: the site's HTML is generated at
// build time by templ through ssg.Build, and a script attaches to it rather than
// re-creating it. Nothing is rendered twice, and updates stay surgical.
//
//	//go:build js && wasm
//
//	func main() {
//		count := reactive.NewSignal(0)
//
//		reactive.Hydrate("#counter", func(root wasmwrap.Element) {
//			reactive.BindText(root.Find("[data-count]"), func() string {
//				return strconv.Itoa(count.Get())
//			})
//			reactive.On(root.Find("[data-inc]"), "click", func(wasmwrap.Event) {
//				count.Update(func(v int) int { return v + 1 })
//			})
//		})
//
//		select {} // keep the module alive
//	}
//
// fn is not called if nothing matches selector, so a script can be included on
// every page and only act where its markup exists. The returned function
// disposes everything fn created.
func Hydrate(selector string, fn func(root wasmwrap.Element)) (dispose func()) {
	root := wasmwrap.Query(selector)
	if !root.Exists() {
		return func() {}
	}
	return Scope(func() { fn(root) })
}

// HydrateAll runs fn for every element matching selector, for markup that
// repeats on a page — a card widget, a collapsible section.
func HydrateAll(selector string, fn func(root wasmwrap.Element)) (dispose func()) {
	roots := wasmwrap.QueryAll(selector)
	if len(roots) == 0 {
		return func() {}
	}
	return Scope(func() {
		for _, root := range roots {
			fn(root)
		}
	})
}
