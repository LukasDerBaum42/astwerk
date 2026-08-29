//go:build js && wasm

package wasmwrap

import "syscall/js"

// Event wraps a DOM event.
type Event struct{ v js.Value }

// Value returns the underlying js.Value — the escape hatch for event
// properties this type doesn't model.
func (ev Event) Value() js.Value { return ev.v }

// PreventDefault suppresses the browser's default action.
func (ev Event) PreventDefault() { ev.v.Call("preventDefault") }

// StopPropagation stops the event bubbling further up the tree.
func (ev Event) StopPropagation() { ev.v.Call("stopPropagation") }

// Target returns the element the event fired on.
func (ev Event) Target() Element { return Element{v: ev.v.Get("target")} }

// Type returns the event name ("click", "keydown").
func (ev Event) Type() string { return ev.v.Get("type").String() }

// Key returns the pressed key for keyboard events ("a", "Enter", "ArrowLeft").
func (ev Event) Key() string {
	k := ev.v.Get("key")
	if k.IsUndefined() || k.IsNull() {
		return ""
	}
	return k.String()
}

// Pos returns the pointer position for mouse and pointer events, relative to
// the viewport.
func (ev Event) Pos() (x, y int) {
	return ev.v.Get("clientX").Int(), ev.v.Get("clientY").Int()
}

// PosIn returns the pointer position relative to el's top-left corner — what
// you want for hit-testing inside a canvas.
func (ev Event) PosIn(el Element) (x, y int) {
	r := el.v.Call("getBoundingClientRect")
	cx, cy := ev.Pos()
	return cx - r.Get("left").Int(), cy - r.Get("top").Int()
}

// InputValue returns the target's value for input/change events — the field's
// current text, read from the event rather than a captured element.
func (ev Event) InputValue() string {
	t := ev.v.Get("target")
	if t.IsNull() || t.IsUndefined() {
		return ""
	}
	return t.Get("value").String()
}

// On binds fn to the named event and returns a function that unbinds it.
//
// Calling the returned function also releases the underlying js.Func. Handlers
// bound for the lifetime of the page never need it, but anything bound
// repeatedly — per row of a re-rendered list, say — leaks without it.
func (e Element) On(event string, fn func(Event)) (unsubscribe func()) {
	cb := js.FuncOf(func(_ js.Value, args []js.Value) any {
		var ev Event
		if len(args) > 0 {
			ev = Event{v: args[0]}
		}
		fn(ev)
		return nil
	})
	e.v.Call("addEventListener", event, cb)

	done := false
	return func() {
		if done {
			return
		}
		done = true
		e.v.Call("removeEventListener", event, cb)
		cb.Release()
	}
}

// Once binds fn to the named event and unbinds it after the first call.
func (e Element) Once(event string, fn func(Event)) (unsubscribe func()) {
	var off func()
	off = e.On(event, func(ev Event) {
		off()
		fn(ev)
	})
	return off
}

// OnOptions tailors an event listener with the flags addEventListener accepts.
type OnOptions struct {
	Capture bool // fire during the capture phase, not the bubble phase
	Once    bool // unbind after the first call
	Passive bool // never call preventDefault — lets the browser scroll freely
}

// OnOpts binds fn to the named event with the given options and returns a
// function that unbinds it. See [Element.On] for the js.Func lifecycle.
func (e Element) OnOpts(event string, opts OnOptions, fn func(Event)) (unsubscribe func()) {
	cb := js.FuncOf(func(_ js.Value, args []js.Value) any {
		var ev Event
		if len(args) > 0 {
			ev = Event{v: args[0]}
		}
		fn(ev)
		return nil
	})
	add := js.Global().Get("Object").New()
	if opts.Capture {
		add.Set("capture", true)
	}
	if opts.Once {
		add.Set("once", true)
	}
	if opts.Passive {
		add.Set("passive", true)
	}
	e.v.Call("addEventListener", event, cb, add)

	done := false
	return func() {
		if done {
			return
		}
		done = true
		rem := js.Global().Get("Object").New()
		if opts.Capture {
			rem.Set("capture", true)
		}
		e.v.Call("removeEventListener", event, cb, rem)
		cb.Release()
	}
}
