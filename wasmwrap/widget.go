//go:build js && wasm

package wasmwrap

// Widget helpers are semantic constructors on top of [Create], with the
// defaults a browser needs: buttons are type="button" so they never submit a
// form, inputs carry their type. Everything a helper does is exactly what
// Create + setters do — visible, not magical.

// Button returns a <button type="button"> with text.
func Button(text string) Element {
	return Create("button").SetAttr("type", "button").SetText(text)
}

// Input returns a text <input>.
func Input() Element { return Create("input").SetAttr("type", "text") }

// NumberInput returns a number <input>.
func NumberInput() Element { return Create("input").SetAttr("type", "number") }

// Checkbox returns a checkbox <input>.
func Checkbox() Element { return Create("input").SetAttr("type", "checkbox") }

// Label returns a <label> with text.
func Label(text string) Element { return Create("label").SetText(text) }

// Link returns an <a href> with text.
func Link(href, text string) Element { return Create("a").SetAttr("href", href).SetText(text) }

// Span returns a <span> with text.
func Span(text string) Element { return Create("span").SetText(text) }

// Panel returns a plain <div>, the generic container for composing widgets.
func Panel() Element { return Create("div") }

// CreateText returns a detached text node, for appending raw text where an
// element's textContent would overwrite its children.
func CreateText(text string) Element {
	return Element{v: document().Call("createTextNode", text)}
}
