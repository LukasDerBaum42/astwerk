//go:build js && wasm

package wasmwrap

import (
	"strings"
	"syscall/js"
	"testing"
	"time"
)

func TestQueryAndCreate(t *testing.T) {
	resetDOM(t)

	box := mount("div").SetAttr("id", "box").SetText("hello")
	mount("p").Class().Add("note")
	mount("p").Class().Add("note")

	if got := Query("#box"); !got.Exists() || got.Text() != "hello" {
		t.Errorf("Query(#box) = exists:%v text:%q", got.Exists(), got.Text())
	}
	if got := QueryAll(".note"); len(got) != 2 {
		t.Errorf("QueryAll(.note) returned %d elements, want 2", len(got))
	}
	if got := QueryAll(".missing"); len(got) != 0 {
		t.Errorf("QueryAll on no matches returned %d, want 0", len(got))
	}
	if Create("span").Value().Get("tagName").String() != "SPAN" {
		t.Errorf("Create did not make a span")
	}
	_ = box
}

// A failed Query must be inert rather than panicking, so callers can check it.
func TestQueryMissingIsInert(t *testing.T) {
	resetDOM(t)

	missing := Query("#nope")
	if missing.Exists() {
		t.Fatal("Query on no match reported Exists() == true")
	}
	if got := missing.Text(); got != "" {
		t.Errorf("Text() on a missing element = %q, want \"\"", got)
	}
	if got := missing.HTML(); got != "" {
		t.Errorf("HTML() on a missing element = %q, want \"\"", got)
	}
	missing.Remove() // must not panic
}

func TestTreeManipulation(t *testing.T) {
	resetDOM(t)

	parent := mount("div")
	a, b := Create("span").SetText("a"), Create("span").SetText("b")

	if got := parent.Append(a, b); !got.Value().Equal(parent.Value()) {
		t.Error("Append did not return its receiver")
	}
	if n := len(parent.Children()); n != 2 {
		t.Fatalf("parent has %d children, want 2", n)
	}

	a.Remove()
	if n := len(parent.Children()); n != 1 {
		t.Errorf("after Remove parent has %d children, want 1", n)
	}
	if !b.Parent().Value().Equal(parent.Value()) {
		t.Error("Parent() did not return the parent")
	}

	clone := b.Clone()
	if clone.Text() != "b" {
		t.Errorf("Clone lost the text: %q", clone.Text())
	}
	if clone.Value().Equal(b.Value()) {
		t.Error("Clone returned the same node")
	}

	parent.Append(Create("em").SetAttr("id", "deep"))
	if !parent.Find("#deep").Exists() {
		t.Error("Find did not locate a descendant")
	}
}

func TestContentAndAttributes(t *testing.T) {
	resetDOM(t)
	el := mount("div")

	el.SetText("plain")
	if el.Text() != "plain" {
		t.Errorf("Text = %q", el.Text())
	}

	el.SetHTML("<b>bold</b>")
	if el.HTML() != "<b>bold</b>" {
		t.Errorf("HTML = %q", el.HTML())
	}

	if got := el.Attr("data-x"); got != "" {
		t.Errorf("Attr on an absent attribute = %q, want \"\"", got)
	}
	el.SetAttr("data-x", "1")
	if got := el.Attr("data-x"); got != "1" {
		t.Errorf("Attr = %q, want 1", got)
	}
	el.RemoveAttr("data-x")
	if got := el.Attr("data-x"); got != "" {
		t.Errorf("Attr after RemoveAttr = %q", got)
	}

	input := mount("input")
	input.SetInputValue("typed")
	if input.InputValue() != "typed" {
		t.Errorf("InputValue = %q", input.InputValue())
	}
}

