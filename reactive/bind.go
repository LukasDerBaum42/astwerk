//go:build js && wasm

package reactive

import (
	"fmt"
	"strconv"

	"github.com/LukasDerBaum42/astwerk/wasmwrap"
)

// The Bind* functions are all one-liners over [Effect]. They exist because
// "re-run this and poke one property" is the entire job of a reactive DOM
// binding, and writing the effect out by hand at every call site is the
// boilerplate worth removing.
//
// Each takes the signal object directly — the element knows how to render the
// value, so the conversion (strconv and friends) dies here rather than at the
// call site. Each returns a dispose function; called inside an effect the
// binding is owned by that effect and torn down with it, so you can usually
// ignore the return.

// TextValue is what [BindText] renders: a number or a string. The binding
// formats the value, so strconv dies at the call site.
type TextValue interface{ int | int64 | float64 | string }

// BindText keeps el's text in sync with s, formatting numbers as text directly.
func BindText[T TextValue](el wasmwrap.Element, s Readable[T]) (dispose func()) {
	return Effect(func() { el.SetText(fmt.Sprint(s.Get())) })
}

// BindHTML keeps el's innerHTML in sync with s.
//
// s's value is not escaped. Never pass user input through it.
func BindHTML(el wasmwrap.Element, s Readable[string]) (dispose func()) {
	return Effect(func() { el.SetHTML(s.Get()) })
}

// BindAttr keeps one attribute in sync with s. An empty string removes the
// attribute, which is what boolean attributes like "disabled" need.
func BindAttr(el wasmwrap.Element, name string, s Readable[string]) (dispose func()) {
	return Effect(func() {
		if v := s.Get(); v == "" {
			el.RemoveAttr(name)
		} else {
			el.SetAttr(name, v)
		}
	})
}

// BindClass adds or removes one class according to s.
func BindClass(el wasmwrap.Element, class string, s Readable[bool]) (dispose func()) {
	return Effect(func() {
		if s.Get() {
			el.Class().Add(class)
		} else {
			el.Class().Remove(class)
		}
	})
}

// BindStyle keeps one CSS property in sync with s.
func BindStyle(el wasmwrap.Element, prop string, s Readable[string]) (dispose func()) {
	return Effect(func() { el.Style().Set(prop, s.Get()) })
}

// BindShow shows or hides el according to s.
//
// Hiding sets display:none; showing clears the inline value rather than setting
// a specific display, so the stylesheet stays in charge of how it lays out.
func BindShow(el wasmwrap.Element, s Readable[bool]) (dispose func()) {
	return Effect(func() {
		if s.Get() {
			el.Style().Show()
		} else {
			el.Style().Hide()
		}
	})
}

// BindDisabled toggles the boolean disabled attribute according to s.
func BindDisabled(el wasmwrap.Element, s Readable[bool]) (dispose func()) {
	return Effect(func() {
		if s.Get() {
			el.SetAttr("disabled", "")
		} else {
			el.RemoveAttr("disabled")
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
func BindValue(el wasmwrap.Element, s *StrSignal) (dispose func()) {
	return Effect(func() {
		el.SetInputValue(s.Get())
		On(el, "input", func(wasmwrap.Event) { s.Set(el.InputValue()) })
	})
}

// BindNumber two-way binds a number input to s. Input that isn't a number is
// ignored rather than clobbering the signal with zero.
func BindNumber[T int | int64 | float64](el wasmwrap.Element, s *NumSignal[T]) (dispose func()) {
	return Effect(func() {
		el.SetInputValue(strconv.FormatFloat(float64(s.Get()), 'f', -1, 64))
		On(el, "input", func(wasmwrap.Event) {
			if v, err := strconv.ParseFloat(el.InputValue(), 64); err == nil {
				s.Set(T(v))
			}
		})
	})
}

// BindChecked two-way binds a checkbox to s.
func BindChecked(el wasmwrap.Element, s *BoolSignal) (dispose func()) {
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
func BindWhen(parent wasmwrap.Element, cond Readable[bool], render func() wasmwrap.Element) (dispose func()) {
	return Effect(func() {
		if !cond.Get() {
			return
		}
		// Untracked so building the subtree doesn't add its reads to this
		// effect's dependencies — only cond should drive it.
		el := Untracked(render)
		parent.Append(el)
		OnCleanup(el.Remove)
	})
}
