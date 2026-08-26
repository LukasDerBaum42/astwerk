package x

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func render(c templ.Component) string {
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		panic(err)
	}
	return buf.String()
}

func TestCounter(t *testing.T) {
	count := NewSignal(0)

	page := Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := Text(count).Render(ctx, w); err != nil {
			return err
		}
		btn := El("button", On("click", count.Set(count.Add(1))))
		return btn.Render(templ.WithChildren(ctx, templ.Raw("+")), w)
	}))

	out := render(page)
	for _, want := range []string{
		`<span data-x="0">0</span>`,
		`<button data-x="1">+</button>`,
		"const s0 = aw.signal(0);",
		`aw.text("0", s0);`,
		`aw.on("1", "click", () => { s0.set(s0.get() + 1); });`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	// Declarations load before the body; bindings after it.
	if i, j := strings.Index(out, "const s0 = aw.signal(0);"), strings.Index(out, `<span data-x="0">`); i < 0 || j < 0 || i >= j {
		t.Errorf("declaration should precede the body:\n%s", out)
	}
	if i, j := strings.Index(out, `<span data-x="0">`), strings.Index(out, `aw.text("0", s0);`); i < 0 || j < 0 || i >= j {
		t.Errorf("bindings should follow the body:\n%s", out)
	}
}

func TestStaticWithoutDocument(t *testing.T) {
	count := NewSignal(7)
	if got, want := render(Text(count)), `<span>7</span>`; got != want {
		t.Errorf("Text outside Document = %q, want %q", got, want)
	}
}

func TestStaticPageEmitsNoScript(t *testing.T) {
	out := render(Document(templ.Raw("<h1>Hello World</h1>")))
	if strings.Contains(out, "<script>") {
		t.Errorf("static page emitted a script:\n%s", out)
	}
	if !strings.Contains(out, "<h1>Hello World</h1>") {
		t.Errorf("static page lost its body:\n%s", out)
	}
}

func TestScript(t *testing.T) {
	ctx := templ.WithChildren(context.Background(), templ.Raw(`console.log("hello");`))
	var buf bytes.Buffer
	if err := Script().Render(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), `<script>console.log("hello");</script>`; got != want {
		t.Errorf("Script = %q, want %q", got, want)
	}
}

func TestJS(t *testing.T) {
	script := templ.Raw(`<script>console.log("hello");</script>`)
	var buf bytes.Buffer
	if err := JS(script).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), `<script>console.log("hello");</script>`; got != want {
		t.Errorf("JS = %q, want %q", got, want)
	}
}

func TestNestedDocument(t *testing.T) {
	a := NewSignal(1)
	b := NewSignal(2)

	page := Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := Text(a).Render(ctx, w); err != nil {
			return err
		}
		return Document(Text(b)).Render(ctx, w)
	}))

	out := render(page)
	if got := strings.Count(out, "globalThis.aw ="); got != 1 {
		t.Errorf("runtime emitted %d times, want 1:\n%s", got, out)
	}
	for _, want := range []string{
		"const s0 = aw.signal(1);",
		"const s1 = aw.signal(2);",
		`<span data-x="0">1</span>`,
		`<span data-x="1">2</span>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestBindings(t *testing.T) {
	url := NewSignal("/home")
	active := NewSignal(true)
	shown := NewSignal(false)
	name := NewSignal("Lukas")
	done := NewSignal(true)

	page := Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := El("a", Attr("href", url)).Render(templ.WithChildren(ctx, templ.Raw("home")), w); err != nil {
			return err
		}
		if err := El("div", Class("on", active), Show(shown)).Render(ctx, w); err != nil {
			return err
		}
		if err := Input(name).Render(ctx, w); err != nil {
			return err
		}
		return Checkbox(done).Render(ctx, w)
	}))

	out := render(page)
	for _, want := range []string{
		`<a data-x="0" href="/home">home</a>`,
		`<div data-x="1" class="on" style="display:none"></div>`,
		`<input data-x="2" value="Lukas">`,
		`<input type="checkbox" data-x="3" checked>`,
		`aw.attr("0", "href", s0);`,
		`aw.cls("1", "on", s1);`,
		`aw.show("1", s2);`,
		`aw.value("2", s3);`,
		`aw.checked("3", s4);`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestStyleAndOn(t *testing.T) {
	color := NewSignal("red")
	count := NewSignal(0)

	page := Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := El("p", Style("color", color)).Render(templ.WithChildren(ctx, templ.Raw("hi")), w); err != nil {
			return err
		}
		return El("a", On("click", count.Set(count.Add(1)))).Render(templ.WithChildren(ctx, templ.Raw("inc")), w)
	}))

	out := render(page)
	for _, want := range []string{
		`<p data-x="0" style="color: red">hi</p>`,
		`<a data-x="1">inc</a>`,
		`aw.style("0", "color", s0);`,
		`aw.on("1", "click", () => { s1.set(s1.get() + 1); });`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestComputed(t *testing.T) {
	price := NewSignal(10)
	qty := NewSignal(2)
	total := Computed(price.Mul(qty))

	if got := total.Get(); got != 20 {
		t.Errorf("total.Get() = %d, want 20", got)
	}

	page := Document(Text(total))
	out := render(page)
	for _, want := range []string{
		`<span data-x="0">20</span>`,
		"const s0 = aw.signal(10);",
		"const s1 = aw.signal(2);",
		"const s2 = aw.computed([s0, s1], () => s0.get() * s1.get());",
		`aw.text("0", s2);`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestNamedScriptAccess(t *testing.T) {
	count := Named("count", 5)

	page := Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := Text(count).Render(ctx, w); err != nil {
			return err
		}
		return Script().Render(templ.WithChildren(ctx, templ.Raw(`aw.effect(() => console.log(count.get()));`)), w)
	}))

	out := render(page)
	for _, want := range []string{
		"const count = aw.signal(5);",
		"aw.text(\"0\", count);",
		`<script>aw.effect(() => console.log(count.get()));</script>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestInvalidNameFallsBack(t *testing.T) {
	count := Named("for", 3)
	out := render(Document(Text(count)))
	if !strings.Contains(out, "const s0 = aw.signal(3);") {
		t.Errorf("reserved name should fall back to an assigned name:\n%s", out)
	}
}

func TestEffect(t *testing.T) {
	count := Named("count", 0)
	script := templ.Raw("<script>\n\t\t\tdocument.title = \"Count: \" + count.get();\n\t\t</script>")
	want := `aw.effect(() => { document.title = "Count: " + count.get(); });`

	// argument form
	page := Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := Text(count).Render(ctx, w); err != nil {
			return err
		}
		return Effect(script).Render(ctx, w)
	}))
	if out := render(page); !strings.Contains(out, want) {
		t.Errorf("argument form missing %q:\n%s", want, out)
	}

	// children form
	page = Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := Text(count).Render(ctx, w); err != nil {
			return err
		}
		return Effect().Render(templ.WithChildren(ctx, script), w)
	}))
	if out := render(page); !strings.Contains(out, want) {
		t.Errorf("children form missing %q:\n%s", want, out)
	}
}

