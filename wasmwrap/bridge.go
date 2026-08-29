//go:build js && wasm

package wasmwrap

import "syscall/js"

// Export publishes fn as a global JavaScript function named name
// (globalThis.name), so x Script/Effect blocks and ordinary page scripts can
// call it. The returned function removes the global and releases the underlying
// js.Func — call it when the handler must not outlive its owner, or leave it for
// the lifetime of the page. The callback shape is the raw syscall/js one:
// honest, no hidden conversion.
func Export(name string, fn func(this js.Value, args []js.Value) any) (unexport func()) {
	f := js.FuncOf(fn)
	js.Global().Set(name, f)

	done := false
	return func() {
		if done {
			return
		}
		done = true
		js.Global().Delete(name)
		f.Release()
	}
}

// Ready marks the wasm program as initialized: it sets globalThis.astwerkWasm
// and dispatches an "astwerk-wasm-ready" event. Call it at the end of main()'s
// setup, after all exports and bindings, so a page script that gates on the wasm
// module (the whenWasm snippet) knows it can call exported functions.
func Ready() {
	js.Global().Set("astwerkWasm", true)
	if ev := js.Global().Get("Event"); ev.Truthy() {
		js.Global().Call("dispatchEvent", ev.New("astwerk-wasm-ready"))
	}
}
