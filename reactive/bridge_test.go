//go:build js && wasm

package reactive

import (
	"syscall/js"
	"testing"
)

// installStub installs a single x cell on globalThis.aw and returns it, so the
// bridge can be tested against the same surface a real page exposes (get, set,
// onChange, sub) without the x runtime.
func installStub() js.Value {
	const src = `(() => {
		let val = null;
		const subs = new Set();
		const cell = {
			get: () => val,
			set: (nv) => { if (nv !== val) { val = nv; [...subs].forEach(f => f(val)); } },
			onChange: (fn) => { subs.add(fn); return () => subs.delete(fn); },
			sub: (fn) => { const off = cell.onChange(fn); fn(val); return off; },
		};
		globalThis.aw = { state: (n) => cell };
		return cell;
	})();
	`
	return js.Global().Call("eval", src)
}

func TestXNumMirror(t *testing.T) {
	cell := installStub()
	cell.Call("set", 5)

	count := XNum[int]("count")
	if got := count.Get(); got != 5 {
		t.Fatalf("initial Get = %d, want 5", got)
	}

	// Go -> JS
	count.Inc(1)
	if got := cell.Call("get").Int(); got != 6 {
		t.Errorf("cell after Inc = %d, want 6", got)
	}

	// JS -> Go
	cell.Call("set", 10)
	if got := count.Get(); got != 10 {
		t.Errorf("Get after JS set = %d, want 10", got)
	}
}

func TestXBoolToggleMirrors(t *testing.T) {
	cell := installStub()
	cell.Call("set", false)

	open := XBool("open")
	open.Toggle()
	if !cell.Call("get").Bool() {
		t.Error("cell did not flip on Toggle")
	}
}

func TestXStrMirror(t *testing.T) {
	cell := installStub()
	cell.Call("set", "a")

	draft := XStr("draft")
	if got := draft.Get(); got != "a" {
		t.Fatalf("initial Get = %q", got)
	}
	draft.Set("b")
	if got := cell.Call("get").String(); got != "b" {
		t.Errorf("cell after Set = %q, want b", got)
	}
}

func TestXListMirror(t *testing.T) {
	type todo struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	cell := installStub()
	cell.Call("set", js.Global().Get("JSON").Call("parse", `[{"id":"1","text":"buy milk"}]`))

	items := XList[todo]("items")
	if got := items.Get(); len(got) != 1 || got[0].Text != "buy milk" {
		t.Fatalf("initial items = %+v", got)
	}
}

// A missing cell must not panic: the signal degrades to a local zero value.
func TestXNumMissingCell(t *testing.T) {
	js.Global().Set("aw", js.Undefined())
	if got := XNum[int]("nope").Get(); got != 0 {
		t.Errorf("missing cell Get = %d, want 0", got)
	}
}