func TestMinimalRuntime(t *testing.T) {
	count := NewSignal(0)
	out := render(Document(Text(count)))
	for _, absent := range []string{"aw.effect", "aw.computed", "aw.when", "aw.list", "aw.value", "aw.checked", "aw.attr"} {
		if strings.Contains(out, absent) {
			t.Errorf("text-only page pulled in %s:\n%s", absent, out)
		}
	}
	if !strings.Contains(out, `globalThis.aw = { signal, text };`) {
		t.Errorf("expected a text-only export list:\n%s", out)
	}
}

func TestWhen(t *testing.T) {
	shown := NewSignal(true)
	out := render(Document(When(shown, templ.Raw(`<p>secret</p>`))))
	for _, want := range []string{
		`<div data-x="0"><p>secret</p></div>`,
		`aw.when("0", "\u003cp\u003esecret\u003c/p\u003e", s0);`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}

	hidden := NewSignal(false)
	out = render(Document(When(hidden, templ.Raw(`<p>secret</p>`))))
	if !strings.Contains(out, `<div data-x="0"></div>`) {
		t.Errorf("false condition should render an empty container:\n%s", out)
	}
}

func TestWhenStatic(t *testing.T) {
	shown := NewSignal(true)
	if got, want := render(When(shown, templ.Raw(`<p>x</p>`))), `<p>x</p>`; got != want {
		t.Errorf("When outside Document = %q, want %q", got, want)
	}
}

