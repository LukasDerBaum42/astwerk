//go:build js && wasm

package reactive

import (
	"context"
	"io"
	"strconv"
	"strings"
	"syscall/js"
	"testing"
	"time"

	"github.com/LukasDerBaum42/astwerk/jsdom"
	"github.com/LukasDerBaum42/astwerk/wasmwrap"
	"github.com/a-h/templ"
)

// node has no DOM, so these tests run against jsdom, the same stub the
// wasmwrap tests use. Assertions check that a binding pokes the right property
// at the right time.
func init() { jsdom.Install() }

func resetDOM(t *testing.T) {
	t.Helper()
	jsdom.Reset()
}

func el(tag string) wasmwrap.Element {
	return wasmwrap.Wrap(jsdom.NewElement(tag))
}

func mount(tag string) wasmwrap.Element {
	e := el(tag)
	wasmwrap.Body().Append(e)
	return e
}

// tree is the comma-joined text of parent's subtree, for asserting list order.
func tree(parent wasmwrap.Element) string {
	return parent.Value().Get("_tree").String()
}

func TestBindText(t *testing.T) {
	resetDOM(t)
	e := mount("span")
	s := NewSignal("one")

	defer BindText(e, s.Get)()
	if e.Text() != "one" {
		t.Errorf("text = %q", e.Text())
	}
	s.Set("two")
	if e.Text() != "two" {
		t.Errorf("text = %q after Set", e.Text())
	}
}

func TestBindAttrRemovesOnEmpty(t *testing.T) {
	resetDOM(t)
	e := mount("input")
	disabled := NewSignal(true)

	defer BindAttr(e, "disabled", func() string {
		if disabled.Get() {
			return "disabled"
		}
		return ""
	})()

	if e.Attr("disabled") != "disabled" {
		t.Errorf("attr = %q", e.Attr("disabled"))
	}
	disabled.Set(false)
	if e.Attr("disabled") != "" {
		t.Errorf("attr = %q, want it removed", e.Attr("disabled"))
	}
}

func TestBindClassAndStyleAndShow(t *testing.T) {
	resetDOM(t)
	e := mount("div")
	active := NewSignal(false)
	color := NewSignal("red")

	defer BindClass(e, "active", active.Get)()
	defer BindStyle(e, "color", color.Get)()
	defer BindShow(e, active.Get)()

	if e.Class().Has("active") {
		t.Error("class applied while false")
	}
	if got := e.Style().Get("display"); got != "none" {
		t.Errorf("display = %q, want none", got)
	}

	active.Set(true)
	color.Set("blue")

	if !e.Class().Has("active") {
		t.Error("class not applied")
	}
	if got := e.Style().Get("color"); got != "blue" {
		t.Errorf("color = %q", got)
	}
	if got := e.Style().Get("display"); got != "" {
		t.Errorf("display = %q, want cleared", got)
	}
}

func TestBindValueTwoWay(t *testing.T) {
	resetDOM(t)
	input := mount("input")
	s := NewSignal("start")

	defer BindValue(input, s)()
	if input.InputValue() != "start" {
		t.Errorf("value = %q", input.InputValue())
	}

	// Signal -> field
	s.Set("fromSignal")
	if input.InputValue() != "fromSignal" {
		t.Errorf("value = %q after Set", input.InputValue())
	}

	// Field -> signal
	input.Value().Set("value", "typed")
	input.Value().Call("dispatch", "input", js.Null())
	if s.Get() != "typed" {
		t.Errorf("signal = %q after input", s.Get())
	}

	// Exactly one listener: the echo-back effect must not stack them up.
	if n := input.Value().Call("listenerCount", "input").Int(); n != 1 {
		t.Errorf("%d input listeners bound, want 1", n)
	}
}

func TestBindChecked(t *testing.T) {
	resetDOM(t)
	box := mount("input")
	s := NewSignal(false)

	defer BindChecked(box, s)()
	if box.Value().Get("checked").Bool() {
		t.Error("checked while signal is false")
	}

	s.Set(true)
	if !box.Value().Get("checked").Bool() {
		t.Error("not checked after Set(true)")
	}

	box.Value().Set("checked", false)
	box.Value().Call("dispatch", "change", js.Null())
	if s.Get() {
		t.Error("signal not updated from the checkbox")
	}
}

