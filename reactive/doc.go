// Package reactive is an opt-in reactive layer over [wasmwrap] for building
// interactive UI in the browser.
//
// It is fine-grained rather than virtual-DOM based: a binding updates exactly
// the text, attribute or class it owns, and a keyed list moves nodes instead of
// rebuilding them. Nothing here re-renders a tree and diffs the result.
//
// # The pieces
//
// State — [Signal] holds a value, [Computed] derives one, [Effect] runs work
// when either changes, [Batch] coalesces a group of writes.
//
// DOM — BindText, BindHTML, BindAttr, BindClass, BindStyle, BindShow for
// one-property bindings; BindValue, BindNumber, BindChecked for form fields;
// [BindWhen] for a conditional subtree; [BindList] for a keyed list.
//
// Routing — [Router], [Navigate], [Path], [InterceptLinks].
//
// Async — [Resource] wraps a loader with Data, Err and Loading signals;
// [FetchJSON] decodes a response into a Go type.
//
// templ — [Hydrate] binds to markup astwerk already generated; [BindTempl]
// re-renders a component into an element when you want a whole fragment
// replaced.
//
// # Dependencies are tracked, not declared
//
// [Signal.Get] records a dependency on the effect currently running, so an
// effect subscribes to exactly what it read on its last run. A branch not taken
// creates no dependency, and a signal read behind an if that later goes the
// other way stops triggering it:
//
//	name := reactive.NewSignal("world")
//	reactive.Effect(func() {
//		fmt.Println("hello", name.Get()) // re-runs whenever name changes
//	})
//
// Use [Signal.Peek] to read without subscribing, and [Untracked] for a whole
// block.
//
// # Ownership
//
// Effects nest. One created inside another is disposed and rebuilt when the
// outer re-runs, so bindings inside a view that gets rebuilt don't leak.
// [OnCleanup] hooks into that, and [Scope] groups effects under one owner
// without making them reactive to anything.
//
// Every Bind* function returns a dispose func. Called inside an effect the
// binding is owned by it, so the return is usually ignorable — it matters at the
// top level of a script, where nothing else owns it.
//
// # Two ways to use templ
//
// astwerk already renders templ at build time. In the browser you can either:
//
// Bind to that output — [Hydrate] finds server-rendered markup and attaches
// behaviour. Markup lives in one place, updates stay surgical, nothing renders
// twice. This is the recommended default.
//
// Re-render fragments — [BindTempl] runs a templ component in the browser and
// assigns the result as innerHTML. Convenient for display-only chunks, but it
// destroys and rebuilds everything inside the target, losing focus, scroll
// position and event handlers. See its documentation.
//
// # Threading
//
// Go on js/wasm runs one goroutine at a time, so signals need no locking. A
// goroutine may still write a signal — that is how [Resource] reports its
// result — and the effects it triggers run on that goroutine.
package reactive
