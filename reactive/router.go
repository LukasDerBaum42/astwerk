//go:build js && wasm

package reactive

import (
	"strings"
	"sync"
	"syscall/js"

	"github.com/LukasDerBaum42/astwerk/wasmwrap"
)

// Params holds the values captured from a route pattern's :segments.
type Params map[string]string

// Get returns a captured parameter, or "" if the route didn't capture it.
func (p Params) Get(name string) string { return p[name] }

// Route maps a URL pattern to a view.
//
// Patterns are matched segment by segment:
//
//	/about                exact
//	/projects/:slug       captures slug
//	/docs/*               matches any remaining segments, captured as "*"
//
// Routes are tried in order, so put the specific ones first.
type Route struct {
	Path string
	View func(Params) wasmwrap.Element
}

var (
	pathOnce   sync.Once
	pathSignal *Signal[string]
)

// history is the single source of truth for the current URL, so the signal
// behind it is package-level by necessity — there is only one address bar.
func pathState() *Signal[string] {
	pathOnce.Do(func() {
		pathSignal = NewSignal(location())
		// The browser's back and forward buttons change the URL without going
		// through Navigate, so the signal has to follow popstate too.
		wasmwrap.Wrap(js.Global()).On("popstate", func(wasmwrap.Event) {
			pathSignal.Set(location())
		})
	})
	return pathSignal
}

func location() string {
	p := js.Global().Get("location").Get("pathname").String()
	if p == "" {
		return "/"
	}
	return p
}

// Path returns the current path and records a dependency on it, so an effect
// that reads it re-runs on navigation.
func Path() string { return pathState().Get() }

// Navigate goes to path, pushing a history entry.
func Navigate(path string) {
	js.Global().Get("history").Call("pushState", js.Null(), "", path)
	pathState().Set(path)
}

// Replace goes to path without adding a history entry, so the back button skips
// it. Use it for redirects.
func Replace(path string) {
	js.Global().Get("history").Call("replaceState", js.Null(), "", path)
	pathState().Set(path)
}

// Router renders whichever route matches the current path into root, swapping
// the view on navigation.
//
// Each view is built in its own scope, so effects and handlers inside it are
// disposed when you navigate away. If no route matches, root is left empty —
// add a Route with Path "/*" to catch that.
func Router(root wasmwrap.Element, routes []Route) (dispose func()) {
	return Effect(func() {
		path := Path()

		for _, r := range routes {
			params, ok := matchRoute(r.Path, path)
			if !ok {
				continue
			}

			// Detached from tracking so building the view doesn't subscribe this
			// effect to everything the view reads; only the path drives routing.
			view := Untracked(func() wasmwrap.Element { return r.View(params) })
			root.Append(view)
			OnCleanup(view.Remove)
			return
		}
	})
}

// InterceptLinks makes same-origin anchor clicks inside root route client-side
// instead of reloading the page.
//
// Left alone are modified clicks (so ctrl-click still opens a tab), anything
// with a target, external links, and anchors marked data-native — a download
// link needs a real navigation.
func InterceptLinks(root wasmwrap.Element) (dispose func()) {
	return root.On("click", func(e wasmwrap.Event) {
		ev := e.Value()
		if ev.Get("metaKey").Truthy() || ev.Get("ctrlKey").Truthy() ||
			ev.Get("shiftKey").Truthy() || ev.Get("altKey").Truthy() {
			return
		}
		if ev.Get("button").Int() != 0 {
			return
		}

		anchor := e.Target().Value().Call("closest", "a")
		if !anchor.Truthy() {
			return
		}
		a := wasmwrap.Wrap(anchor)
		if a.Attr("target") != "" || a.Attr("download") != "" || a.Attr("data-native") != "" {
			return
		}

		href := anchor.Get("href")
		if !href.Truthy() {
			return
		}
		url := js.Global().Get("URL").New(href.String())
		if url.Get("origin").String() != js.Global().Get("location").Get("origin").String() {
			return
		}

		e.PreventDefault()
		Navigate(url.Get("pathname").String())
	})
}

// matchRoute matches one pattern against a path, returning captured params.
func matchRoute(pattern, path string) (Params, bool) {
	pSegs := splitPath(pattern)
	uSegs := splitPath(path)
	params := Params{}

	for i, seg := range pSegs {
		if seg == "*" {
			params["*"] = strings.Join(uSegs[min(i, len(uSegs)):], "/")
			return params, true
		}
		if i >= len(uSegs) {
			return nil, false
		}
		if strings.HasPrefix(seg, ":") {
			if uSegs[i] == "" {
				return nil, false
			}
			params[seg[1:]] = uSegs[i]
			continue
		}
		if seg != uSegs[i] {
			return nil, false
		}
	}

	if len(uSegs) != len(pSegs) {
		return nil, false
	}
	return params, true
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}
