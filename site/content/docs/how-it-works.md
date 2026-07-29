+++
Title = "How it works"
Description = "The walker, locale derivation, and dependency tracking — the three mechanisms, in full."
Order = 90
+++

astwerk has three moving parts worth understanding. None of them is large; all of
them are readable in one sitting.

## The walker

`Build` is a depth-first recursion over `Node`. There is no plugin pipeline, no
lifecycle, no pass ordering to reason about — one function, roughly a hundred
lines, and this is all it does per node:

```go
func buildNode(n Node, dir, sitePath string, loc *localeCtx, opts BuildOptions) error {
	// 1. make the directory, if this node has any output
	// 2. render Page to index.html, then each Files entry
	// 3. copy CopyFrom
	// 4. compile CompileFrom
	// 5. merge Generate() into Children — generated keys win
	// 6. recurse, appending each key to dir and sitePath
}
```

Two paths are threaded down in parallel: `dir` is where files land on disk, and
`sitePath` is what the page believes its URL to be. Keeping them separate is what
lets `BaseURL` change every generated link without moving a single output file.

Children are visited in sorted key order. That's deliberate: a build is
reproducible, and when something fails you get the same node reported every time
rather than whichever the map handed over first.

### Failure

Errors propagate immediately, wrapped with the path being written:

```
ssg: render build/projects/thing/index.html: …
```

No partial-success reporting, no continuing past a broken page. A site that
half-built is worse than one that didn't.

## Locale derivation

`BuildLocales` copies the base tree and mounts the copy under the locale's code.
The copy carries `Page`, `Files`, `Generate` and `Children` — and deliberately
drops `CopyFrom` and `CompileFrom`.

That omission is the interesting part. Stylesheets and scripts are shared across
languages and served from the site root, so inheriting them would produce
`build/de/style/` duplicating every asset per language. The rule is: a locale
subtree mirrors *pages*, not assets. Branches that end up with nothing to render
are pruned.

Overrides then replace nodes at a path, applied shallowest-first so an override of
`projects` lands before one of `projects/foo`. Locale identity is carried on an
unexported field of the locale's root node and inherited downward by the walker,
which is how a page five levels deep still knows its prefix.

## Dependency tracking

`reactive` uses two package-level variables. That's the entire mechanism:

```go
var (
	owner    *effect // who disposes what — nested effects attach here
	observer *effect // who depends on what — signal reads register here
)
```

`Signal.Get` links the current `observer` to the signal's subscriber list.
`Signal.Set` walks that list and re-runs each subscriber. Before an effect
re-runs, it unlinks from everything it read last time and re-collects as it goes —
which is why dependencies are dynamic for free, and why a branch not taken creates
no subscription.

Keeping `owner` and `observer` separate matters more than it looks. Conflating
them was a bug during development: `Untracked` has to stop dependencies being
recorded *without* orphaning effects created inside it, and with one variable
doing both jobs those effects escaped their parent and were never disposed.

### Change detection with an unconstrained type

`Signal[T]` accepts any `T`, so `==` isn't available at compile time. Comparison
goes through `any`, which works for comparable dynamic types and panics for
slices, maps and funcs:

```go
func equalValues[T any](a, b T) (eq bool) {
	defer func() {
		if recover() != nil {
			eq = false
		}
	}()
	return any(a) == any(b)
}
```

Recovering to "not equal" is the safe direction: a spurious re-run is wasteful,
a missed one is a bug.

### Why no virtual DOM

A VDOM exists to make "re-render everything" cheap. Fine-grained bindings make it
unnecessary — a binding knows which property it owns, so it writes that property
and nothing else. `BindList` is the only place doing real reconciliation, and it
works from keys you supply rather than by diffing rendered output.

The cost is that you write bindings instead of templates for interactive parts.
The benefit is that nothing is ever destroyed and rebuilt behind your back, so
focus, scroll position and transitions survive without special handling.

## Reading the source

Roughly in order of how much they'll teach you:

| File | What's in it |
| --- | --- |
| `ssg/build.go` | the walker |
| `ssg/node.go` | `Node`, `Ctx`, `BuildOptions` |
| `ssg/locale.go` | tree derivation and override application |
| `reactive/signal.go` | the dependency graph; no build constraints, so it's host-testable |
| `reactive/list.go` | keyed reconciliation |
| `wasmwrap/element.go` | the `syscall/js` wrapping style everything else follows |

`reactive/signal.go` carries no build constraint on purpose: the tracking logic is
pure Go and gets fast host tests, while only the DOM half needs node. Two real
bugs surfaced that way — the ownership split above, and `BindList` disposing the
very entries it was reusing.

## This site

Built with astwerk, source in [`site/`](https://github.com/LukasDerBaum42/astwerk/tree/master/site).
It's a nested Go module with a `replace` pointing at the parent, so it always
builds against the working tree — if a change breaks the library's own
documentation site, the site stops building.

Writing it found four gaps that nothing else had: no base-path support (every link
broke under `/astwerk/`), no way to configure the markdown renderer (so no syntax
highlighting), no locale-independent path for a language switcher, and no
`InsertBefore` for keyed list ordering. All four are in the library now. That is
what a real test is for.