func TestList(t *testing.T) {
	type Todo struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	todos := NewSignal([]Todo{
		{ID: "1", Text: "buy milk"},
		{ID: "2", Text: "write code"},
	})
	row := func(t Todo) templ.Component {
		return templ.Raw(`<li>` + templ.EscapeString(t.Text) + `</li>`)
	}

	out := render(Document(List(todos, "id", row, "rowTemplate")))
	for _, want := range []string{
		`<div data-x="0"><div data-key="1"><li>buy milk</li></div><div data-key="2"><li>write code</li></div></div>`,
		`aw.list("0", s0, "id", rowTemplate);`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestListStatic(t *testing.T) {
	type Todo struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	todos := NewSignal([]Todo{{ID: "1", Text: "hi"}})
	row := func(t Todo) templ.Component {
		return templ.Raw(`<li>` + templ.EscapeString(t.Text) + `</li>`)
	}
	got := render(List(todos, "id", row, ""))
	if want := `<div data-key="1"><li>hi</li></div>`; got != want {
		t.Errorf("List outside Document = %q, want %q", got, want)
	}
}

func TestDeadStateElimination(t *testing.T) {
	used := NewSignal(1)
	unused := NewSignal(2)
	Computed(unused.Mul(2))

	out := render(Document(Text(used)))
	if !strings.Contains(out, "const s0 = aw.signal(1);") {
		t.Errorf("used signal missing:\n%s", out)
	}
	if strings.Contains(out, "aw.signal(2)") || strings.Contains(out, "aw.computed") {
		t.Errorf("unused state was emitted:\n%s", out)
	}
}

func TestLitAndNot(t *testing.T) {
	count := NewSignal(5)
	open := NewSignal(false)

	page := Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := El("button", On("click", count.Set(Lit(0)))).Render(templ.WithChildren(ctx, templ.Raw("reset")), w); err != nil {
			return err
		}
		return El("button", On("click", open.Set(Not(open)))).Render(templ.WithChildren(ctx, templ.Raw("toggle")), w)
	}))

	out := render(page)
	for _, want := range []string{
		`aw.on("0", "click", () => { s0.set(0); });`,
		`aw.on("1", "click", () => { s1.set(!s1.get()); });`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// The runtime's set() must iterate a snapshot of its subscribers and pass the
// new value: an effect re-subscribes when it re-runs, and iterating the live
// set would pick the re-added subscription up and call it again — an infinite
// loop that freezes the page (found the hard way on the docs site). Pin the
// shape so it cannot regress.
func TestRuntimeSetIteratesASnapshot(t *testing.T) {
	if !strings.Contains(runtimeCore, "[...subs].forEach((f) => f(v))") {
		t.Errorf("runtime set() must iterate a snapshot of subscribers and pass the value, got:\n%s", runtimeCore)
	}
}

func TestMinified(t *testing.T) {
	count := NewSignal(0)
	page := Document(Text(count), Minified())
	out := render(page)

	dev := render(Document(Text(count)))
	if len(out) >= len(dev) {
		t.Errorf("minified output (%d bytes) not smaller than dev (%d bytes)", len(out), len(dev))
	}

	head := out[strings.Index(out, "(() =>"):strings.Index(out, "\n\nconst")]
	if strings.Contains(head, "\n") {
		t.Errorf("minified runtime still has line breaks:\n%s", head)
	}
	if !strings.Contains(head, "globalThis.aw = { signal, text };") {
		t.Errorf("minified runtime missing exports:\n%s", head)
	}
	if !strings.Contains(out, "const s0 = aw.signal(0);") {
		t.Errorf("declarations missing:\n%s", out)
	}
}

func TestFullPage(t *testing.T) {
	count := Named("count", 0)
	name := NewSignal("Lukas")
	price := NewSignal(10)
	qty := NewSignal(2)
	total := Computed(price.Mul(qty))
	shown := NewSignal(true)
	type Todo struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	todos := NewSignal([]Todo{{ID: "1", Text: "buy milk"}})
	row := func(t Todo) templ.Component {
		return templ.Raw(`<li>` + templ.EscapeString(t.Text) + `</li>`)
	}

	page := Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := Text(count).Render(ctx, w); err != nil {
			return err
		}
		if err := El("button", On("click", count.Set(count.Add(1)))).Render(templ.WithChildren(ctx, templ.Raw("+")), w); err != nil {
			return err
		}
		if err := Input(name).Render(ctx, w); err != nil {
			return err
		}
		if err := Text(total).Render(ctx, w); err != nil {
			return err
		}
		if err := When(shown, templ.Raw(`<b>tip</b>`)).Render(ctx, w); err != nil {
			return err
		}
		if err := List(todos, "id", row, "rowTemplate").Render(ctx, w); err != nil {
			return err
		}
		return Effect().Render(templ.WithChildren(ctx, templ.Raw(`<script>document.title = "C: " + count.get();</script>`)), w)
	}))

	out := render(page)
	for _, want := range []string{
		`<span data-x="0">0</span>`,
		`<button data-x="1">+</button>`,
		`<input data-x="2" value="Lukas">`,
		`<span data-x="3">20</span>`,
		`<div data-x="4"><b>tip</b></div>`,
		`<div data-x="5"><div data-key="1"><li>buy milk</li></div></div>`,
		"const count = aw.signal(0);",
		`const s0 = aw.signal("Lukas");`,
		"const s1 = aw.signal(10);",
		"const s2 = aw.signal(2);",
		"const s3 = aw.computed([s1, s2], () => s1.get() * s2.get());",
		"const s4 = aw.signal(true);",
		`const s5 = aw.signal([{"id":"1","text":"buy milk"}]);`,
		"globalThis.aw = { signal, effect, computed, text, on, value, when, list };",
		`aw.text("0", count);`,
		`aw.on("1", "click", () => { count.set(count.get() + 1); });`,
		`aw.value("2", s0);`,
		`aw.text("3", s3);`,
		`aw.when("4", "\u003cb\u003etip\u003c/b\u003e", s4);`,
		`aw.list("5", s5, "id", rowTemplate);`,
		`aw.effect(() => { document.title = "C: " + count.get(); });`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}
