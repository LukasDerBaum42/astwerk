// Package reactive is an opt-in reactive layer over [wasmwrap] for building
// interactive UI in the browser.
//
// It is fine-grained rather than virtual-DOM based: a binding updates exactly
// the text, attribute or class it owns, and a keyed list moves nodes instead of
// rebuilding them. Nothing here re-renders a tree and diffs the result.
//
// # State
//
// State is split into one type per value kind, so what compiles is what is
// meaningful: [Num] (int/int64/float64), [Bool], [Str] and [List]. Short
// constructors keep a declaration to one line:
//
//	count := reactive.Num(0)
//	open  := reactive.Bool(false)
//
// Each is an object with real methods — count.Inc(1) increments now, open.
// Toggle() flips now. [Computed] derives a read-only memo from a Go closure,
// [Effect] runs work when a signal it read changes, and [Batch] coalesces a
// group of writes. The generic [Signal] stays for the cases with no useful kind
// — a [Resource]'s Data or Err field.
//
// # DOM bindings
//
// Bindings take the signal object directly, so the element renders the value
// and strconv dies at the call site:
//
//	reactive.BindText(label, count)   // a NumSignal or StrSignal
//	reactive.BindClass(item, "active", selected)
//	reactive.BindValue(input, draft)  // two-way, form controls
//
// Each returns a dispose function; called inside an effect the binding is owned
// by that effect. [BindList] keeps a container in sync with a [ListSignal],
// with per-row state via [Row]. [Hydrate] attaches behaviour to markup ssg
// already generated.
//
// # Routing, async, templ
//
// [Router], [Navigate] and [Path] drive a single-page app; [Resource] wraps an
// async loader with Data, Err and Loading. [Hydrate] is the recommended way to
// use templ in the browser — markup lives in one place, updates stay surgical.
//
// # Dependencies are tracked, not declared
//
// [Signal.Get] records a dependency on the effect currently running, so an
// effect subscribes to exactly what it read on its last run. Use [Signal.Peek]
// to read without subscribing, and [Untracked] for a whole block.
//
// # The x bridge
//
// [XNum], [XBool], [XStr] and [XList] turn a cell an x page declared (with a
// Named constructor) into the same state object a local signal is: reads come
// from the x cell, writes go to it, and both sides update live.
//
// # Threading
//
// Go on js/wasm runs one goroutine at a time, so signals need no locking. A
// goroutine may still write a signal — that is how [Resource] reports its
// result — and the effects it triggers run on that goroutine.
package reactive
