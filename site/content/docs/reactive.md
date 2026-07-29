+++
Title = "reactive"
Description = "Signals, fine-grained DOM bindings, keyed lists, routing, async data."
Order = 70
+++

`reactive` is opt-in state management on top of `wasmwrap`. It is fine-grained
rather than virtual-DOM based: a binding updates exactly the property it owns,
and a keyed list moves nodes rather than rebuilding them. Nothing here re-renders
a tree and diffs the result.

See it running on the [Examples](../../examples/) page.

## Signals

```go
count := reactive.NewSignal(0)

count.Get()    // read, and subscribe if inside an Effect
count.Peek()   // read without subscribing
count.Set(5)
count.Update(func(v int) int { return v + 1 })
```

`Set` compares against the current value and does nothing if they're equal, so a
redundant write wakes nobody.

## Dependencies are tracked, not declared

`Get` records a dependency on whatever effect is currently running. An effect
therefore subscribes to exactly what it read on its **last** run:

```go
reactive.Effect(func() {
	if useA.Get() {
		fmt.Println(a.Get()) // only a is a dependency
	} else {
		fmt.Println(b.Get()) // only b is
	}
})
```

A branch not taken creates no dependency, and flipping `useA` swaps which signal
triggers the effect. You never register or clean up a subscription by hand.

## Computed

```go
full := reactive.Computed(func() string {
	return first.Get() + " " + last.Get()
})

full.Get() // "Ada Lovelace"
```

A `Memo` recomputes when its inputs change and is itself trackable, so derived
values chain. It also gates: if the recomputed value is equal to the previous
one, downstream effects don't run.

## Ownership

Effects nest. One created inside another is disposed and rebuilt when the outer
re-runs, so bindings inside a view that gets rebuilt don't accumulate:

```go
reactive.Effect(func() {
	user := current.Get()
	reactive.BindText(nameEl, func() string { return user.Name })
	// this inner binding is replaced, not duplicated, on every outer run
})
```

`OnCleanup` hooks into the same lifecycle, and `Scope` groups effects under one
owner without making them reactive to anything:

```go
dispose := reactive.Scope(func() {
	reactive.BindText(a, …)
	reactive.BindText(b, …)
})
dispose() // tears down both
```

Every `Bind*` returns a dispose func. Inside an effect it's owned for you, so the
return is usually ignorable — it matters at the top level of a script.

## Batching

```go
reactive.Batch(func() {
	first.Set("Ada")
	last.Set("Lovelace")
})
```

One update per affected effect instead of one per write. Batches nest.

## DOM bindings

Each is a one-liner over `Effect`, which is the point — "re-run this and poke one
property" is the whole job, and writing the effect out by hand every time is the
boilerplate worth removing.

```go
reactive.BindText(el, func() string { return strconv.Itoa(count.Get()) })
reactive.BindHTML(el, renderedMarkdown.Get)
reactive.BindClass(el, "active", isActive.Get)
reactive.BindStyle(el, "color", colour.Get)
reactive.BindShow(el, visible.Get)
reactive.BindAttr(el, "disabled", func() string {
	if busy.Get() { return "disabled" }
	return "" // empty removes the attribute
})
```

Form fields are two-way:

```go
reactive.BindValue(input, draft)    // string
reactive.BindNumber(input, amount)  // float64, ignores unparseable input
reactive.BindChecked(box, done)     // bool
```

These don't feed back on themselves: typing sets the signal, and the effect that
echoes it into the field stops because `Set` ignores an equal write.

Use `reactive.On` rather than `Element.On` inside an effect — it registers the
unbind with the enclosing effect, so a re-render doesn't stack up handlers.

## Conditional subtrees

```go
reactive.BindWhen(parent, loggedIn.Get, func() wasmwrap.Element {
	return renderDashboard()
})
```

The subtree is built when the condition becomes true and removed when it goes
false, so effects inside it are genuinely fresh each time. That's the difference
from `BindShow`, which only toggles visibility and keeps state.

