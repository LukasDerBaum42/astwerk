//go:build js && wasm

package reactive

import (
	"strconv"

	"github.com/LukasDerBaum42/astwerk/wasmwrap"
)

// The Bind* functions are all one-liners over [Effect]. They exist because
// "re-run this and poke one property" is the entire job of a reactive DOM
// binding, and writing the effect out by hand at every call site is the
// boilerplate worth removing.
//
// Each returns a dispose function. Called inside an effect, the binding is owned
// by that effect and torn down with it, so you can usually ignore the return.

// BindText keeps el's text in sync with fn.
func BindText(el wasmwrap.Element, fn func() string) (dispose func()) {
	return Effect(func() { el.SetText(fn()) })
}

// BindHTML keeps el's innerHTML in sync with fn.
//
// fn's output is not escaped. Never pass user input through it.
func BindHTML(el wasmwrap.Element, fn func() string) (dispose func()) {
	return Effect(func() { el.SetHTML(fn()) })
}

// BindAttr keeps one attribute in sync with fn. An empty string removes the
// attribute, which is what boolean attributes like "disabled" need.
func BindAttr(el wasmwrap.Element, name string, fn func() string) (dispose func()) {
	return Effect(func() {
		if v := fn(); v == "" {
			el.RemoveAttr(name)
		} else {
			el.SetAttr(name, v)
		}
	})
}

// BindClass adds or removes one class according to fn.
func BindClass(el wasmwrap.Element, class string, fn func() bool) (dispose func()) {
	return Effect(func() {
		if fn() {
			el.Class().Add(class)
		} else {
			el.Class().Remove(class)
		}
	})
}

// BindStyle keeps one CSS property in sync with fn.
func BindStyle(el wasmwrap.Element, prop string, fn func() string) (dispose func()) {
	return Effect(func() { el.Style().Set(prop, fn()) })
}

// BindShow shows or hides el according to fn.
//
// Hiding sets display:none; showing clears the inline value rather than setting
// a specific display, so the stylesheet stays in charge of how it lays out.
func BindShow(el wasmwrap.Element, fn func() bool) (dispose func()) {
	return Effect(func() {
		if fn() {
			el.Style().Show()
		} else {
			el.Style().Hide()
		}
	})
}

// On binds an event handler and registers its removal with the enclosing effect,
// so a handler bound inside a re-rendered view doesn't leak.
func On(el wasmwrap.Element, event string, fn func(wasmwrap.Event)) {
	off := el.On(event, fn)
	OnCleanup(off)
}

// BindValue two-way binds a text input, textarea or select to s.
//
// Typing writes to the signal; setting the signal updates the field. It does not
// feed back on itself: [Signal.Set] ignores a write equal to the current value,
// so the effect that echoes the signal back into the field stops there.
func BindValue(el wasmwrap.Element, s *Signal[string]) (dispose func()) {
	return Effect(func() {
		el.SetInputValue(s.Get())
		On(el, "input", func(wasmwrap.Event) { s.Set(el.InputValue()) })
	})
}

// BindNumber two-way binds a number input to s. Input that isn't a number is
// ignored rather than clobbering the signal with zero.
func BindNumber(el wasmwrap.Element, s *Signal[float64]) (dispose func()) {
	return Effect(func() {
		el.SetInputValue(strconv.FormatFloat(s.Get(), 'f', -1, 64))
		On(el, "input", func(wasmwrap.Event) {
			if v, err := strconv.ParseFloat(el.InputValue(), 64); err == nil {
				s.Set(v)
			}
		})
	})
}

// BindChecked two-way binds a checkbox to s.
func BindChecked(el wasmwrap.Element, s *Signal[bool]) (dispose func()) {
	return Effect(func() {
		el.Value().Set("checked", s.Get())
		On(el, "change", func(wasmwrap.Event) { s.Set(el.Value().Get("checked").Bool()) })
	})
}

// BindWhen renders a subtree only while cond is true, and removes it when cond
// goes false.
//
// render runs again on every transition into true, so the subtree and any
// effects inside it are genuinely fresh — that is the difference from
// [BindShow], which only toggles visibility and keeps state.
func BindWhen(parent wasmwrap.Element, cond func() bool, render func() wasmwrap.Element) (dispose func()) {
	return Effect(func() {
		if !cond() {
			return
		}
		// Untracked so building the subtree doesn't add its reads to this
		// effect's dependencies — only cond should drive it.
		el := Untracked(render)
		parent.Append(el)
		OnCleanup(el.Remove)
	})
}