func TestClassList(t *testing.T) {
	resetDOM(t)
	c := mount("div").Class()

	c.Add("a", "b")
	if !c.Has("a") || !c.Has("b") {
		t.Error("Add did not add both names")
	}
	c.Remove("a")
	if c.Has("a") {
		t.Error("Remove did not remove")
	}
	if on := c.Toggle("new"); !on || !c.Has("new") {
		t.Errorf("Toggle on = %v, has = %v", on, c.Has("new"))
	}
	if on := c.Toggle("new"); on || c.Has("new") {
		t.Errorf("Toggle off = %v, has = %v", on, c.Has("new"))
	}
}

func TestStyleChaining(t *testing.T) {
	resetDOM(t)
	el := mount("div")

	el.Style().Color("red").Display("block").Background("#fff").
		Size("10px", "20px").Position("1px", "2px").Set("border-radius", "4px")

	s := el.Style()
	for prop, want := range map[string]string{
		"color":         "red",
		"display":       "block",
		"background":    "#fff",
		"width":         "10px",
		"height":        "20px",
		"top":           "1px",
		"left":          "2px",
		"border-radius": "4px",
	} {
		if got := s.Get(prop); got != want {
			t.Errorf("style %s = %q, want %q", prop, got, want)
		}
	}
}

// An empty string must leave a dimension alone rather than clearing it.
func TestStyleSizeSkipsEmpty(t *testing.T) {
	resetDOM(t)
	s := mount("div").Style().Size("5px", "5px").Size("9px", "")

	if got := s.Get("width"); got != "9px" {
		t.Errorf("width = %q, want 9px", got)
	}
	if got := s.Get("height"); got != "5px" {
		t.Errorf("height = %q, want the untouched 5px", got)
	}
}

func TestStyleHideShow(t *testing.T) {
	resetDOM(t)
	s := mount("div").Style().Hide()
	if got := s.Get("display"); got != "none" {
		t.Errorf("after Hide display = %q", got)
	}
	if got := s.Show().Get("display"); got != "" {
		t.Errorf("after Show display = %q, want \"\"", got)
	}
}

func TestEventsAndUnsubscribe(t *testing.T) {
	resetDOM(t)
	el := mount("button")

	clicks := 0
	off := el.On("click", func(Event) { clicks++ })

	el.Value().Call("dispatch", "click", js.Null())
	el.Value().Call("dispatch", "click", js.Null())
	if clicks != 2 {
		t.Fatalf("handler ran %d times, want 2", clicks)
	}

	off()
	el.Value().Call("dispatch", "click", js.Null())
	if clicks != 2 {
		t.Errorf("handler ran after unsubscribe: %d", clicks)
	}
	if n := el.Value().Call("listenerCount", "click").Int(); n != 0 {
		t.Errorf("%d listeners still bound after unsubscribe", n)
	}
	off() // must be idempotent, not a double Release
}

func TestEventAccessors(t *testing.T) {
	resetDOM(t)
	el := mount("div")

	var got Event
	el.On("keydown", func(e Event) { got = e })

	props := js.Global().Get("Object").New()
	props.Set("key", "Enter")
	props.Set("clientX", 30)
	props.Set("clientY", 40)
	ev := el.Value().Call("dispatch", "keydown", props)

	if got.Type() != "keydown" {
		t.Errorf("Type = %q", got.Type())
	}
	if got.Key() != "Enter" {
		t.Errorf("Key = %q", got.Key())
	}
	if x, y := got.Pos(); x != 30 || y != 40 {
		t.Errorf("Pos = %d,%d want 30,40", x, y)
	}
	if !got.Target().Value().Equal(el.Value()) {
		t.Error("Target did not return the dispatching element")
	}

	got.PreventDefault()
	got.StopPropagation()
	if !ev.Get("defaultPrevented").Bool() {
		t.Error("PreventDefault had no effect")
	}
	if !ev.Get("propagationStopped").Bool() {
		t.Error("StopPropagation had no effect")
	}
}

// Key must not panic on an event that has no key property.
func TestEventKeyAbsent(t *testing.T) {
	resetDOM(t)
	el := mount("div")

	var got Event
	el.On("click", func(e Event) { got = e })
	el.Value().Call("dispatch", "click", js.Null())

	if k := got.Key(); k != "" {
		t.Errorf("Key on a click = %q, want \"\"", k)
	}
}

