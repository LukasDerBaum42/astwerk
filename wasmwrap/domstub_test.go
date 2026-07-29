//go:build js && wasm

package wasmwrap

import (
	"syscall/js"
	"testing"

	"github.com/LukasDerBaum42/astwerk/internal/jsdom"
)

// node has no DOM, so these tests run against internal/jsdom — a stub that
// records what the wrapper does to it. That is exactly what is worth testing
// here: whether each Go method reaches for the right JS property and passes the
// right arguments. A real browser's DOM behaviour is not ours to verify.
func init() { jsdom.Install() }

// resetDOM gives each test a clean document.body.
func resetDOM(t *testing.T) {
	t.Helper()
	jsdom.Reset()
}

// mount attaches a fresh element to document.body and returns it.
func mount(tag string) Element {
	el := Wrap(jsdom.NewElement(tag))
	Body().Append(el)
	return el
}

// stubFetch replaces globalThis.fetch for one test.
func stubFetch(t *testing.T, body string, ok bool, status int, reject bool) {
	t.Helper()
	prev := js.Global().Get("fetch")
	t.Cleanup(func() { js.Global().Set("fetch", prev) })

	impl := js.FuncOf(func(_ js.Value, args []js.Value) any {
		promise := js.Global().Get("Promise")
		if reject {
			return promise.Call("reject", js.Global().Get("Error").New("network down"))
		}
		resp := js.Global().Get("Object").New()
		resp.Set("ok", ok)
		resp.Set("status", status)
		resp.Set("statusText", "Stub Status")
		resp.Set("arrayBuffer", js.FuncOf(func(js.Value, []js.Value) any {
			enc := js.Global().Get("TextEncoder").New()
			return promise.Call("resolve", enc.Call("encode", body).Get("buffer"))
		}))
		return promise.Call("resolve", resp)
	})
	t.Cleanup(impl.Release)
	js.Global().Set("fetch", impl)
}
