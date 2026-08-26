# Astwerk Reactive System

**Status:** Draft  
**Version:** 0.2  
**Project:** Astwerk

---

## 0. Why This Document Exists

The current Astwerk `reactive` package (signals, `Computed`, `Effect`,
`BindText`/`BindWhen`/`BindList`, two-way form bindings, `Batch`,
`Untracked`, `Router`, `jsdom` for testing) works today, compiled to
WASM via `wasmwrap`. It is **not yet used in production.**

The problem this spec addresses is not "does reactivity work" — it
already does. The problem is **payload size**: `GOOS=js GOARCH=wasm`
output bundles the Go runtime and GC, which is heavy for what is
often a single counter or form on an otherwise static page. This
conflicts with Astwerk's "static/serverless site" positioning.

This document specifies a **second implementation** of the same
reactive model — same primitives, same semantics where possible —
that generates plain JavaScript at build time instead of compiling
Go to WASM. The WASM `reactive` package is the reference
implementation: where this spec is silent or vague, the WASM
package's actual behaviour is the default answer, not a blank slate.

Every place in this document marked **OPEN QUESTION** is a decision
that has not been made yet and needs Lukas's input before or during
implementation — these are not rhetorical; do not resolve them
silently.

### Open questions at a glance

| # | Section | Question |
|---|---------|----------|
| 1 | §8 References | Split DOM-bound references from state-to-state dependencies (Computed/Effect), or keep one struct with optional fields? |
| 2 | §9 DOM Node Tracking | Does node identity need to be stable across builds, or is per-build-only fine? |
| 3 | §13 State Mutation | Mirror WASM's `Set`/`Update`/`Peek` naming 1:1, or diverge (e.g. explicit `SetJS`)? |
| 4 | §14 Events | Explicit `Batch()` like the WASM version, or automatic per-handler batching? |
| 5 | §24.1 Lists | API shape, diff strategy/target scale, and whether list items need their own reactive sub-values |
| 6 | §24.2 Conditionals | Untaken branch: never generated vs. generated-but-hidden |
| 7 | §24.3 Cleanup | Does the runtime need a `Scope`/`OnCleanup` equivalent, or wait until lists/conditionals exist? |
| 8 | §24.4 Two-way binding | Add `.Bind()`-style sugar now, or manual `Get()` + `Event` for v1? |
| 9 | §24.5 Runtime budget | Hard byte ceiling now, or measure after first implementation? |
| 10 | §30 Security | Fixed per-attribute sanitization table, or generic escaping for v1? |

---

## 1. Overview

Astwerk's reactive system provides client-side reactivity without requiring:

- a virtual DOM,
- a frontend compiler,
- a templ preprocessor,
- Go/WASM in the browser,
- a JavaScript framework such as React or Vue.

The system uses **ordinary Go functions executed at build time** to generate the static HTML and the JavaScript required for client-side behaviour.

Astwerk's existing build context is responsible for collecting:

- generated HTML,
- JavaScript,
- reactive values,
- references,
- DOM nodes,
- components,
- scripts and other client-side resources.

The core principle is:

> **The Go program executing the build is the code generator.**

A reactive API call does not merely return a value. It can also register the necessary client-side code and relationships in the current Astwerk build context.

---

# 2. Design Goals

The reactive system MUST:

1. Work with existing templ components.
2. Require no modification to templ's syntax for basic functionality.
3. Require no source-code preprocessing.
4. Require no AST transformation of Go code.
5. Generate static HTML whenever possible.
6. Generate JavaScript only when required.
7. Prefer embedding generated JavaScript directly into the resulting HTML.
8. Support generating TypeScript when requested or useful.
9. Allow arbitrary JavaScript/TypeScript to participate in the reactive system.
10. Allow ordinary `<script>` tags to use Astwerk reactive values.
11. Allow reusable reactive components to be implemented entirely as Go functions.
12. Keep the browser-side runtime as small as practical.
13. Permit future optimisations without changing the public API.
14. Allow server/build-time Go values and client-side reactive values to coexist.