func TestEventPosIn(t *testing.T) {
	resetDOM(t)
	el := mount("canvas")
	rect := js.Global().Get("Object").New()
	rect.Set("left", 100)
	rect.Set("top", 50)
	el.Value().Set("_rect", rect)

	var got Event
	el.On("click", func(e Event) { got = e })

	props := js.Global().Get("Object").New()
	props.Set("clientX", 130)
	props.Set("clientY", 70)
	el.Value().Call("dispatch", "click", props)

	if x, y := got.PosIn(el); x != 30 || y != 20 {
		t.Errorf("PosIn = %d,%d want 30,20", x, y)
	}
}

func TestOnce(t *testing.T) {
	resetDOM(t)
	el := mount("div")

	runs := 0
	el.Once("click", func(Event) { runs++ })

	el.Value().Call("dispatch", "click", js.Null())
	el.Value().Call("dispatch", "click", js.Null())

	if runs != 1 {
		t.Errorf("Once handler ran %d times, want 1", runs)
	}
	if n := el.Value().Call("listenerCount", "click").Int(); n != 0 {
		t.Errorf("Once left %d listeners bound", n)
	}
}

func TestSetTimeout(t *testing.T) {
	done := make(chan struct{})
	SetTimeout(func() { close(done) }, 1)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SetTimeout never fired")
	}
}

