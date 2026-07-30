//go:build js && wasm

package jsdom

import (
	"syscall/js"
	"testing"
)

func init() { Install() }

func TestInstallIsIdempotent(t *testing.T) {
	Install()
	Install()
	if !js.Global().Get("document").Truthy() {
		t.Fatal("document is missing after repeated Install")
	}
}

func TestResetClearsTheBody(t *testing.T) {
	Reset()
	body := js.Global().Get("document").Get("body")
	body.Call("appendChild", NewElement("p"))
	if got := body.Get("children").Length(); got != 1 {
		t.Fatalf("body has %d children before Reset, want 1", got)
	}

	Reset()
	body = js.Global().Get("document").Get("body")
	if got := body.Get("children").Length(); got != 0 {
		t.Errorf("body has %d children after Reset, want 0", got)
	}
}

func TestElementTreeManipulation(t *testing.T) {
	Reset()
	parent := NewElement("div")
	a, b, c := NewElement("p"), NewElement("p"), NewElement("p")
	a.Set("textContent", "a")
	b.Set("textContent", "b")
	c.Set("textContent", "c")

	parent.Call("appendChild", a)
	parent.Call("appendChild", c)
	parent.Call("insertBefore", b, c)

	if got := parent.Get("_tree").String(); got != "a,b,c" {
		t.Errorf("_tree = %q, want \"a,b,c\"", got)
	}

	b.Call("remove")
	if got := parent.Get("_tree").String(); got != "a,c" {
		t.Errorf("_tree after remove = %q, want \"a,c\"", got)
	}
	if b.Get("parentElement").Truthy() {
		t.Error("removed element still has a parentElement")
	}
}

func TestAttributesClassesAndSelectors(t *testing.T) {
	Reset()
	body := js.Global().Get("document").Get("body")
	el := NewElement("span")
	el.Call("setAttribute", "id", "box")
	el.Call("setAttribute", "data-x", "1")
	el.Get("classList").Call("add", "on")
	body.Call("appendChild", el)

	doc := js.Global().Get("document")
	for _, sel := range []string{"#box", ".on", "[data-x]", "span"} {
		if !doc.Call("querySelector", sel).Truthy() {
			t.Errorf("querySelector(%q) found nothing", sel)
		}
	}
	if got := doc.Call("querySelectorAll", "span").Length(); got != 1 {
		t.Errorf("querySelectorAll(\"span\") returned %d, want 1", got)
	}
	if got := el.Call("closest", "#box"); !got.Truthy() {
		t.Error("closest did not match the element itself")
	}
	if got := el.Call("getAttribute", "data-x").String(); got != "1" {
		t.Errorf("getAttribute(data-x) = %q, want \"1\"", got)
	}
	el.Call("removeAttribute", "data-x")
	if el.Call("getAttribute", "data-x").Truthy() {
		t.Error("removeAttribute left the attribute in place")
	}
}

func TestEventsAndListenerCount(t *testing.T) {
	Reset()
	el := NewElement("button")

	fired := 0
	fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		fired++
		if got := args[0].Get("key").String(); got != "Enter" {
			t.Errorf("event key = %q, want \"Enter\"", got)
		}
		args[0].Call("preventDefault")
		return nil
	})
	defer fn.Release()

	el.Call("addEventListener", "keydown", fn)
	if got := el.Call("listenerCount", "keydown").Int(); got != 1 {
		t.Fatalf("listenerCount = %d, want 1", got)
	}

	ev := el.Call("dispatch", "keydown", map[string]any{"key": "Enter"})
	if fired != 1 {
		t.Errorf("handler fired %d times, want 1", fired)
	}
	if !ev.Get("defaultPrevented").Bool() {
		t.Error("preventDefault did not record on the event")
	}

	el.Call("removeEventListener", "keydown", fn)
	if got := el.Call("listenerCount", "keydown").Int(); got != 0 {
		t.Errorf("listenerCount after removal = %d, want 0", got)
	}
}

func TestCanvasContextRecordsCalls(t *testing.T) {
	Reset()
	canvas := NewElement("canvas")
	ctx := canvas.Call("getContext", "2d")
	ctx.Set("fillStyle", "red")
	ctx.Call("fillRect", 1, 2, 3, 4)
	ctx.Call("beginPath")

	calls := canvas.Get("_ctxCalls")
	if got := calls.Length(); got != 2 {
		t.Fatalf("_ctxCalls has %d entries, want 2", got)
	}
	if got := calls.Index(0).String(); got != "fillRect(1,2,3,4)" {
		t.Errorf("_ctxCalls[0] = %q", got)
	}
	if got := ctx.Get("fillStyle").String(); got != "red" {
		t.Errorf("fillStyle = %q, want \"red\"", got)
	}
	// The same context comes back, so a second getContext keeps the recording.
	canvas.Call("getContext", "2d").Call("stroke")
	if got := canvas.Get("_ctxCalls").Length(); got != 3 {
		t.Errorf("_ctxCalls has %d entries after a second getContext, want 3", got)
	}
}

func TestHistoryDrivesLocation(t *testing.T) {
	js.Global().Get("history").Call("pushState", js.Null(), "", "/projects/alpha")
	if got := js.Global().Get("location").Get("pathname").String(); got != "/projects/alpha" {
		t.Errorf("pathname = %q after pushState", got)
	}
	js.Global().Get("history").Call("replaceState", js.Null(), "", "/")
	if got := js.Global().Get("location").Get("pathname").String(); got != "/" {
		t.Errorf("pathname = %q after replaceState", got)
	}
}

func TestCloneNode(t *testing.T) {
	Reset()
	el := NewElement("div")
	el.Call("setAttribute", "id", "orig")
	el.Get("classList").Call("add", "on")
	child := NewElement("span")
	child.Set("textContent", "hi")
	el.Call("appendChild", child)

	shallow := el.Call("cloneNode", false)
	if got := shallow.Get("children").Length(); got != 0 {
		t.Errorf("shallow clone has %d children, want 0", got)
	}
	if got := shallow.Call("getAttribute", "id").String(); got != "orig" {
		t.Errorf("shallow clone lost attributes: id = %q", got)
	}

	deep := el.Call("cloneNode", true)
	if got := deep.Get("_tree").String(); got != "hi" {
		t.Errorf("deep clone _tree = %q, want \"hi\"", got)
	}
	if !deep.Get("classList").Call("contains", "on").Bool() {
		t.Error("deep clone lost classes")
	}
}