The system SHOULD:

1. Produce readable JavaScript in development builds.
2. Minimise generated JavaScript in production builds.
3. Deduplicate generated state and functions.
4. Avoid generating code for unused reactive values.
5. Avoid requiring hydration for completely static components.

---

# 3. Non-Goals

The initial implementation does NOT attempt to:

- compile arbitrary Go to JavaScript,
- infer reactivity from arbitrary Go code,
- transform arbitrary JavaScript into reactive JavaScript,
- provide a virtual DOM,
- recreate React's component lifecycle,
- require a JavaScript framework.

Astwerk is not intended to make arbitrary code magically reactive.

A value becomes reactive because it is created/accessed through the Astwerk reactive API.

---

# 4. Architecture

The build pipeline is:

```text
                    Go program
                       │
                       ▼
               Astwerk Context
                       │
          ┌────────────┴────────────┐
          │                         │
          ▼                         ▼
     templ components          reactive API
          │                         │
          ▼                         ▼
        HTML                    JS IR
          │                         │
          └────────────┬────────────┘
                       ▼
                 finalization
                       │
             ┌─────────┴─────────┐
             │                   │
             ▼                   ▼
        static HTML         generated JS
                                 │
                                 ▼
                         embedded <script>
```

The JavaScript intermediate representation is not a source compiler.

It exists only to allow Astwerk's Go functions to construct JavaScript safely, deduplicate declarations, track references, and generate the final output.

---

# 5. Astwerk Context

The existing Astwerk context is extended with reactive build information.

Conceptually:

```go
type Context struct {
    // Existing Astwerk state...

    Reactive *ReactiveContext
    JS       *JSProgram
}
```

`ReactiveContext` stores all information required to generate client-side behaviour.

Conceptually:

```go
type ReactiveContext struct {
    States      map[StateID]*StateNode
    References  []Reference
    Nodes       map[NodeID]*DOMNode
    Components  map[ComponentID]*ComponentNode
}
```

The exact structures are implementation details.

The public API MUST NOT require users to manually manipulate these structures.

---

# 6. Reactive Values

A reactive value represents state which exists both:

1. as a build-time Go value, and
2. as a client-side JavaScript value.

Conceptually:

```go
count := aw.State(0)
```

creates:

```text
State
├── ID: 0
├── Go value: 0
└── JS representation: $s0
```

The state does not necessarily generate JavaScript immediately.

JavaScript SHOULD only be generated once the state is actually used.

This allows unused states to disappear entirely from the output.

---

# 7. `Get`

`Get()` is the fundamental reactive operation.

```go
count.Get()
```

has two meanings during a build.

### Build-time meaning

Return the current Go value.

For example:

```go
count := aw.State(42)

fmt.Println(count.Get())
```

returns:

```text
42
```

### Client-side meaning

Register that the current output location depends on the state.

For example:

```templ
<span>{count.Get()}</span>
```

causes Astwerk to record a reference approximately equivalent to:

```text
State #0
    ↓
DOM node #17
    ↓
text content
```

Astwerk can then generate the necessary JavaScript.

---

# 8. References

Every reactive access MAY create a `Reference`.

A reference contains information about where and how a reactive value was used.

Conceptually:

```go
type Reference struct {
    State       StateID
    Node        NodeID
    Component   ComponentID

    Kind        ReferenceKind

    Attribute   string
    Location    SourceLocation
}
```

Possible reference kinds include:

```text
Text
Attribute
Property
Class
Style
Event
Script
Computed
Effect
```

The exact set can evolve.

The reference count is derived from the references:

```go
len(state.References)
```

A separate integer reference counter is therefore unnecessary unless profiling demonstrates that it provides a meaningful optimisation.