func TestBindNumberIgnoresGarbage(t *testing.T) {
	resetDOM(t)
	input := mount("input")
	s := NewSignal(1.5)

	defer BindNumber(input, s)()
	if got := input.InputValue(); got != "1.5" {
		t.Errorf("value = %q", got)
	}

	input.Value().Set("value", "abc")
	input.Value().Call("dispatch", "input", js.Null())
	if s.Get() != 1.5 {
		t.Errorf("signal = %v, want unchanged 1.5", s.Get())
	}

	input.Value().Set("value", "2.25")
	input.Value().Call("dispatch", "input", js.Null())
	if s.Get() != 2.25 {
		t.Errorf("signal = %v, want 2.25", s.Get())
	}
}

// On must unbind when the enclosing effect re-runs, or handlers pile up.
func TestOnCleansUpWithEffect(t *testing.T) {
	resetDOM(t)
	btn := mount("button")
	trigger := NewSignal(0)
	clicks := 0

	dispose := Effect(func() {
		trigger.Get()
		On(btn, "click", func(wasmwrap.Event) { clicks++ })
	})

	trigger.Set(1)
	trigger.Set(2)

	if n := btn.Value().Call("listenerCount", "click").Int(); n != 1 {
		t.Fatalf("%d click listeners after 3 runs, want 1", n)
	}
	btn.Value().Call("dispatch", "click", js.Null())
	if clicks != 1 {
		t.Errorf("handler ran %d times for one click", clicks)
	}

	dispose()
	if n := btn.Value().Call("listenerCount", "click").Int(); n != 0 {
		t.Errorf("%d listeners after dispose", n)
	}
}

func TestBindWhen(t *testing.T) {
	resetDOM(t)
	parent := mount("div")
	show := NewSignal(false)
	builds := 0

	defer BindWhen(parent, show.Get, func() wasmwrap.Element {
		builds++
		return el("p").SetText("here")
	})()

	if len(parent.Children()) != 0 {
		t.Fatal("subtree present while false")
	}

	show.Set(true)
	if len(parent.Children()) != 1 || builds != 1 {
		t.Fatalf("children = %d builds = %d", len(parent.Children()), builds)
	}

	show.Set(false)
	if len(parent.Children()) != 0 {
		t.Errorf("subtree not removed: %d children", len(parent.Children()))
	}

	show.Set(true)
	if builds != 2 {
		t.Errorf("builds = %d, want a fresh build on re-entry", builds)
	}
}

type row struct {
	id   string
	text string
}

func TestBindListAddRemoveReorder(t *testing.T) {
	resetDOM(t)
	parent := mount("ul")
	items := NewSignal([]row{{"a", "A"}, {"b", "B"}})

	renders := map[string]int{}
	defer BindList(parent, items.Get,
		func(r row) string { return r.id },
		func(r row) wasmwrap.Element {
			renders[r.id]++
			return el("li").SetText(r.text)
		})()

	if got := tree(parent); got != "A,B" {
		t.Fatalf("tree = %q, want A,B", got)
	}

	// Append
	items.Set([]row{{"a", "A"}, {"b", "B"}, {"c", "C"}})
	if got := tree(parent); got != "A,B,C" {
		t.Fatalf("tree = %q, want A,B,C", got)
	}

	// Reorder — must move nodes, not re-render them
	items.Set([]row{{"c", "C"}, {"a", "A"}, {"b", "B"}})
	if got := tree(parent); got != "C,A,B" {
		t.Fatalf("tree = %q, want C,A,B", got)
	}
	for id, n := range renders {
		if n != 1 {
			t.Errorf("item %s rendered %d times, want 1 — reorder rebuilt it", id, n)
		}
	}

	// Remove from the middle
	items.Set([]row{{"c", "C"}, {"b", "B"}})
	if got := tree(parent); got != "C,B" {
		t.Errorf("tree = %q, want C,B", got)
	}
}

// An item's own signals must keep patching after the list effect re-runs.
func TestBindListItemBindingsSurviveReruns(t *testing.T) {
	resetDOM(t)
	parent := mount("ul")

	label := NewSignal("first")
	items := NewSignal([]string{"only"})

	defer BindList(parent, items.Get,
		func(s string) string { return s },
		func(string) wasmwrap.Element {
			li := el("li")
			BindText(li, label.Get)
			return li
		})()

	if got := tree(parent); got != "first" {
		t.Fatalf("tree = %q", got)
	}

	// Re-run the list effect. The item is reused, and its inner binding must
	// not have been disposed as a child of the list effect.
	items.Set([]string{"only", "second"})
	label.Set("updated")

	if got := parent.ChildAt(0).Text(); got != "updated" {
		t.Errorf("reused item text = %q, want updated — its binding was disposed", got)
	}
}

