//go:build js && wasm

// Command demo powers the live examples on the Examples page.
//
// It hydrates markup the site already generated rather than building any DOM
// from scratch, which is the pattern the docs recommend: one source of markup,
// surgical updates, and a page that still reads with WebAssembly disabled.
package main

import (
	"math"
	"slices"
	"strconv"

	"github.com/LukasDerBaum42/astwerk/reactive"
	"github.com/LukasDerBaum42/astwerk/wasmwrap"
)

func main() {
	counter()
	todoList()
	canvasDemo()

	// Keep the module alive: main returning would tear down every handler.
	select {}
}

func counter() {
	count := reactive.NewSignal(0)
	parity := reactive.Computed(func() string {
		if count.Get()%2 == 0 {
			return "even"
		}
		return "odd"
	})

	reactive.Hydrate("#counter", func(root wasmwrap.Element) {
		reactive.BindText(root.Find("[data-count]"), func() string {
			return strconv.Itoa(count.Get())
		})
		reactive.BindText(root.Find("[data-parity]"), parity.Get)

		reactive.On(root.Find("[data-inc]"), "click", func(wasmwrap.Event) {
			count.Update(func(v int) int { return v + 1 })
		})
		reactive.On(root.Find("[data-dec]"), "click", func(wasmwrap.Event) {
			count.Update(func(v int) int { return v - 1 })
		})
	})
}

type item struct {
	ID   string
	Text string
}

func todoList() {
	items := reactive.NewSignal([]item{
		{ID: "1", Text: "Read the docs"},
		{ID: "2", Text: "Build something"},
	})
	nextID := 2

	reactive.Hydrate("#todo", func(root wasmwrap.Element) {
		field := root.Find("[data-new]")
		draft := reactive.NewSignal("")
		reactive.BindValue(field, draft)

		add := func() {
			if draft.Get() == "" {
				return
			}
			nextID++
			id := strconv.Itoa(nextID)
			text := draft.Get()
			reactive.Batch(func() {
				items.Update(func(all []item) []item {
					return append(all, item{ID: id, Text: text})
				})
				draft.Set("")
			})
		}

		// Submit rather than click, so the Enter key works too.
		reactive.On(root.Find("[data-form]"), "submit", func(e wasmwrap.Event) {
			e.PreventDefault()
			add()
		})

		reactive.BindText(root.Find("[data-remaining]"), func() string {
			return strconv.Itoa(len(items.Get()))
		})

		reactive.BindList(root.Find("[data-list]"), items.Get,
			func(it item) string { return it.ID },
			func(it item) wasmwrap.Element {
				row := wasmwrap.Create("li")
				text := wasmwrap.Create("input").SetInputValue(it.Text)
				remove := wasmwrap.Create("button").SetText("remove")
				remove.Class().Add("remove")

				reactive.On(remove, "click", func(wasmwrap.Event) {
					items.Update(func(all []item) []item {
						return slices.DeleteFunc(slices.Clone(all), func(o item) bool {
							return o.ID == it.ID
						})
					})
				})
				return row.Append(text, remove)
			})
	})
}

func canvasDemo() {
	reactive.Hydrate("#canvas-demo", func(root wasmwrap.Element) {
		canvas := root.Find("[data-canvas]").AsCanvas()
		ctx2d := canvas.Context2D()
		w, h := canvas.FitToDisplay(ctx2d)

		pointer := reactive.NewSignal(w / 2)
		reactive.On(canvas.Element(), "pointermove", func(e wasmwrap.Event) {
			x, _ := e.PosIn(canvas.Element())
			pointer.Set(float64(x))
		})

		elapsed := 0.0
		draw := func(dt float64) {
			elapsed += dt
			ctx2d.Clear(0, 0, w, h)
			for i := 0.0; i < w; i += 6 {
				wave := math.Sin(i/40+elapsed*2) * 40
				// Peek, not Get: this runs outside an effect, and a dependency
				// here would be meaningless — the loop already redraws every frame.
				lift := math.Exp(-math.Pow(i-pointer.Peek(), 2)/4000) * 30
				ctx2d.FillStyle("#4f7cff").FillRect(i, h/2+wave-lift, 3, 3)
			}
		}

		cancel := ctx2d.RequestAnimationFrame(draw)
		running := true

		toggle := root.Find("[data-toggle]")
		reactive.On(toggle, "click", func(wasmwrap.Event) {
			if running {
				cancel()
				toggle.SetText("Resume")
			} else {
				cancel = ctx2d.RequestAnimationFrame(draw)
				toggle.SetText("Pause")
			}
			running = !running
		})
	})
}