> **OPEN QUESTION — Reference struct shape.**
> `Reference{State, Node, Component, Kind, Attribute, Location}` assumes
> every reference is bound to a DOM node. `Computed` and `Effect` (§15,
> §16) don't have a node — a computed value's "consumer" is another
> `State`, and an effect's consumer is a callback, not a DOM location.
> The WASM `reactive` package keeps these separate: dependency tracking
> between signals (`Signal`/`Computed`/`Effect`) is independent of DOM
> binding (`BindText`, `BindAttr`, etc.). Two options for this spec:
> 1. Split into `DOMReference` (node-bound: Text/Attribute/Class/Style/Event)
>    and `Dependency` (state-to-state graph edges, used by Computed/Effect).
> 2. Keep one struct, make `Node`/`Attribute` optional/nullable for
>    non-DOM-bound kinds.
> **Which do you want — or is there a third shape you have in mind?**

---

# 9. DOM Node Tracking

Reactive DOM references require stable build-time identities.

When Astwerk creates:

```templ
<span>{count.Get()}</span>
```

the element receives an internal node ID.

Conceptually:

```text
span
└── NodeID: 17
```

The initial implementation MAY emit this ID into the HTML:

```html
<span data-aw-node="17">42</span>
```

This is primarily a correctness/debugging mechanism.

Production output SHOULD eventually use a more compact representation or eliminate unnecessary attributes where possible.

> **OPEN QUESTION — Node ID stability.** If IDs are assigned by build
> traversal order (a monotonic counter), the same logical node can get a
> different ID between builds whenever unrelated content changes the
> traversal order upstream of it (e.g. a conditionally-rendered sibling
> appears/disappears — see §33). This matters for:
> - **Dev-mode incremental rebuilds** — does live-reload state (if any
>   persists across a rebuild) stay attached to the right node?
> - **§33/§34 (lists/conditionals)** — once nodes can be added/removed at
>   runtime, a pure counter can't describe "the node that would exist at
>   position N if the list had M items."
>
> Options:
> 1. Keep the monotonic counter — simplest, fine if nothing needs
>    cross-build or cross-navigation stability.
> 2. Content-addressed IDs (hash of component path + position within it) —
>    stable across builds as long as the component's structure doesn't
>    change above that point.
> 3. Anchor-based scheme for dynamic regions specifically (see §33) with
>    plain counters everywhere else.
> **Do you need cross-build ID stability at all, or is per-build-only
> fine since state doesn't persist across a full page reload anyway?**

---

# 10. Generated JavaScript

Astwerk SHOULD generate JavaScript directly into the resulting HTML.

Example:

```html
<span data-aw-node="17">42</span>

<script>
const $s0 = aw.state(42);
aw.text(17, $s0);
</script>
```

The exact generated representation is implementation-defined.

A production implementation SHOULD attempt to produce output closer to:

```html
<span>42</span>

<script>
let s0=aw.s(42);
aw.t(17,s0);
</script>
```

or an equivalent minimal representation.

The generated JavaScript SHOULD NOT require a large framework runtime.

---

# 11. JavaScript Output Modes

Astwerk supports two output modes.

## 11.1 Embedded JavaScript

Default.

Generated code is inserted into the resulting HTML:

```html
<script>
    // generated Astwerk code
</script>
```

This is preferred because:

- no additional assets are required,
- small applications can remain a single HTML file,
- generated code can be specialised for the page,
- deployment remains extremely simple.

## 11.2 Generated TypeScript

Optional.

Astwerk MAY emit TypeScript instead:

```text
dist/
├── index.html
└── index.ts
```

The existing Astwerk TypeScript compiler can then produce the final JavaScript.

TypeScript generation SHOULD primarily be useful for:

- development,
- larger applications,
- debugging,
- user-authored TypeScript,
- applications where external bundling is desirable.

The reactive API itself MUST NOT depend on TypeScript.

---

# 12. JavaScript Representation

Astwerk SHOULD NOT immediately concatenate JavaScript strings whenever a reactive function is called.

Instead it maintains a lightweight JavaScript representation.

For example:

```go
type JSProgram struct {
    Declarations []JSDeclaration
    Expressions  []JSExpression
    Statements   []JSStatement
}
```

This permits:

- deduplication,
- stable IDs,
- dependency ordering,
- minification,
- dead-code elimination,
- development debugging.

This representation is an output builder rather than a source-code compiler.

---

# 13. State Mutation

State mutation is represented as a client-side operation.

A build-time call such as:

```go
count.Set(10)
```

MUST NOT automatically mean "execute this mutation in the browser."

Instead, Astwerk needs a distinction between:

```text
build-time execution
```

and:

```text
generated client-side execution
```

For example, the API MAY expose:

```go
count.Set(...)
count.Update(...)
count.SetJS(...)
count.UpdateJS(...)
```

or an event/action abstraction.

> **OPEN QUESTION — mutation API shape.** The WASM `reactive` package
> already settled this for the Go/WASM version: `Signal.Set`, `Update`,
> `Peek`, `Subscribe`. The straightforward move is to mirror that surface
> here (`count.Set(10)`, `count.Update(func(v int) int {...})`) and treat
> every call made from *inside* an event/effect closure as "generate
> client-side code" while every call made at top-level build time is
> "execute now" — i.e. the distinction is *context*, not a separate
> `SetJS`/`UpdateJS` method name.
> **Do you want to mirror the WASM naming 1:1 (`Set`/`Update`/`Peek`), or
> is there a reason to diverge (e.g. `SetJS` as an explicit escape hatch
> for cases where context-inference is ambiguous)?**

The critical requirement is that users must be able to express:

```text
when browser event X occurs:
    modify state Y
```

without executing the modification during the build.

---

# 14. Events

Reactive components need a way to construct browser event handlers.

Example conceptual API:

```go
button.On("click", count.Increment())
```

or:

```go
button.On("click", aw.Action(func() {
    count.Update(...)
}))
```

The function passed to an event MUST be interpreted as a description of client-side behaviour, not ordinary build-time execution.

The resulting JavaScript could be:

```js
node.addEventListener("click", () => {
    s0++;
});
```

> **OPEN QUESTION — update batching.** If a handler sets multiple states,
> does each `.Set()`/`.Update()` trigger its own immediate DOM write, or
> do writes coalesce into one microtask/`requestAnimationFrame` flush
> before touching the DOM? The WASM package has `Batch(func() {...})` as
> an explicit opt-in. Two directions:
> 1. Mirror it exactly — batching is explicit via `aw.Batch(...)`,
>    unbatched writes are synchronous (matches WASM behaviour, keeps
>    generated JS runtime simplest).
> 2. Batch every event handler by default (auto-flush at the end of the
>    handler), since that's the common case and avoids a footgun where
>    someone forgets to wrap a multi-`Set()` handler in `Batch`.
> **Which model do you want — explicit `Batch()` like the WASM version,
> or automatic per-handler batching?** This affects the shape of
> `aw.state()`/`aw.text()` in the generated runtime, so it's worth
> deciding before Phase 5 implementation starts.

---

# 15. Computed Values

The system SHOULD support derived reactive values.

Example:

```go
total := aw.Computed(func() int {
    return price.Get() * quantity.Get()
})
```

During the build, the function executes normally.

While executing:

```go
price.Get()
quantity.Get()
```

register their use as dependencies of `total`.

Conceptually:

```text
price ───────┐
             ▼
           total
             │
             ▼
quantity ────┘
```

The generated JavaScript represents the same computation.

This dependency tracking happens because the reactive functions are executed during the build.

No Go AST analysis is required.

---

# 16. Effects

The system MAY support effects:

```go
aw.Effect(func() {
    log.Println(value.Get())
})
```

The build execution establishes the dependencies.

The browser-side representation runs again whenever one of those dependencies changes.

Effects are intended for operations such as:

- modifying document state,
- interacting with browser APIs,
- synchronising external systems,
- custom JavaScript behaviour.

