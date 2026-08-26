// Package x is Astwerk's compiled reactive markup: ordinary Go functions that
// generate both the HTML and the small JavaScript a page needs to become
// interactive, with no virtual DOM, no framework, and no Go/WASM in the
// browser.
//
// Markers are plain Go functions that render at the exact spot they are used.
// Each owns the element it renders, so it can stamp it with an internal id and
// append its wiring to a script buffer in the same call — there is no build
// pass, no selector to keep in sync, and nothing to re-render client-side.
//
// A reactive page is a [Document] wrapping a component:
//
//	var count = x.NewSignal(0)
//
//	templ Counter() {
//		<div>
//			@x.Text(count)
//			@x.El("button", x.On("click", count.Set(count.Add(1)))) {
//				+
//			}
//		</div>
//	}
//
// Text and the form markers (Input, Number, Checkbox) own the element they
// render. For elements that carry reactive attributes, styles or events
// instead, use [El] with bindings ([Attr], [Class], [Disabled], [Show],
// [Style], [On]). Values are derived with [Computed]; arbitrary browser work
// runs through [Effect]; structure changes through [When] and [List].
//
// Rendered inside a [Document] the markers emit live bindings; rendered
// without one they fall back to static HTML, which is what makes reactivity
// opt-in rather than a rewrite of the whole page. State that is never used in
// markup emits no JavaScript at all.
//
// The generated JavaScript is split into two scripts around the body: the
// runtime and state declarations load first, so any <script> in the page can
// read reactive state; the bindings load last, once the DOM exists. Signals
// created with [Named] keep their name in the generated code, so hand-written
// scripts and effects can reference them directly. Both depend on a small
// vendored runtime (see runtime.go), assembled per page from fragments so only
// the pieces a page uses are shipped; pass [Minified] to [Document] for
// compact production output.
package x
