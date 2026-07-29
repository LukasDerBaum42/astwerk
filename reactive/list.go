//go:build js && wasm

package reactive

import "github.com/LukasDerBaum42/astwerk/wasmwrap"

type listEntry struct {
	el      wasmwrap.Element
	dispose func()
}

// BindList keeps parent's children in sync with a reactive slice, keyed.
//
// An element is rendered once per key and then reused: adding, removing or
// reordering items moves existing nodes rather than rebuilding them, so input
// focus, scroll position and CSS transitions survive. This is the "targeted DOM
// patching" the design calls for — there is no virtual DOM and no diffing of
// rendered output.
//
// parent should be a container dedicated to the list. Ordering is done by index
// against parent's children, so unrelated siblings would be shuffled.
//
// Because entries are keyed, changing a field of an existing item does not
// re-render it. Make the parts that change signals:
//
//	type Todo struct {
//		ID   string
//		Text string
//		Done *reactive.Signal[bool] // toggling this patches just the checkbox
//	}
func BindList[T any](
	parent wasmwrap.Element,
	items func() []T,
	key func(T) string,
	render func(T) wasmwrap.Element,
) (dispose func()) {
	live := map[string]listEntry{}

	stop := Effect(func() {
		next := items()

		seen := make(map[string]bool, len(next))
		ordered := make([]wasmwrap.Element, 0, len(next))

		for _, item := range next {
			k := key(item)
			if seen[k] {
				// A duplicate key would mean two entries fighting over one
				// element; skipping is less surprising than corrupting the list.
				continue
			}
			seen[k] = true

			entry, ok := live[k]
			if !ok {
				// Detached: this entry's bindings must outlive the list effect's
				// next run, so they can't be its children.
				entry = detached(func() listEntry {
					var el wasmwrap.Element
					d := Scope(func() { el = render(item) })
					return listEntry{el: el, dispose: d}
				})
				live[k] = entry
			}
			ordered = append(ordered, entry.el)
		}

		for k, entry := range live {
			if !seen[k] {
				entry.dispose()
				entry.el.Remove()
				delete(live, k)
			}
		}

		for i, el := range ordered {
			if cur := parent.ChildAt(i); !cur.Value().Equal(el.Value()) {
				parent.InsertBefore(el, cur)
			}
		}
	})

	return func() {
		stop()
		for k, entry := range live {
			entry.dispose()
			entry.el.Remove()
			delete(live, k)
		}
	}
}
