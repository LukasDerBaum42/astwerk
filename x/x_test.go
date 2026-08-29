package x

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
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

func renderErr(c templ.Component) (string, error) {
	var buf bytes.Buffer
	err := c.Render(context.Background(), &buf)
	return buf.String(), err
}

func mustPanic(t *testing.T, msg string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected a panic containing %q, got none", msg)
		}
		if !strings.Contains(fmt.Sprint(r), msg) {
			t.Fatalf("panic = %q, want it to contain %q", r, msg)
		}
	}()
	fn()
}

func TestCounter(t *testing.T) {
	count := NewNumSignal(0)

	page := Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := Text(count).Render(ctx, w); err != nil {
			return err
		}
		btn := El("button", On("click", count.Inc(1)))
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

	if i, j := strings.Index(out, "const s0 = aw.signal(0);"), strings.Index(out, `<span data-x="0">`); i < 0 || j < 0 || i >= j {
		t.Errorf("declaration should precede the body:\n%s", out)
	}
	if i, j := strings.Index(out, `<span data-x="0">`), strings.Index(out, `aw.text("0", s0);`); i < 0 || j < 0 || i >= j {
		t.Errorf("bindings should follow the body:\n%s", out)
	}
}

func TestStaticWithoutDocument(t *testing.T) {
	count := NewNumSignal(7)
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
	a := NewNumSignal(1)
	b := NewNumSignal(2)

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
	url := NewStrSignal("/home")
	active := NewBoolSignal(true)
	shown := NewBoolSignal(false)
	name := NewStrSignal("Lukas")
	done := NewBoolSignal(true)

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
	color := NewStrSignal("red")
	count := NewNumSignal(0)

	page := Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := El("p", Style("color", color)).Render(templ.WithChildren(ctx, templ.Raw("hi")), w); err != nil {
			return err
		}
		return El("a", On("click", count.Inc(1))).Render(templ.WithChildren(ctx, templ.Raw("inc")), w)
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
	price := NewNumSignal(10)
	qty := NewNumSignal(2)
	total := Computed(Mul(price, qty))

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
	count := NamedNumSignal("count", 5)

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
	count := NamedNumSignal("for", 3)
	out := render(Document(Text(count)))
	if !strings.Contains(out, "const s0 = aw.signal(3);") {
		t.Errorf("reserved name should fall back to an assigned name:\n%s", out)
	}
	// A name that fell back is not registered: it is not stable API.
	if strings.Contains(out, "aw.register") {
		t.Errorf("assigned name was registered:\n%s", out)
	}
}

func TestRegistry(t *testing.T) {
	count := NamedNumSignal("count", 0)
	anon := NewNumSignal(1)

	page := Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := Text(count).Render(ctx, w); err != nil {
			return err
		}
		return Text(anon).Render(ctx, w)
	}))

	out := render(page)
	for _, want := range []string{
		`aw.register("count", count);`,
		"globalThis.aw = { signal, text, state, register };",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `aw.register("s1"`) {
		t.Errorf("assigned name was registered:\n%s", out)
	}
}

func TestNoRegistryWithoutNamedSignals(t *testing.T) {
	out := render(Document(Text(NewNumSignal(0))))
	if strings.Contains(out, "aw.register") || strings.Contains(out, "aw.state") {
		t.Errorf("unnamed page shipped the registry:\n%s", out)
	}
	if !strings.Contains(out, `globalThis.aw = { signal, text };`) {
		t.Errorf("expected text-only export list:\n%s", out)
	}
}

