//go:build js && wasm

package reactive

import "github.com/LukasDerBaum42/astwerk/wasmwrap"

type listEntry struct {
	el      wasmwrap.Element
	dispose func()
}

// Row is the per-row state factory handed to a list row. Signals created
// through it are row-owned: the row's bindings live in a scope that [BindList]
// disposes when the row is removed, so state never leaks into the next row with
// the same key. Row state is UI state — write it back into the item list
// explicitly with [ListSignal.Update].
//
// Bool and Str cover the common per-row state; numeric row state is rare enough
// that it uses [Num] directly (Go 1.25 has no generic methods, so a Num factory
// cannot live here).
type Row struct{}

// Bool returns a bool signal owned by the row, disposed when the row is
// removed so per-row state never leaks into the next row with the same key.
func (r *Row) Bool(v bool) *BoolSignal { return Bool(v) }

// Str returns a string signal owned by the row, disposed when the row is
// removed so per-row state never leaks into the next row with the same key.
func (r *Row) Str(v string) *StrSignal { return Str(v) }

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
// row receives a [Row] so per-row UI state (a checked box, an open panel) is
// created fresh with the row and disposed with it. Because entries are keyed,
// changing a field of an existing item does not re-render it — make the parts
// that change signals.
func BindList[T any](
	parent wasmwrap.Element,
	items *ListSignal[T],
	key func(T) string,
	row func(T, *Row) wasmwrap.Element,
) (dispose func()) {
	live := map[string]listEntry{}

	stop := Effect(func() {
		next := items.Get()

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
					d := Scope(func() { el = row(item, &Row{}) })
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