func TestBindListDisposeClears(t *testing.T) {
	resetDOM(t)
	parent := mount("ul")
	items := NewSignal([]row{{"a", "A"}, {"b", "B"}})

	dispose := BindList(parent, items.Get,
		func(r row) string { return r.id },
		func(r row) wasmwrap.Element { return el("li").SetText(r.text) })

	dispose()
	if n := len(parent.Children()); n != 0 {
		t.Errorf("%d children left after dispose", n)
	}
}

func TestBindListSkipsDuplicateKeys(t *testing.T) {
	resetDOM(t)
	parent := mount("ul")
	items := NewSignal([]row{{"a", "A"}, {"a", "again"}, {"b", "B"}})

	defer BindList(parent, items.Get,
		func(r row) string { return r.id },
		func(r row) wasmwrap.Element { return el("li").SetText(r.text) })()

	if got := tree(parent); got != "A,B" {
		t.Errorf("tree = %q, want A,B", got)
	}
}

func TestMatchRoute(t *testing.T) {
	cases := []struct {
		pattern, path string
		ok            bool
		params        Params
	}{
		{"/", "/", true, Params{}},
		{"/about", "/about", true, Params{}},
		{"/about", "/about/", true, Params{}},
		{"/about", "/other", false, nil},
		{"/projects/:slug", "/projects/thing", true, Params{"slug": "thing"}},
		{"/projects/:slug", "/projects", false, nil},
		{"/projects/:slug", "/projects/a/b", false, nil},
		{"/a/:x/b/:y", "/a/1/b/2", true, Params{"x": "1", "y": "2"}},
		{"/docs/*", "/docs/a/b/c", true, Params{"*": "a/b/c"}},
		{"/docs/*", "/docs", true, Params{"*": ""}},
		{"/*", "/anything/here", true, Params{"*": "anything/here"}},
	}

	for _, c := range cases {
		params, ok := matchRoute(c.pattern, c.path)
		if ok != c.ok {
			t.Errorf("matchRoute(%q, %q) ok = %v, want %v", c.pattern, c.path, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if len(params) != len(c.params) {
			t.Errorf("matchRoute(%q, %q) params = %v, want %v", c.pattern, c.path, params, c.params)
			continue
		}
		for k, v := range c.params {
			if params[k] != v {
				t.Errorf("matchRoute(%q, %q)[%q] = %q, want %q", c.pattern, c.path, k, params[k], v)
			}
		}
	}
}

func TestRouterSwapsViews(t *testing.T) {
	resetDOM(t)
	root := mount("div")

	Replace("/")
	built := map[string]int{}

	dispose := Router(root, []Route{
		{Path: "/", View: func(Params) wasmwrap.Element {
			built["home"]++
			return el("p").SetText("home")
		}},
		{Path: "/projects/:slug", View: func(p Params) wasmwrap.Element {
			built["project"]++
			return el("p").SetText("project:" + p.Get("slug"))
		}},
		{Path: "/*", View: func(Params) wasmwrap.Element {
			built["notfound"]++
			return el("p").SetText("404")
		}},
	})
	defer dispose()

	if got := tree(root); got != "home" {
		t.Fatalf("tree = %q, want home", got)
	}

	Navigate("/projects/dice")
	if got := tree(root); got != "project:dice" {
		t.Fatalf("tree = %q, want project:dice", got)
	}
	if n := len(root.Children()); n != 1 {
		t.Errorf("%d children after navigation, want 1 — old view not removed", n)
	}

	Navigate("/nowhere")
	if got := tree(root); got != "404" {
		t.Errorf("tree = %q, want 404", got)
	}
}

// A view's effects must be disposed when navigating away.
func TestRouterDisposesViewScope(t *testing.T) {
	resetDOM(t)
	root := mount("div")
	Replace("/")

	label := NewSignal("a")
	binds := 0

	dispose := Router(root, []Route{
		{Path: "/", View: func(Params) wasmwrap.Element {
			e := el("p")
			Effect(func() { label.Get(); binds++ })
			return e
		}},
		{Path: "/other", View: func(Params) wasmwrap.Element { return el("p").SetText("other") }},
	})
	defer dispose()

	if binds != 1 {
		t.Fatalf("binds = %d", binds)
	}
	label.Set("b")
	if binds != 2 {
		t.Fatalf("binds = %d", binds)
	}

	Navigate("/other")
	label.Set("c")
	if binds != 2 {
		t.Errorf("binds = %d after navigating away, want 2 — view scope leaked", binds)
	}
}

func TestNavigateUpdatesHistory(t *testing.T) {
	Replace("/start")
	if got := js.Global().Get("location").Get("pathname").String(); got != "/start" {
		t.Errorf("pathname = %q", got)
	}
	Navigate("/next")
	if got := Path(); got != "/next" {
		t.Errorf("Path() = %q", got)
	}
}

func TestRenderTempl(t *testing.T) {
	c := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, "<b>hi</b>")
		return err
	})
	got, err := RenderTempl(c)
	if err != nil {
		t.Fatal(err)
	}
	if got != "<b>hi</b>" {
		t.Errorf("RenderTempl = %q", got)
	}
}