func TestEffect(t *testing.T) {
	count := NamedNumSignal("count", 0)
	script := templ.Raw("<script>\n\t\t\tdocument.title = \"Count: \" + count.get();\n\t\t</script>")
	want := `aw.effect(() => { document.title = "Count: " + count.get(); });`

	page := Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := Text(count).Render(ctx, w); err != nil {
			return err
		}
		return Effect(script).Render(ctx, w)
	}))
	if out := render(page); !strings.Contains(out, want) {
		t.Errorf("argument form missing %q:\n%s", want, out)
	}

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
	count := NewNumSignal(0)
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
	shown := NewBoolSignal(true)
	out := render(Document(When(shown, templ.Raw(`<p>secret</p>`))))
	for _, want := range []string{
		`<div data-x="0"><p>secret</p></div>`,
		`aw.when("0", "\u003cp\u003esecret\u003c/p\u003e", s0);`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}

	hidden := NewBoolSignal(false)
	out = render(Document(When(hidden, templ.Raw(`<p>secret</p>`))))
	if !strings.Contains(out, `<div data-x="0"></div>`) {
		t.Errorf("false condition should render an empty container:\n%s", out)
	}
}

func TestWhenStatic(t *testing.T) {
	shown := NewBoolSignal(true)
	if got, want := render(When(shown, templ.Raw(`<p>x</p>`))), `<p>x</p>`; got != want {
		t.Errorf("When outside Document = %q, want %q", got, want)
	}
}

