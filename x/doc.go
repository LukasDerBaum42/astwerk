// Package x is Astwerk's compiled reactive markup: ordinary Go functions that
// generate both the HTML and the small JavaScript a page needs to become
// interactive, with no virtual DOM, no framework, and no Go/WASM in the
// browser.
//
// State is one of four split types, each exposing only the operations
// meaningful for its value: [NumSignal] (int/int64/float64), [BoolSignal],
// [StrSignal] and [ListSignal]. Constructors are [NewNumSignal] and its
// [NamedNumSignal] siblings; [Computed] derives a read-only [Memo].
//
//	var count = x.NewNumSignal(0)
//	var open  = x.NewBoolSignal(false)
//
//	templ Counter() {
//		<div>
//			@count
//			@x.El("button", x.HtmlAttr("class", "btn"), x.On("click", count.Dec(1))) { - }
//			@x.El("button", x.HtmlAttr("class", "btn"), x.On("click", count.Inc(1))) { + }
//		</div>
//	}
//
// A signal is a [Value], which also satisfies [templ.Component] — @count and
// @count.Text() render the same bound span. Form markers are methods:
// @draft.Input(), @price.Number(), @agreed.Checkbox(). Expressions are
// package functions: [Add], [Sub], [Mul], [Div], [Concat], [Gt], [Lt], [Gte],
// [Lte], [Eq], [Ne], [Not], [Lit]. Handlers are actions built by the signal
// methods ([Set], [SetTo], [Inc], [Dec], [Toggle]) and composed with [Seq] and
// [If].
//
// Elements carry bindings through [El] or [Wrap]: [HtmlAttr], [Attr], [Class],
// [Disabled], [Show], [Style] and [On]. Structure changes through [When] and
// [List]; arbitrary browser work runs through [Effect] and [Script].
//
// A reactive page is a [Document] wrapping a component. Markers rendered
// inside a [Document] emit live bindings; rendered without one they fall back
// to static HTML, which is what makes reactivity opt-in. State that is never
// used in markup emits no JavaScript at all. The generated JavaScript is split
// into two scripts around the body: the runtime and state declarations load
// first, so any <script> in the page can read reactive state; the bindings
// load last, once the DOM exists. Signals created with a Named constructor
// keep their name in the generated code, so hand-written scripts and effects
// can reference them directly. The runtime is assembled per page from
// fragments so only the pieces a page uses are shipped; pass [Minified] to
// [Document] for compact production output.
package x
