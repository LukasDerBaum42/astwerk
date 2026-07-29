// Package wasmwrap makes syscall/js feel like Go rather than like reading
// JavaScript through a keyhole.
//
// It covers what you actually do in a browser script — query and create
// elements, edit style and attributes, bind events, drive a canvas — with
// normal Go types and no promise or callback juggling at the call site.
//
// Every method that has nothing to return returns its receiver, so calls chain:
//
//	wasmwrap.Query("#box").Style().Color("red").Display("block")
//
// Anything not modeled here drops to [Element.Value] and raw syscall/js, so the
// wrapper is never a dead end — only a shortcut for the common 90%.
//
// # Build constraints
//
// The API only exists under GOOS=js GOARCH=wasm. This file deliberately carries
// no constraint so that `go build ./...` on a host platform sees a valid (empty)
// package instead of failing with "build constraints exclude all Go files".
// Scripts using it need the same constraint:
//
//	//go:build js && wasm
//
// # Blocking
//
// [Fetch] blocks its goroutine until the response arrives. That is safe from
// main or any goroutine you start: when every goroutine is blocked, the Go
// runtime hands control back to the JavaScript event loop, so the pending
// promise still resolves.
//
// It is not safe from inside an event handler. Handlers run on a callback
// goroutine that the event loop is waiting on, so blocking there deadlocks.
// Start a goroutine instead:
//
//	btn.On("click", func(e wasmwrap.Event) {
//		go func() {
//			body, err := wasmwrap.Fetch("/api/thing")
//			// ...
//		}()
//	})
package wasmwrap