Effects SHOULD be used sparingly.

---

# 17. Arbitrary JavaScript

Astwerk MUST support arbitrary user-authored JavaScript.

For example:

```go
aw.JS(`
    console.log("Hello from Astwerk");
`)
```

must be possible.

The generated code is inserted into the final page without requiring it to pass through the reactive system.

This provides an escape hatch for functionality not covered by the Astwerk API.

---

# 18. Reactive JavaScript

Arbitrary JavaScript can also participate in the reactive system through explicit access to Astwerk state.

For example, generated/user-authored JavaScript might conceptually use:

```js
state.get()
```

or:

```js
state.subscribe(...)
```

The reactive runtime MUST expose a minimal JavaScript API for this purpose.

Astwerk does NOT need to understand arbitrary JavaScript source code.

Instead:

```text
Astwerk reactive API
        │
        ▼
generated JavaScript
        │
        ▼
user JavaScript
```

This means arbitrary JavaScript can consume reactive state without Astwerk parsing it.

---

# 19. `<script>` Tags

Ordinary templ/HTML script tags MUST remain valid.

For example:

```templ
<script>
    console.log("hello");
</script>
```

must work unchanged.

Astwerk SHOULD additionally provide a mechanism for exposing reactive values to such scripts.

Conceptually:

```templ
<script>
    const value = $astwerk.state(...);
</script>
```

or through generated bindings.

The exact syntax should be determined during implementation.

The important architectural rule is:

> `<script>` is not special-cased by a compiler. It receives access to the same JavaScript runtime and state objects generated by Astwerk.

---

# 20. Reactive JavaScript Functions

Astwerk SHOULD allow Go functions to create reusable JavaScript functions.

For example:

```go
format := aw.JSFunction(...)
```

could produce:

```js
function format(value) {
    ...
}
```

The function can then be used by multiple components without duplication.

A generated function should be emitted once even if referenced many times.

---

# 21. Premade Components

Components remain ordinary Go/templ functions.

Example:

```go
func Counter(count *State[int]) templ.Component {
    // templ output
}
```

A component does not require special compiler support.

It can simply call the reactive API while rendering.

This allows component libraries to be written entirely in Go.

For example:

```text
astwerk/components
├── Button
├── Input
├── Counter
├── Tabs
├── Modal
└── ...
```

All of these can use the same reactive primitives.

---

# 22. Static Optimisation

The build system SHOULD automatically avoid generating client-side code when it is unnecessary.

Example:

```go
name := aw.State("Lukas")
```

with no client-side references produces:

```text
JavaScript generated: none
```

Likewise:

```templ
<h1>Hello World</h1>
```

produces only static HTML.

This is a fundamental Astwerk design principle:

> **Static by default, reactive by necessity.**

---

# 23. Server/Build-Time Values

Normal Go values remain ordinary build-time values.

For example:

```go
user := database.GetUser()

templ Profile() {
    <h1>{user.Name}</h1>
}
```

does not become reactive.

This is simply static HTML generation.

Only values explicitly created through the reactive API participate in client-side reactivity.

This distinction is essential.

---

# 24. Reactive Boundaries

A reactive reference can occur at multiple levels:

```text
Page
├── static Header
├── reactive Counter
│   ├── text reference
│   └── event reference
├── static Article
└── reactive UserStatus
```

Astwerk SHOULD avoid turning the entire page into one reactive object.

Only the necessary DOM nodes and JavaScript behaviour should become reactive.

---

# 24.1 Dynamic List Rendering

**Status: not covered by the original draft. This is the single biggest
gap between this spec and the WASM `reactive` package's `BindList`.**

Everything in §7–§10 assumes a fixed DOM shape: a `Reference` points at
a node that always exists, and only its content/attributes change.
`BindList` in the WASM package is different in kind — it adds, removes,
and reorders actual DOM nodes as a backing slice changes, keyed so that
existing nodes are moved rather than destroyed and recreated (preserving
input focus, scroll position, etc. inside list items).