func TestSetTimeoutCancel(t *testing.T) {
	fired := make(chan struct{}, 1)
	cancel := SetTimeout(func() { fired <- struct{}{} }, 20)
	cancel()
	cancel() // idempotent

	select {
	case <-fired:
		t.Fatal("cancelled timeout still fired")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestSetInterval(t *testing.T) {
	ticks := make(chan struct{}, 8)
	cancel := SetInterval(func() {
		select {
		case ticks <- struct{}{}:
		default:
		}
	}, 1)
	defer cancel()

	for i := range 3 {
		select {
		case <-ticks:
		case <-time.After(2 * time.Second):
			t.Fatalf("interval stopped after %d ticks", i)
		}
	}
}

func TestFetch(t *testing.T) {
	stubFetch(t, "hello wasm", true, 200, false)

	got, err := Fetch("/thing")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello wasm" {
		t.Errorf("Fetch body = %q", got)
	}
}

func TestFetchStatusError(t *testing.T) {
	stubFetch(t, "", false, 404, false)

	_, err := Fetch("/missing")
	if err == nil {
		t.Fatal("expected an error for a 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error does not mention the status: %v", err)
	}
}

func TestFetchRejection(t *testing.T) {
	stubFetch(t, "", true, 200, true)

	_, err := Fetch("/boom")
	if err == nil {
		t.Fatal("expected an error for a rejected promise")
	}
	if !strings.Contains(err.Error(), "network down") {
		t.Errorf("error lost the rejection message: %v", err)
	}
}

// Fetch from a goroutine while main is blocked must still resolve.
func TestFetchFromGoroutine(t *testing.T) {
	stubFetch(t, "async", true, 200, false)

	type res struct {
		body string
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		b, err := Fetch("/thing")
		ch <- res{string(b), err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.body != "async" {
			t.Errorf("body = %q", r.body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Fetch in a goroutine deadlocked")
	}
}

func TestCanvasContext2D(t *testing.T) {
	resetDOM(t)
	canvas := mount("canvas").AsCanvas()

	canvas.SetSize(300, 150)
	if w, h := canvas.Size(); w != 300 || h != 150 {
		t.Errorf("Size = %d,%d want 300,150", w, h)
	}

	ctx := canvas.Context2D()
	ctx.FillStyle("red").StrokeStyle("blue").LineWidth(2).Font("12px sans")

	if got := ctx.Value().Get("fillStyle").String(); got != "red" {
		t.Errorf("fillStyle = %q", got)
	}
	if got := ctx.Value().Get("strokeStyle").String(); got != "blue" {
		t.Errorf("strokeStyle = %q", got)
	}

	ctx.Clear(0, 0, 300, 150).
		FillRect(1, 2, 3, 4).
		StrokeRect(5, 6, 7, 8).
		BeginPath().MoveTo(0, 0).LineTo(10, 10).
		Arc(1, 2, 3, 0, 6).
		ClosePath().Fill().Stroke().
		FillText("hi", 4, 5)

	want := []string{
		"clearRect(0,0,300,150)",
		"fillRect(1,2,3,4)",
		"strokeRect(5,6,7,8)",
		"beginPath()",
		"moveTo(0,0)",
		"lineTo(10,10)",
		"arc(1,2,3,0,6)",
		"closePath()",
		"fill()",
		"stroke()",
		"fillText(hi,4,5)",
	}
	calls := canvas.Element().Value().Get("_ctxCalls")
	if n := calls.Get("length").Int(); n != len(want) {
		t.Fatalf("recorded %d calls, want %d", n, len(want))
	}
	for i, w := range want {
		if got := calls.Index(i).String(); got != w {
			t.Errorf("call %d = %q, want %q", i, got, w)
		}
	}

	if got := ctx.MeasureText("abc"); got != 21 {
		t.Errorf("MeasureText = %v, want 21", got)
	}
}

func TestCanvasFitToDisplay(t *testing.T) {
	resetDOM(t)
	canvas := mount("canvas").AsCanvas()

	rect := js.Global().Get("Object").New()
	rect.Set("left", 0)
	rect.Set("top", 0)
	rect.Set("width", 200)
	rect.Set("height", 100)
	canvas.Element().Value().Set("_rect", rect)

	ctx := canvas.Context2D()
	w, h := canvas.FitToDisplay(ctx) // devicePixelRatio is 2 in the stub

	if w != 200 || h != 100 {
		t.Errorf("logical size = %v,%v want 200,100", w, h)
	}
	if bw, bh := canvas.Size(); bw != 400 || bh != 200 {
		t.Errorf("buffer size = %d,%d want 400,200", bw, bh)
	}
	calls := canvas.Element().Value().Get("_ctxCalls")
	if got := calls.Index(calls.Get("length").Int() - 1).String(); got != "scale(2,2)" {
		t.Errorf("last call = %q, want scale(2,2)", got)
	}
}

func TestRequestAnimationFrameLoopsAndCancels(t *testing.T) {
	resetDOM(t)
	ctx := mount("canvas").AsCanvas().Context2D()

	frames := make(chan float64, 16)
	cancel := ctx.RequestAnimationFrame(func(dt float64) {
		select {
		case frames <- dt:
		default:
		}
	})

	// It reschedules itself, so several frames arrive from one call.
	var first float64
	for i := range 3 {
		select {
		case dt := <-frames:
			if i == 0 {
				first = dt
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("only got %d frames", i)
		}
	}
	if first != 0 {
		t.Errorf("first frame dt = %v, want 0", first)
	}

	cancel()
	cancel() // idempotent

	// Drain anything already queued, then confirm it has stopped.
	time.Sleep(100 * time.Millisecond)
	for len(frames) > 0 {
		<-frames
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(frames); n != 0 {
		t.Errorf("got %d frames after cancel", n)
	}
}

func TestComputedStyle(t *testing.T) {
	resetDOM(t)
	el := mount("div")
	el.Style().Color("rebeccapurple")

	if got := el.Computed("color"); got != "rebeccapurple" {
		t.Errorf("Computed(color) = %q", got)
	}
}

func TestWrapAndValueRoundTrip(t *testing.T) {
	resetDOM(t)
	el := mount("div").SetText("x")

	if Wrap(el.Value()).Text() != "x" {
		t.Error("Wrap(Value()) did not round-trip")
	}
}
