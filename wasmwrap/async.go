//go:build js && wasm

package wasmwrap

import (
	"fmt"
	"syscall/js"
)

// SetTimeout runs fn once after ms milliseconds and returns a cancel function.
// Calling cancel after fn has run is harmless.
func SetTimeout(fn func(), ms int) (cancel func()) {
	return schedule("setTimeout", "clearTimeout", fn, ms, true)
}

// SetInterval runs fn every ms milliseconds until cancel is called.
func SetInterval(fn func(), ms int) (cancel func()) {
	return schedule("setInterval", "clearInterval", fn, ms, false)
}

// schedule wires up a JS timer, taking care of releasing the callback exactly
// once whether it is cancelled or (for a timeout) allowed to fire.
func schedule(set, clear string, fn func(), ms int, once bool) func() {
	var (
		cb   js.Func
		id   js.Value
		done bool
	)

	release := func() {
		if done {
			return
		}
		done = true
		cb.Release()
	}

	cb = js.FuncOf(func(js.Value, []js.Value) any {
		if once {
			// Release after fn so a panic in fn can't leave the Func alive.
			defer release()
		}
		fn()
		return nil
	})

	id = js.Global().Call(set, cb, ms)

	return func() {
		if done {
			return
		}
		js.Global().Call(clear, id)
		release()
	}
}

// Fetch GETs url and returns the response body.
//
// It blocks the calling goroutine until the request finishes, hiding the promise
// entirely. Safe from main or any goroutine you start; not safe from inside an
// event handler — see the package documentation.
//
// A non-2xx response is an error, and the error includes the status.
func Fetch(url string) ([]byte, error) {
	resp, err := await(js.Global().Call("fetch", url))
	if err != nil {
		return nil, fmt.Errorf("wasmwrap: fetch %s: %w", url, err)
	}
	if !resp.Get("ok").Bool() {
		return nil, fmt.Errorf("wasmwrap: fetch %s: %d %s",
			url, resp.Get("status").Int(), resp.Get("statusText").String())
	}

	buf, err := await(resp.Call("arrayBuffer"))
	if err != nil {
		return nil, fmt.Errorf("wasmwrap: fetch %s: reading body: %w", url, err)
	}

	u8 := js.Global().Get("Uint8Array").New(buf)
	out := make([]byte, u8.Get("length").Int())
	js.CopyBytesToGo(out, u8)
	return out, nil
}

// FetchString is Fetch returning the body as a string.
func FetchString(url string) (string, error) {
	b, err := Fetch(url)
	return string(b), err
}

// await blocks until promise settles. The Go runtime hands control back to the
// JS event loop while every goroutine is blocked, so the promise still resolves.
func await(promise js.Value) (js.Value, error) {
	type result struct {
		v   js.Value
		err error
	}
	ch := make(chan result, 1)

	onOK := js.FuncOf(func(_ js.Value, args []js.Value) any {
		var v js.Value
		if len(args) > 0 {
			v = args[0]
		}
		ch <- result{v: v}
		return nil
	})
	defer onOK.Release()

	onErr := js.FuncOf(func(_ js.Value, args []js.Value) any {
		msg := "rejected"
		if len(args) > 0 {
			msg = errString(args[0])
		}
		ch <- result{err: fmt.Errorf("%s", msg)}
		return nil
	})
	defer onErr.Release()

	promise.Call("then", onOK).Call("catch", onErr)

	r := <-ch
	return r.v, r.err
}

// errString gets something readable out of a rejected value, which may be an
// Error, a string, or anything else JS felt like throwing.
func errString(v js.Value) string {
	if v.IsNull() || v.IsUndefined() {
		return "rejected"
	}
	if msg := v.Get("message"); msg.Type() == js.TypeString {
		return msg.String()
	}
	if v.Type() == js.TypeString {
		return v.String()
	}
	return js.Global().Get("String").Invoke(v).String()
}