A single `Reference{Node: 17, Kind: Text}` cannot express "the node that
would exist at index 3 if the list currently has 5 items." List
rendering needs an **anchor** — a stable marker in the DOM (e.g. a
comment node or empty text node) that keyed items are inserted
before/after — rather than a single `NodeID`.

Conceptually:

```go
items := aw.State([]Item{...})

aw.List(items, func(item Item) templ.Component {
    return ItemRow(item)
}, aw.Key(func(item Item) string { return item.ID }))
```

which conceptually produces:

```html
<!--aw-list-start:3-->
<!-- item nodes go here -->
<!--aw-list-end:3-->
```

and generated JS that diffs the incoming slice against the currently
rendered keys, moving/inserting/removing DOM nodes accordingly (a
minimal keyed-diff, not a full VDOM reconciler — closer to how
`BindList` in the WASM package already works).

> **OPEN QUESTION — list API shape and diff strategy.**
> 1. Should the Go-side API mirror `BindList` as closely as possible
>    (same key function signature, same semantics), or does the
>    build-time/JS-codegen split change what's natural here?
> 2. Keyed diffing needs *some* algorithm — even a minimal one (e.g.
>    LIS-based reordering vs. naive remove/reinsert) has real performance
>    tradeoffs at large list sizes. Is there a target list size this
>    needs to perform well at (tens of items vs. thousands)?
> 3. Do list items themselves need to contain reactive sub-values (e.g.
>    each `Item` has its own editable field), or is the common case a
>    list of static rows where only membership/order changes?
> **This needs a decision before Phase 5 in §31, since it changes the
> node-identity model those phases assume.**

---

# 24.2 Conditional Rendering

**Status: not covered by the original draft.** Equivalent to `BindWhen`
in the WASM package — mount/unmount a subtree based on a boolean (or
other) condition, rather than mutating an existing node's content.

Conceptually:

```go
templ Panel() {
    if loggedIn.Get() {
        <UserMenu/>
    } else {
        <LoginButton/>
    }
}
```

This needs the same anchor-node mechanism as §24.1 (a conditional region
isn't a fixed node either — it's zero or one subtree between two
anchors), and needs a decision on whether the "off" branch's HTML is:

- **Never generated** into the initial static HTML (built client-side
  from a template on first activation) — smaller initial payload, more
  generated JS to build the DOM.
- **Generated but hidden** (`display:none` or a `<template>` tag) —
  bigger initial payload, no client-side DOM construction needed, only
  visibility toggling.

