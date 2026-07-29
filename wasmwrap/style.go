//go:build js && wasm

package wasmwrap

import "syscall/js"

// Style edits an element's inline style. Common properties get their own
// method so they type-check and autocomplete; anything not modeled falls
// through [Style.Set].
type Style struct{ v js.Value }

// Style returns e's inline style.
func (e Element) Style() Style {
	return Style{v: e.v.Get("style")}
}

// Set assigns any CSS property by name — the fallback for anything without a
// dedicated method. Use the CSS spelling ("border-radius"), not the JS one.
func (s Style) Set(prop, value string) Style {
	s.v.Call("setProperty", prop, value)
	return s
}

// Get reads back a property set on this element's inline style. It does not
// resolve stylesheet rules; use [Element.Computed] for that.
func (s Style) Get(prop string) string {
	return s.v.Call("getPropertyValue", prop).String()
}

// Computed resolves a property's actual value, including stylesheet rules.
func (e Element) Computed(prop string) string {
	cs := js.Global().Call("getComputedStyle", e.v)
	return cs.Call("getPropertyValue", prop).String()
}

func (s Style) Display(v string) Style    { return s.Set("display", v) }
func (s Style) Color(v string) Style      { return s.Set("color", v) }
func (s Style) Background(v string) Style { return s.Set("background", v) }
func (s Style) Opacity(v string) Style    { return s.Set("opacity", v) }
func (s Style) Overflow(v string) Style   { return s.Set("overflow", v) }

// Size sets width and height. An empty string leaves that dimension alone.
func (s Style) Size(width, height string) Style {
	if width != "" {
		s.Set("width", width)
	}
	if height != "" {
		s.Set("height", height)
	}
	return s
}

// Position sets top and left. An empty string leaves that offset alone. It does
// not set the CSS position property — pair it with [Style.Set] if the element
// isn't already positioned.
func (s Style) Position(top, left string) Style {
	if top != "" {
		s.Set("top", top)
	}
	if left != "" {
		s.Set("left", left)
	}
	return s
}

// Hide sets display:none and returns the style.
func (s Style) Hide() Style { return s.Set("display", "none") }

// Show clears an inline display:none, letting the stylesheet decide again.
func (s Style) Show() Style { return s.Set("display", "") }