func TestBindTempl(t *testing.T) {
	resetDOM(t)
	panel := mount("div")
	n := NewSignal(1)

	defer BindTempl(panel, func() templ.Component {
		v := n.Get()
		return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, "<b>"+strconv.Itoa(v)+"</b>")
			return err
		})
	})()

	if got := panel.HTML(); got != "<b>1</b>" {
		t.Errorf("html = %q", got)
	}
	n.Set(2)
	if got := panel.HTML(); got != "<b>2</b>" {
		t.Errorf("html = %q after Set", got)
	}
}

func TestTemplElement(t *testing.T) {
	c := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, "<span>x</span>")
		return err
	})
	e := TemplElement("li", c)
	if got := e.Value().Get("tagName").String(); got != "LI" {
		t.Errorf("tag = %q", got)
	}
	if got := e.HTML(); got != "<span>x</span>" {
		t.Errorf("html = %q", got)
	}
}

// A render error must surface in the DOM rather than being swallowed.
func TestBindTemplRenderError(t *testing.T) {
	resetDOM(t)
	panel := mount("div")
	boom := templ.ComponentFunc(func(context.Context, io.Writer) error {
		return io.ErrUnexpectedEOF
	})

	defer BindTempl(panel, func() templ.Component { return boom })()

	if !strings.Contains(panel.Text(), "render error") {
		t.Errorf("text = %q, want a render error", panel.Text())
	}
}

func TestHydrate(t *testing.T) {
	resetDOM(t)
	widget := mount("div").SetAttr("id", "counter")
	out := el("span").SetAttr("data-count", "")
	btn := el("button").SetAttr("data-inc", "")
	widget.Append(out, btn)

	count := NewSignal(0)
	dispose := Hydrate("#counter", func(root wasmwrap.Element) {
		BindText(root.Find("[data-count]"), func() string { return strconv.Itoa(count.Get()) })
		On(root.Find("[data-inc]"), "click", func(wasmwrap.Event) {
			count.Update(func(v int) int { return v + 1 })
		})
	})
	defer dispose()

	if out.Text() != "0" {
		t.Fatalf("text = %q", out.Text())
	}
	btn.Value().Call("dispatch", "click", js.Null())
	if out.Text() != "1" {
		t.Errorf("text = %q after click", out.Text())
	}
}

// A script included on every page must be inert where its markup is absent.
func TestHydrateMissingSelectorIsNoop(t *testing.T) {
	resetDOM(t)
	called := false
	dispose := Hydrate("#nope", func(wasmwrap.Element) { called = true })
	dispose()
	if called {
		t.Error("Hydrate ran its callback with no matching element")
	}
}

func TestHydrateAll(t *testing.T) {
	resetDOM(t)
	for range 3 {
		mount("div").Class().Add("card")
	}
	n := 0
	defer HydrateAll(".card", func(wasmwrap.Element) { n++ })()
	if n != 3 {
		t.Errorf("ran on %d elements, want 3", n)
	}
}

func TestResourceLoads(t *testing.T) {
	r := NewResource(func() (string, error) {
		time.Sleep(10 * time.Millisecond)
		return "loaded", nil
	})

	if !r.Loading.Get() {
		t.Error("Loading is false immediately after construction")
	}

	deadline := time.After(2 * time.Second)
	for r.Loading.Get() {
		select {
		case <-deadline:
			t.Fatal("resource never finished loading")
		case <-time.After(5 * time.Millisecond):
		}
	}

	if got := r.Data.Get(); got != "loaded" {
		t.Errorf("Data = %q", got)
	}
	if err := r.Err.Get(); err != nil {
		t.Errorf("Err = %v", err)
	}
}

func TestResourceError(t *testing.T) {
	r := NewResource(func() (int, error) { return 0, io.ErrUnexpectedEOF })

	deadline := time.After(2 * time.Second)
	for r.Loading.Get() {
		select {
		case <-deadline:
			t.Fatal("resource never finished")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if r.Err.Get() == nil {
		t.Error("Err is nil after a failed load")
	}
}
