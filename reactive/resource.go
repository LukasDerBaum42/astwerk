//go:build js && wasm

package reactive

import (
	"encoding/json"

	"github.com/LukasDerBaum42/astwerk/wasmwrap"
)

// Resource is an async value with its loading and error states exposed as
// signals, so a view can bind to all three without tracking them by hand.
//
//	posts := reactive.NewResource(func() ([]Post, error) {
//		return reactive.FetchJSON[[]Post]("/api/posts")
//	})
//
//	reactive.BindShow(spinner, posts.Loading.Get)
//	reactive.BindText(errBox, func() string {
//		if err := posts.Err.Get(); err != nil {
//			return err.Error()
//		}
//		return ""
//	})
//	reactive.BindList(list, posts.Data.Get, func(p Post) string { return p.ID }, renderPost)
type Resource[T any] struct {
	// Data is the loaded value, or T's zero value before the first success.
	Data *Signal[T]
	// Err is the last error, cleared when a load succeeds.
	Err *Signal[error]
	// Loading is true while a load is in flight.
	Loading *Signal[bool]

	load func() (T, error)
	gen  int // generation counter, so a stale reload can't overwrite a newer one
}

// NewResource starts loading immediately and returns the resource.
//
// load runs on its own goroutine, so a blocking call like [wasmwrap.Fetch] is
// fine here — that is the intended use.
func NewResource[T any](load func() (T, error)) *Resource[T] {
	var zero T
	r := &Resource[T]{
		Data:    NewSignal(zero),
		Err:     NewSignal[error](nil),
		Loading: NewSignal(false),
		load:    load,
	}
	r.Reload()
	return r
}

// Reload runs the loader again. An in-flight load is not cancelled, but its
// result is discarded if it lands after a newer one.
func (r *Resource[T]) Reload() {
	r.gen++
	gen := r.gen
	r.Loading.Set(true)

	go func() {
		v, err := r.load()

		// Go on js/wasm runs one goroutine at a time, so this needs no lock —
		// but it does need the generation check, or a slow first request could
		// land after a fast second one and show stale data.
		if gen != r.gen {
			return
		}

		Batch(func() {
			if err != nil {
				r.Err.Set(err)
			} else {
				r.Err.Set(nil)
				r.Data.Set(v)
			}
			r.Loading.Set(false)
		})
	}()
}

// FetchJSON GETs url and decodes the response into T.
//
// It blocks, like [wasmwrap.Fetch] — call it from a goroutine or from a
// [Resource] loader, not directly inside an event handler.
func FetchJSON[T any](url string) (T, error) {
	var out T
	body, err := wasmwrap.Fetch(url)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	return out, nil
}