## Keyed lists

```go
reactive.BindList(list, items.Get,
	func(t Todo) string { return t.ID },
	func(t Todo) wasmwrap.Element {
		row := wasmwrap.Create("li")
		reactive.BindClass(row, "done", t.Done.Get)
		return row.Append(…)
	})
```

An element is rendered once per key and then reused. Adding, removing or
reordering moves existing nodes, so input focus, scroll position and CSS
transitions survive — try it on the [Examples](../../examples/) page: type into a
row, then add another.

Two consequences worth knowing:

**Changing a field of an existing item does not re-render it.** Keys identify
elements, so make the parts that change signals:

```go
type Todo struct {
	ID   string
	Text string
	Done *reactive.Signal[bool] // toggling this patches just the checkbox
}
```

**The parent should be dedicated to the list.** Ordering is done by index against
the parent's children, so unrelated siblings would get shuffled.

## Routing

```go
reactive.Router(root, []reactive.Route{
	{Path: "/", View: home},
	{Path: "/projects/:slug", View: func(p reactive.Params) wasmwrap.Element {
		return project(p.Get("slug"))
	}},
	{Path: "/*", View: notFound},
})
reactive.InterceptLinks(wasmwrap.Body())
```

Patterns match segment by segment: `:name` captures one segment, `*` catches
whatever remains. Routes are tried in order, so put specific ones first. Each
view gets its own scope, so navigating away disposes its effects.

`Navigate` and `Replace` change the URL; `Path()` reads it as a tracked signal so
anything can react to navigation. `InterceptLinks` makes same-origin anchor
clicks route client-side, leaving modified clicks, external links and
`data-native` anchors alone so ctrl-click and downloads still behave.

## Async data

```go
posts := reactive.NewResource(func() ([]Post, error) {
	return reactive.FetchJSON[[]Post]("/api/posts")
})

reactive.BindShow(spinner, posts.Loading.Get)
reactive.BindList(list, posts.Data.Get, postID, renderPost)
reactive.BindText(errBox, func() string {
	if err := posts.Err.Get(); err != nil {
		return err.Error()
	}
	return ""
})
```

`Data`, `Err` and `Loading` are signals, so all three states bind like anything
else. The loader runs on its own goroutine, so a blocking `Fetch` belongs there.
`Reload` re-runs it, and a stale response can't overwrite a newer one.

## Using templ in the browser

templ is pure Go, so it compiles to wasm and runs client-side unchanged. There
are two ways to use it and they are not equally good.

**Bind to markup astwerk already generated** — the recommended default. Your
templ renders at build time; the script attaches behaviour:

```go
reactive.Hydrate("#counter", func(root wasmwrap.Element) {
	reactive.BindText(root.Find("[data-count]"), func() string {
		return strconv.Itoa(count.Get())
	})
	reactive.On(root.Find("[data-inc]"), "click", func(wasmwrap.Event) {
		count.Update(func(v int) int { return v + 1 })
	})
})
```

Markup lives in one place, updates stay surgical, nothing renders twice, and the
page still reads with WebAssembly disabled. `Hydrate` does nothing if the
selector matches nothing, so one script can be included on every page and act
only where its markup exists.

**Re-render a fragment client-side** — `BindTempl` runs a component in the browser
and assigns the result as innerHTML:

```go
reactive.BindTempl(preview, func() templ.Component {
	return views.Markdown(source.Get())
})
```

Understand the trade-off first. templ produces a *string*, so there is nothing to
patch against: everything inside the target is destroyed and rebuilt, losing input
focus, scroll position, selection, and any handler bound to a node inside. Good
for a display-only panel that changes as a unit — a rendered preview, a summary
table. Wrong for anything the user interacts with.

## Threading

Go on js/wasm runs one goroutine at a time, so signals need no locking. A
goroutine may still write one — that's how `Resource` reports its result — and the
effects it triggers run on that goroutine.
