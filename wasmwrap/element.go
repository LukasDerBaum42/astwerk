//go:build js && wasm

package wasmwrap

import "syscall/js"

func document() js.Value { return js.Global().Get("document") }

// Query returns the first element matching selector. If nothing matches, the
// returned Element does not exist — check [Element.Exists] before using it, or
// its methods will return zero values.
func Query(selector string) Element {
	return Element{v: document().Call("querySelector", selector)}
}

// QueryAll returns every element matching selector, or an empty slice.
func QueryAll(selector string) []Element {
	list := document().Call("querySelectorAll", selector)
	n := list.Get("length").Int()
	out := make([]Element, 0, n)
	for i := range n {
		out = append(out, Element{v: list.Index(i)})
	}
	return out
}

// Create returns a new detached element. Append it to attach it.
func Create(tag string) Element {
	return Element{v: document().Call("createElement", tag)}
}

// Body returns document.body.
func Body() Element { return Element{v: document().Get("body")} }

// Doc returns the document itself. Useful for document-level events.
func Doc() Element { return Element{v: document()} }

// Element wraps a DOM node.
type Element struct{ v js.Value }

// Wrap adapts a raw js.Value, for handing values from other libraries back in.
func Wrap(v js.Value) Element { return Element{v: v} }

// Value returns the underlying js.Value — the escape hatch to raw syscall/js.
func (e Element) Value() js.Value { return e.v }

// Exists reports whether the element is a real node. A failed [Query] returns
// an Element for which this is false.
func (e Element) Exists() bool {
	return !e.v.IsNull() && !e.v.IsUndefined()
}

// --- Tree manipulation ---

// Append adds children as the last children of e and returns e.
func (e Element) Append(children ...Element) Element {
	for _, c := range children {
		e.v.Call("appendChild", c.v)
	}
	return e
}

// InsertBefore moves child to sit directly before ref among e's children. If
// ref does not exist, child is appended instead.
func (e Element) InsertBefore(child, ref Element) Element {
	if !ref.Exists() {
		return e.Append(child)
	}
	e.v.Call("insertBefore", child.v, ref.v)
	return e
}

// ChildAt returns e's nth child element, or a non-existent Element if there is
// no such child.
func (e Element) ChildAt(i int) Element {
	list := e.v.Get("children")
	if list.IsUndefined() || list.IsNull() || i < 0 || i >= list.Get("length").Int() {
		return Element{v: js.Null()}
	}
	return Element{v: list.Index(i)}
}

// Clear removes all of e's children.
func (e Element) Clear() Element {
	e.v.Set("innerHTML", "")
	return e
}

// Remove detaches e from its parent.
func (e Element) Remove() {
	if e.Exists() {
		e.v.Call("remove")
	}
}

// Clone returns a deep copy of e, not attached to the document.
func (e Element) Clone() Element {
	return Element{v: e.v.Call("cloneNode", true)}
}

// Parent returns e's parent element.
func (e Element) Parent() Element {
	return Element{v: e.v.Get("parentElement")}
}

// Children returns e's child elements.
func (e Element) Children() []Element {
	list := e.v.Get("children")
	if list.IsUndefined() || list.IsNull() {
		return nil
	}
	n := list.Get("length").Int()
	out := make([]Element, 0, n)
	for i := range n {
		out = append(out, Element{v: list.Index(i)})
	}
	return out
}

// Find returns the first descendant of e matching selector.
func (e Element) Find(selector string) Element {
	return Element{v: e.v.Call("querySelector", selector)}
}

// --- Content & attributes ---

// Text returns e's textContent.
func (e Element) Text() string {
	if !e.Exists() {
		return ""
	}
	return e.v.Get("textContent").String()
}

// SetText replaces e's textContent and returns e.
func (e Element) SetText(s string) Element {
	e.v.Set("textContent", s)
	return e
}

// HTML returns e's innerHTML.
func (e Element) HTML() string {
	if !e.Exists() {
		return ""
	}
	return e.v.Get("innerHTML").String()
}

// SetHTML replaces e's innerHTML and returns e.
func (e Element) SetHTML(s string) Element {
	e.v.Set("innerHTML", s)
	return e
}

// Attr returns the named attribute, or "" if it is absent.
func (e Element) Attr(name string) string {
	v := e.v.Call("getAttribute", name)
	if v.IsNull() || v.IsUndefined() {
		return ""
	}
	return v.String()
}

// SetAttr sets the named attribute and returns e.
func (e Element) SetAttr(name, value string) Element {
	e.v.Call("setAttribute", name, value)
	return e
}

// RemoveAttr deletes the named attribute and returns e.
func (e Element) RemoveAttr(name string) Element {
	e.v.Call("removeAttribute", name)
	return e
}

// InputValue returns the value of a form control.
func (e Element) InputValue() string {
	return e.v.Get("value").String()
}

// SetInputValue sets the value of a form control and returns e.
func (e Element) SetInputValue(s string) Element {
	e.v.Set("value", s)
	return e
}

// Class returns e's class list.
func (e Element) Class() ClassList {
	return ClassList{v: e.v.Get("classList")}
}

// ClassList wraps element.classList.
type ClassList struct{ v js.Value }

// Add adds every name and returns the list.
func (c ClassList) Add(names ...string) ClassList {
	for _, n := range names {
		c.v.Call("add", n)
	}
	return c
}

// Remove removes every name and returns the list.
func (c ClassList) Remove(names ...string) ClassList {
	for _, n := range names {
		c.v.Call("remove", n)
	}
	return c
}

// Toggle flips one name and reports whether it is now present.
func (c ClassList) Toggle(name string) bool {
	return c.v.Call("toggle", name).Bool()
}

// Has reports whether name is present.
func (c ClassList) Has(name string) bool {
	return c.v.Call("contains", name).Bool()
}
