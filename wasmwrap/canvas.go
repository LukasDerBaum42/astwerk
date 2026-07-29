//go:build js && wasm

package wasmwrap

import "syscall/js"

// Canvas is a <canvas> element.
type Canvas struct{ e Element }

// AsCanvas reinterprets e as a canvas. It does not check the tag name.
func (e Element) AsCanvas() Canvas { return Canvas{e: e} }

// Element returns the underlying element, for styling or event binding.
func (c Canvas) Element() Element { return c.e }

// Size returns the canvas drawing buffer size in pixels.
func (c Canvas) Size() (w, h int) {
	return c.e.v.Get("width").Int(), c.e.v.Get("height").Int()
}

// SetSize sets the canvas drawing buffer size in pixels. This is the width and
// height attributes, not the CSS size — setting it also clears the canvas.
func (c Canvas) SetSize(w, h int) Canvas {
	c.e.v.Set("width", w)
	c.e.v.Set("height", h)
	return c
}

// FitToDisplay sizes the drawing buffer to the element's CSS size times the
// device pixel ratio, so drawing is crisp on high-DPI screens. It returns the
// logical size to draw against, and scales the context to match.
func (c Canvas) FitToDisplay(ctx Ctx2D) (w, h float64) {
	r := c.e.v.Call("getBoundingClientRect")
	w, h = r.Get("width").Float(), r.Get("height").Float()

	ratio := js.Global().Get("devicePixelRatio").Float()
	if ratio == 0 {
		ratio = 1
	}
	c.SetSize(int(w*ratio), int(h*ratio))
	ctx.v.Call("scale", ratio, ratio)
	return w, h
}

// Context2D returns the 2D drawing context.
func (c Canvas) Context2D() Ctx2D {
	return Ctx2D{v: c.e.v.Call("getContext", "2d")}
}

// Ctx2D is a typed CanvasRenderingContext2D. Drawing calls return the receiver
// so a path can be written as one chain.
type Ctx2D struct{ v js.Value }

// Value returns the underlying js.Value — the escape hatch for the parts of the
// 2D context this type doesn't model.
func (c Ctx2D) Value() js.Value { return c.v }

// --- State ---

func (c Ctx2D) FillStyle(color string) Ctx2D {
	c.v.Set("fillStyle", color)
	return c
}

func (c Ctx2D) StrokeStyle(color string) Ctx2D {
	c.v.Set("strokeStyle", color)
	return c
}

func (c Ctx2D) LineWidth(w float64) Ctx2D {
	c.v.Set("lineWidth", w)
	return c
}

func (c Ctx2D) Font(f string) Ctx2D {
	c.v.Set("font", f)
	return c
}

// Save pushes the drawing state; Restore pops it.
func (c Ctx2D) Save() Ctx2D {
	c.v.Call("save")
	return c
}

func (c Ctx2D) Restore() Ctx2D {
	c.v.Call("restore")
	return c
}

// --- Rectangles ---

func (c Ctx2D) FillRect(x, y, w, h float64) Ctx2D {
	c.v.Call("fillRect", x, y, w, h)
	return c
}

func (c Ctx2D) StrokeRect(x, y, w, h float64) Ctx2D {
	c.v.Call("strokeRect", x, y, w, h)
	return c
}

// Clear erases a rectangle back to transparent.
func (c Ctx2D) Clear(x, y, w, h float64) Ctx2D {
	c.v.Call("clearRect", x, y, w, h)
	return c
}

// --- Paths ---

func (c Ctx2D) BeginPath() Ctx2D {
	c.v.Call("beginPath")
	return c
}

func (c Ctx2D) ClosePath() Ctx2D {
	c.v.Call("closePath")
	return c
}

func (c Ctx2D) MoveTo(x, y float64) Ctx2D {
	c.v.Call("moveTo", x, y)
	return c
}

func (c Ctx2D) LineTo(x, y float64) Ctx2D {
	c.v.Call("lineTo", x, y)
	return c
}

// Arc adds an arc. Angles are in radians, clockwise from the positive x axis.
func (c Ctx2D) Arc(x, y, radius, startAngle, endAngle float64) Ctx2D {
	c.v.Call("arc", x, y, radius, startAngle, endAngle)
	return c
}

func (c Ctx2D) Fill() Ctx2D {
	c.v.Call("fill")
	return c
}

func (c Ctx2D) Stroke() Ctx2D {
	c.v.Call("stroke")
	return c
}

// --- Text ---

func (c Ctx2D) FillText(text string, x, y float64) Ctx2D {
	c.v.Call("fillText", text, x, y)
	return c
}

// MeasureText returns the rendered width of text in the current font.
func (c Ctx2D) MeasureText(text string) float64 {
	return c.v.Call("measureText", text).Get("width").Float()
}

// --- Animation ---

// RequestAnimationFrame drives an animation loop, calling fn once per frame
// until cancel is called. dt is the time since the previous frame in seconds,
// and is 0 on the first frame.
//
// Note this is a loop, not a single JS requestAnimationFrame call: it
// reschedules itself, which is what makes the returned cancel meaningful and
// what a canvas animation actually wants.
func (c Ctx2D) RequestAnimationFrame(fn func(dt float64)) (cancel func()) {
	var (
		cb        js.Func
		id        js.Value
		last      float64
		cancelled bool
	)

	cb = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if cancelled {
			return nil
		}
		now := 0.0
		if len(args) > 0 {
			now = args[0].Float()
		}
		dt := 0.0
		if last != 0 {
			dt = (now - last) / 1000
		}
		last = now

		fn(dt)

		if !cancelled {
			id = js.Global().Call("requestAnimationFrame", cb)
		}
		return nil
	})

	id = js.Global().Call("requestAnimationFrame", cb)

	return func() {
		if cancelled {
			return
		}
		cancelled = true
		js.Global().Call("cancelAnimationFrame", id)
		cb.Release()
	}
}