func TestList(t *testing.T) {
	type Todo struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	todos := NewListSignal([]Todo{
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

func TestListMethodForm(t *testing.T) {
	type Todo struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	todos := NewListSignal([]Todo{{ID: "1", Text: "hi"}})
	row := func(t Todo) templ.Component { return templ.Raw(`<li>` + templ.EscapeString(t.Text) + `</li>`) }

	got := render(todos.List("id", row, ""))
	if want := `<div data-key="1"><li>hi</li></div>`; got != want {
		t.Errorf("List method outside Document = %q, want %q", got, want)
	}
}

func TestDeadStateElimination(t *testing.T) {
	used := NewNumSignal(1)
	unused := NewNumSignal(2)
	Computed(Mul(unused, 2))

	out := render(Document(Text(used)))
	if !strings.Contains(out, "const s0 = aw.signal(1);") {
		t.Errorf("used signal missing:\n%s", out)
	}
	if strings.Contains(out, "aw.signal(2)") || strings.Contains(out, "aw.computed") {
		t.Errorf("unused state was emitted:\n%s", out)
	}
}

func TestLitAndNot(t *testing.T) {
	count := NewNumSignal(5)
	open := NewBoolSignal(false)

	page := Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := El("button", On("click", count.SetTo(0))).Render(templ.WithChildren(ctx, templ.Raw("reset")), w); err != nil {
			return err
		}
		return El("button", On("click", open.Toggle())).Render(templ.WithChildren(ctx, templ.Raw("toggle")), w)
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

func TestRuntimeSetIteratesASnapshot(t *testing.T) {
	if !strings.Contains(runtimeCore, "[...subs].forEach((f) => f(v))") {
		t.Errorf("runtime set() must iterate a snapshot of subscribers and pass the value, got:\n%s", runtimeCore)
	}
}

func TestMinified(t *testing.T) {
	count := NewNumSignal(0)
	page := Document(Text(count), Minified())
	out := render(page)

	dev := render(Document(Text(count)))
	if len(out) >= len(dev) {
		t.Errorf("minified output (%d bytes) not smaller than dev (%d bytes)", len(out), len(dev))
	}

	if !strings.Contains(out, "globalThis.aw = { signal, text };") {
		t.Errorf("minified runtime missing exports:\n%s", out)
	}
	if !strings.Contains(out, "const s0 = aw.signal(0);") {
		t.Errorf("declarations missing:\n%s", out)
	}
	// The minified runtime must keep the signal's set method: the fragments
	// carry // comments immediately before it, and stripping line breaks would
	// let a comment swallow set() — the production bug.
	if !strings.Contains(out, "set: (nv) => { if (nv !== v) { v = nv; [...subs].forEach((f) => f(v)); } }") {
		t.Errorf("minified runtime lost the signal's set method:\n%s", out)
	}
}

func TestMinifyJSPreservesLineComments(t *testing.T) {
	in := "a();\n\t// a comment\n\tb();"
	if got, want := minifyJS(in), "a();\n// a comment\nb();"; got != want {
		t.Errorf("minifyJS = %q, want %q (line breaks must survive so comments stay line comments)", got, want)
	}
}

func TestFullPage(t *testing.T) {
	count := NamedNumSignal("count", 0)
	name := NewStrSignal("Lukas")
	price := NewNumSignal(10)
	qty := NewNumSignal(2)
	total := Computed(Mul(price, qty))
	shown := NewBoolSignal(true)
	type Todo struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	todos := NewListSignal([]Todo{{ID: "1", Text: "buy milk"}})
	row := func(t Todo) templ.Component {
		return templ.Raw(`<li>` + templ.EscapeString(t.Text) + `</li>`)
	}

	page := Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := Text(count).Render(ctx, w); err != nil {
			return err
		}
		if err := El("button", On("click", count.Inc(1))).Render(templ.WithChildren(ctx, templ.Raw("+")), w); err != nil {
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
		"const s5 = aw.signal([{\"id\":\"1\",\"text\":\"buy milk\"}]);",
		"globalThis.aw = { signal, effect, computed, text, on, value, when, list, state, register };",
		"aw.register(\"count\", count);",
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

func TestType(t *testing.T) {
	type Todo struct{ ID string }
	cases := []struct {
		name string
		s    State
		want reflect.Type
	}{
		{"int", NewNumSignal(0), reflect.TypeOf(0)},
		{"int64", NewNumSignal(int64(0)), reflect.TypeOf(int64(0))},
		{"float64", NewNumSignal(0.0), reflect.TypeOf(float64(0))},
		{"bool", NewBoolSignal(false), reflect.TypeOf(false)},
		{"string", NewStrSignal(""), reflect.TypeOf("")},
		{"list", NewListSignal([]Todo{}), reflect.TypeOf([]Todo{})},
		{"memo", Computed(Add(NewNumSignal(1), NewNumSignal(2))), reflect.TypeOf(0)},
	}
	for _, c := range cases {
		if got := c.s.Type(); got != c.want {
			t.Errorf("%s: Type() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestOperandTypeCheck(t *testing.T) {
	count := NewNumSignal(0)

	if e := Add(count, 1); e.v != 1 {
		t.Errorf("literal operand: build-time value = %v, want 1", e.v)
	}
	other := NewNumSignal(2)
	if e := Add(count, other); e.v != 2 {
		t.Errorf("value operand: build-time value = %v, want 2", e.v)
	}
	if e := Add(count, Mul(count, 2)); e.v != 0 {
		t.Errorf("expression operand: build-time value = %v, want 0", e.v)
	}

	mustPanic(t, "does not match value type", func() { Add(count, NewStrSignal("x")) })
	mustPanic(t, "does not match value type", func() { Add(count, "x") })
}

func TestSeqAndIf(t *testing.T) {
	count := NewNumSignal(0)
	open := NewBoolSignal(false)
	dark := NewBoolSignal(false)
	accent := NewStrSignal("light")

	page := Document(templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := El("button", On("click", Seq(count.Dec(1), open.SetTo(false)))).Render(templ.WithChildren(ctx, templ.Raw("x")), w); err != nil {
			return err
		}
		if err := El("button", On("click", If(dark, accent.SetTo("light")))).Render(templ.WithChildren(ctx, templ.Raw("y")), w); err != nil {
			return err
		}
		return El("button", On("click", If(open, count.Inc(1), count.Dec(1)))).Render(templ.WithChildren(ctx, templ.Raw("z")), w)
	}))

	out := render(page)
	for _, want := range []string{
		`aw.on("0", "click", () => { s0.set(s0.get() - 1); s1.set(false); });`,
		`if (s2.get()) { s3.set("light") }`,
		`if (s1.get()) { s0.set(s0.get() + 1) } else { s0.set(s0.get() - 1) }`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestConcat(t *testing.T) {
	name := NewStrSignal("astwerk")
	out := render(Document(Text(Computed(Concat(name, " Ltd.")))))
	for _, want := range []string{
		`<span data-x="0">astwerk Ltd.</span>`,
		`const s1 = aw.computed([s0], () => s0.get() + " Ltd.");`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestComparisons(t *testing.T) {
	n := NewNumSignal(75)
	cases := []struct {
		e    Expr[bool]
		want bool
	}{
		{Gt(n, 50), true},
		{Lt(n, 100), true},
		{Gte(n, 75), true},
		{Lte(n, 74), false},
		{Eq(n, 75), true},
		{Ne(n, 75), false},
	}
	for _, c := range cases {
		if c.e.v != c.want {
			t.Errorf("comparison build-time value = %v, want %v", c.e.v, c.want)
		}
	}
}

func TestComparisonRender(t *testing.T) {
	n := NewNumSignal(75)
	ops := []struct {
		e   Expr[bool]
		op  string
		val string
	}{
		{Gt(n, 50), ">", "true"},
		{Lt(n, 100), "<", "true"},
		{Gte(n, 75), ">=", "true"},
		{Lte(n, 74), "<=", "false"},
		{Eq(n, 75), "===", "true"},
		{Ne(n, 75), "!==", "false"},
	}
	for _, c := range ops {
		out := render(Document(Text(Computed(c.e))))
		for _, want := range []string{
			`<span data-x="0">` + c.val + `</span>`,
			"() => s0.get() " + c.op + " ",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("operator %q missing %q:\n%s", c.op, want, out)
			}
		}
	}
}

func TestBareValueRendersText(t *testing.T) {
	count := NewNumSignal(7)
	if bare, explicit := render(count), render(count.Text()); bare != explicit {
		t.Errorf("bare value = %q, Text() = %q; want identical", bare, explicit)
	}
	if got, want := render(count), `<span>7</span>`; got != want {
		t.Errorf("bare value outside Document = %q, want %q", got, want)
	}
}

func TestIncWithSignalAmount(t *testing.T) {
	count := NewNumSignal(0)
	step := NamedNumSignal("step", 2)
	out := render(Document(El("button", On("click", count.Inc(step)))))
	for _, want := range []string{
		`aw.on("0", "click", () => { s0.set(s0.get() + step.get()); });`,
		"const s0 = aw.signal(0);",
		"const step = aw.signal(2);",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestWrap(t *testing.T) {
	count := NewNumSignal(0)
	btn := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<button class="btn btn-primary">+</button>`)
		return err
	})

	page := Document(Wrap(btn, HtmlAttr("aria-label", "increment"), On("click", count.Inc(1))))
	out := render(page)
	for _, want := range []string{
		`<button data-x="0" aria-label="increment" class="btn btn-primary">+</button>`,
		`aw.on("0", "click", () => { s0.set(s0.get() + 1); });`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestWrapStatic(t *testing.T) {
	btn := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<button>+</button>`)
		return err
	})
	out := render(Wrap(btn, On("click", NewNumSignal(0).Inc(1))))
	if got, want := out, `<button>+</button>`; got != want {
		t.Errorf("Wrap outside Document = %q, want %q", got, want)
	}
}

func TestWrapPreservesLeadingText(t *testing.T) {
	count := NewNumSignal(0)
	btn := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w, "\n  <button>+</button>")
		return err
	})
	out := render(Document(Wrap(btn, On("click", count.Inc(1)))))
	if !strings.Contains(out, "\n  <button data-x=\"0\">+</button>") {
		t.Errorf("leading text dropped:\n%q", out)
	}
}

func TestWrapTextOnly(t *testing.T) {
	if _, err := renderErr(Document(Wrap(templ.Raw("hello"), On("click", NewNumSignal(0).Inc(1))))); err == nil || !strings.Contains(err.Error(), "no element") {
		t.Errorf("expected a no-element error, got %v", err)
	}
}

func TestWrapMultiRoot(t *testing.T) {
	c := templ.Raw(`<div>a</div><div>b</div>`)
	if _, err := renderErr(Document(Wrap(c, On("click", NewNumSignal(0).Inc(1))))); err == nil || !strings.Contains(err.Error(), "multiple root") {
		t.Errorf("expected a multiple-root error, got %v", err)
	}
}
