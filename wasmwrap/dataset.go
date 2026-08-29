//go:build js && wasm

package wasmwrap

// Focus gives e keyboard focus.
func (e Element) Focus() { e.v.Call("focus") }

// Blur removes keyboard focus from e.
func (e Element) Blur() { e.v.Call("blur") }

// ScrollIntoView scrolls e into the viewport.
func (e Element) ScrollIntoView() { e.v.Call("scrollIntoView") }

// Dataset reads and writes an element's data-* attributes.
//
// It is modeled on getAttribute/setAttribute rather than the dataset property,
// so it works against the test stub and keeps the data- prefix explicit.
type Dataset struct{ e Element }

// Dataset returns e's data-* attribute handle.
func (e Element) Dataset() Dataset { return Dataset{e: e} }

// Get reads a data-* attribute by its bare name ("x" reads data-x).
func (d Dataset) Get(name string) string { return d.e.Attr("data-" + name) }

// Set writes a data-* attribute and returns the handle.
func (d Dataset) Set(name, value string) Dataset {
	d.e.SetAttr("data-"+name, value)
	return d
}

// Remove deletes a data-* attribute and returns the handle.
func (d Dataset) Remove(name string) Dataset {
	d.e.RemoveAttr("data-" + name)
	return d
}
