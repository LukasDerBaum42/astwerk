+++
Title = "wasmwrap"
Description = "syscall/js made to feel like Go: DOM, style, events, fetch, canvas."
Order = 60
+++

`syscall/js` works, but every call is a stringly-typed `js.Value.Call` and
reading it feels like looking at JavaScript through a keyhole. `wasmwrap` covers
what you actually do in a browser with normal Go types.

It exists only under `GOOS=js GOARCH=wasm`, so a script needs the matching build
constraint.

```go
//go:build js && wasm

package main

import "github.com/LukasDerBaum42/astwerk/wasmwrap"

func main() {
	nav := wasmwrap.Query("#nav")
	wasmwrap.Query("#toggle").On("click", func(e wasmwrap.Event) {
		e.PreventDefault()
		nav.Class().Toggle("open")
	})
	select {}
}
```

## Chaining

Every method with nothing to return returns its receiver:

```go
wasmwrap.Query("#box").
	Style().Color("red").Display("block")

wasmwrap.Create("li").
	SetText("item").
	SetAttr("data-id", "7").
	Class().Add("row")
```

## What's covered

| | |
| --- | --- |
| Finding | `Query`, `QueryAll`, `Create`, `Body`, `Doc`, `Element.Find` |
| Tree | `Append`, `InsertBefore`, `Remove`, `Clone`, `Clear`, `Parent`, `Children`, `ChildAt` |
| Content | `Text`/`SetText`, `HTML`/`SetHTML`, `Attr`/`SetAttr`/`RemoveAttr`, `InputValue` |
| Classes | `Class()` → `Add`, `Remove`, `Toggle`, `Has` |
| Style | `Color`, `Display`, `Background`, `Opacity`, `Overflow`, `Size`, `Position`, `Hide`, `Show`, `Set` |
| Events | `On`, `Once`; `Event.PreventDefault`, `StopPropagation`, `Target`, `Key`, `Pos`, `PosIn` |
| Timers | `SetTimeout`, `SetInterval` — both return a cancel func |
| Network | `Fetch`, `FetchString` |
| Canvas | `AsCanvas`, `Context2D`, `SetSize`, `FitToDisplay`, and a typed `Ctx2D` |

## A missing element is inert, not a panic

`querySelector` returns null when nothing matches. Rather than letting that
become a panic three calls later, a failed `Query` gives an Element you can ask
about:

```go
el := wasmwrap.Query("#maybe")
if !el.Exists() {
	return
}
```

Readers return zero values on a missing element and `Remove` is a no-op, so a
script that forgets the check misbehaves quietly instead of crashing.

## Unsubscribe releases the callback

`On` returns a function that both unbinds the handler and releases the underlying
`js.Func`:

```go
off := btn.On("click", handler)
defer off()
```

For a handler bound once for the lifetime of the page you can ignore it. For
anything bound repeatedly — per row of a re-rendered list — dropping it leaks a
`js.Func` every time. `reactive.On` wires this into effect cleanup
automatically.

## Fetch blocks, and that's fine

```go
body, err := wasmwrap.Fetch("/api/thing")
```

No promise at the call site. It blocks the calling goroutine, which is safe from
`main` or any goroutine you start: when every goroutine is blocked, the Go
runtime hands control back to the JavaScript event loop, so the pending promise
still resolves.

It is **not** safe directly inside an event handler. Handlers run on a callback
goroutine that the event loop is waiting on, so blocking there deadlocks. Start a
goroutine:

```go
btn.On("click", func(wasmwrap.Event) {
	go func() {
		body, err := wasmwrap.Fetch("/api/thing")
		// …
	}()
})
```

## Canvas

The roughest part of raw `syscall/js`, so it gets a typed context:

```go
canvas := wasmwrap.Query("#c").AsCanvas()
ctx := canvas.Context2D()
w, h := canvas.FitToDisplay(ctx)

ctx.FillStyle("#4f7cff").
	BeginPath().
	Arc(w/2, h/2, 40, 0, 2*math.Pi).
	Fill()
```

`FitToDisplay` sizes the drawing buffer to the element's CSS size times the
device pixel ratio and scales the context to match, returning the logical size to
draw against. Getting that wrong is why canvas drawings look blurry on high-DPI
screens: the `width`/`height` attributes are the pixel buffer, the CSS size is
the display size, and they are different things.

`Ctx2D.RequestAnimationFrame` drives a loop rather than a single frame — it
reschedules itself and hands you the delta in seconds:

```go
cancel := ctx.RequestAnimationFrame(func(dt float64) {
	elapsed += dt
	ctx.Clear(0, 0, w, h)
	// draw
})
```

## Never a dead end

Anything not modeled — an obscure API, a browser quirk — drops to the raw value:

```go
el.Value().Call("scrollIntoView", map[string]any{"behavior": "smooth"})
```

`Element.Value()`, `Event.Value()` and `Ctx2D.Value()` all return the underlying
`js.Value`, and `wasmwrap.Wrap` takes one back. The wrapper is a shortcut for the
common ninety percent, not a replacement for the platform.