> **OPEN QUESTION — which strategy for the untaken branch, and does it
> match what `BindWhen` already does in the WASM package (worth staying
> consistent unless there's a reason not to)?**

---

# 24.3 Effect Cleanup and Unmount

**Status: not covered by the original draft.** Once §24.1/§24.2 exist,
subtrees can be removed from the DOM at runtime. Any `Effect` or event
listener registered for state that lived only inside that subtree needs
to be disposed, or it leaks (a listener bound to a removed node, a timer
still running against unmounted state).

The WASM package handles this with `OnCleanup`/`Scope` — effects nest
and dispose their children when the parent scope is torn down.

> **OPEN QUESTION — does the JS-codegen runtime need an equivalent
> `Scope`/`OnCleanup` concept, or can this wait until §24.1/§24.2 are
> actually implemented (i.e. is this purely theoretical until dynamic
> lists/conditionals exist, since without them nothing is ever
> unmounted)?**

---

# 24.4 Two-Way Binding

**Status: not covered by the original draft.** Forms are one of the most
common reactive use cases, and right now this spec only offers `Get()`
(read) plus a separate `Event` reference (write) — meaning a bound
`<input>` requires manually wiring both directions. The WASM package has
explicit sugar for this: `BindValue`, `BindNumber`, `BindChecked`.

Conceptual API:

```go
<input value={name.Bind()} />
```

or an explicit two-way helper mirroring the WASM names.

> **OPEN QUESTION — is a `.Bind()`-style convenience worth adding to this
> spec now, mirroring `BindValue`/`BindNumber`/`BindChecked`, or is
> manual `Get()` + `Event` wiring acceptable for v1 since it's more
> primitive and composes with the existing API without a new concept?**

---

# 24.5 Runtime Size Budget

**Status: not covered by the original draft, but this is the entire
motivation for this spec existing (see §0).** §11.1/§22 talk about
minimising generated JavaScript, but nothing sets a concrete target.
Without one, `BindList`/`BindWhen` equivalents (§24.1/§24.2) can
gradually creep the shared runtime back toward the size this rewrite
exists to avoid.

> **OPEN QUESTION — what's the actual byte budget?** Some reference
> points to react to, not a recommendation:
> - A minimal signals runtime (state + text binding + event binding
>   only) can plausibly stay under ~1KB minified.
> - Adding keyed-list diffing (§24.1) is usually the single biggest
>   line-item in a from-scratch reactive runtime's size.
> **Do you want a hard ceiling (e.g. "core runtime must stay under 2KB
> minified, list/conditional support can add up to N KB more, only
> loaded on pages that use it"), or is this something to measure after
> a first implementation rather than target up front?**

---

# 25. Example

Given:

```go
count := aw.State(0)
```

and:

```templ
templ Counter() {
    <div>
        <button onclick={count.Increment()}>
            +
        </button>

        <span>
            {count.Get()}
        </span>
    </div>
}
```

The build executes the component.

The resulting HTML is conceptually:

```html
<div>
    <button data-aw-node="1">+</button>
    <span data-aw-node="2">0</span>
</div>
```

and the generated JavaScript is conceptually:

```js
const s0 = aw.state(0);

aw.text(2, s0);

aw.event(1, "click", () => {
    s0.update(v => v + 1);
});
```

The browser then performs the updates without rerendering the component or using a virtual DOM.

---

# 26. Computed Example

Given:

```go
price := aw.State(10)
quantity := aw.State(2)

total := aw.Computed(func() int {
    return price.Get() * quantity.Get()
})
```

and:

```templ
<span>{total.Get()}</span>
```

the build establishes:

```text
price ──────┐
            ▼
          total
            │
            ▼
         <span>
            ▲
            │
quantity ───┘
```

The resulting JavaScript can implement the equivalent dependency graph.

No source-code analysis is required.

The graph exists because the Go functions were actually executed.

---

# 27. Error Handling

Reactive API calls outside an active Astwerk build context SHOULD produce a clear error.

For example:

```text
Astwerk reactive value accessed outside build context.
```

Likewise, operations that require a DOM node but occur outside a DOM-producing context SHOULD fail during the build rather than generating invalid JavaScript.

Errors SHOULD contain:

- component name,
- source location where available,
- reactive value ID,
- operation being performed.

---

# 28. Development Mode

Development builds SHOULD provide debugging metadata.

For example:

```text
State #12
Created by: Counter
References:
  node #42 text
  node #43 class
  node #51 event
```

The generated HTML MAY contain readable node IDs and state IDs.

This should make debugging generated reactive behaviour possible without inspecting an enormous generated JavaScript file.

---

# 29. Production Mode

Production builds SHOULD:

- remove unused state,
- deduplicate functions,
- minimise identifiers,
- remove development metadata,
- minimise runtime code,
- combine compatible generated scripts,
- avoid unnecessary DOM markers.

The optimisation phase MUST NOT change reactive semantics.

---

# 30. Security

User-provided strings MUST NOT be blindly inserted into generated JavaScript.

The JS builder MUST distinguish between:

```text
JavaScript source
JavaScript string literals
HTML
HTML attributes
identifiers
```

Generated code SHOULD use proper escaping and/or structured representations rather than string concatenation.

This is especially important because reactive values may contain arbitrary user data.

> **OPEN QUESTION — per-attribute escaping rules.** Generic HTML/JS-string
> escaping isn't enough for every `Attribute`/`Style`/`Property` reference
> kind: `href`/`src` need scheme validation (block `javascript:` etc.),
> and `style` needs protection against CSS injection (e.g. an attacker
> supplying `background:url(...)` or breaking out of a property value).
> **Should the runtime maintain a fixed table of attribute-name →
> sanitization-rule (e.g. `href`/`src` get URL-scheme allowlisting, `style`
> gets a restricted property setter instead of raw string assignment), or
> is generic escaping considered sufficient for v1 and this gets
> revisited later?**

---

# 31. Implementation Phases

### Phase 1 — JS output system

Implement:

```text
Context
JSProgram
JS expression/statement representation
<script> generation
```

No reactivity yet.

Goal:

```go
aw.JS("console.log('hello')")
```

produces valid output.

---

### Phase 2 — State

Implement:

```go
State[T]
Get()
```

with:

- unique IDs,
- Go-side values,
- usage tracking,
- JavaScript state declaration.

Goal:

```go
state := aw.State(42)
state.Get()
```

can produce a JS representation.

---

### Phase 3 — DOM references

Implement:

```text
DOM node IDs
current-node context
Reference
```

Goal:

```templ
<span>{state.Get()}</span>
```

can identify exactly which DOM node consumes the state.

---

### Phase 4 — Text reactivity

Implement:

```text
state.Get()
    ↓
text node binding
```

Goal:

```text
state changes
    ↓
span text changes
```

No other DOM is modified.

---

### Phase 5 — Events and mutation

Implement browser-side state mutation.

Goal:

```text
click
 ↓
state update
 ↓
dependent DOM update
```

> Depends on the open questions in §13 (mutation API shape) and §14
> (batching) being settled first — both affect the runtime API shape
> this phase produces.

---

### Phase 5.5 — Lists and conditionals (NEW)

Implement §24.1 (dynamic lists) and §24.2 (conditional rendering).

Goal:

```text
list.Set(newItems)
    ↓
DOM nodes added/removed/reordered, matching items are moved not rebuilt
```

Placed here rather than later because it changes the node-identity
model (§9) that later phases (attributes, computed values, effects)
build on — retrofitting it after Phase 8 would mean revisiting
everything downstream. Depends on the §24.1/§24.2/§24.3/§9 open
questions being settled first.

---

### Phase 6 — Attributes/properties

Add:

```text
class
style
value
disabled
checked
href
...
```

---

### Phase 7 — Computed values

Implement dependency tracking through Go function execution.

---

### Phase 8 — Effects

Implement browser-side effects.

---

### Phase 9 — Arbitrary JS integration

Expose the reactive runtime to:

- `aw.JS(...)`,
- `<script>`,
- generated TypeScript,
- user-written JavaScript.

---

### Phase 10 — Optimisation

Implement:

- dead-state elimination,
- deduplication,
- compact IDs,
- runtime minimisation,
- production output optimisation.

---

# 32. Fundamental Design Principle

Astwerk should **not** become a frontend compiler.

The system should instead exploit the fact that the application already executes Go functions at build time.

A call such as:

```go
state.Get()
```

is simultaneously:

```text
1. A normal Go operation
2. A request for the current build-time value
3. A declaration of client-side usage
```

Likewise, a component function is simultaneously:

```text
1. A normal Go function
2. An HTML generator
3. A generator of the client-side program required by that HTML
```

The final application is therefore the result of executing the Go program:

```text
                  BUILD TIME
                       │
              Go + templ + Astwerk
                       │
             ┌─────────┴─────────┐
             ▼                   ▼
          HTML                  JS
             │                   │
             └─────────┬─────────┘
                       ▼
                    Browser
```

The browser never needs Go.

The browser never needs templ.

The browser only receives the static HTML and the small JavaScript program that the Go build generated.

This is the defining architectural property of Astwerk's reactive system.