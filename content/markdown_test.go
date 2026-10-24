package content

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/LukasDerBaum42/astwerk/ssg"
	"github.com/a-h/templ"
)

type calloutProps struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

func testComponents() *Components {
	cs := NewComponents()
	Register(cs, "note", func(_ ssg.Ctx, text string) templ.Component {
		return templ.Raw("<span>note: " + text + "</span>")
	})
	Register(cs, "box", func(_ ssg.Ctx, _ struct{}) templ.Component {
		return templ.Raw("<div>box</div>")
	})
	Register(cs, "callout", func(ctx ssg.Ctx, p calloutProps) templ.Component {
		return templ.Raw(`<aside class="` + p.Kind + `">` + p.Text + `:` + ctx.Path + `</aside>`)
	})
	return cs
}

func renderPage(t *testing.T, p Page, ctx ssg.Ctx) string {
	t.Helper()
	var b strings.Builder
	if err := p.Render(ctx).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestComponentTwoArgs(t *testing.T) {
	cs := NewComponents()
	Register2(cs, "figure", func(_ ssg.Ctx, src, caption string) templ.Component {
		return templ.Raw("<figure>" + src + "|" + caption + "</figure>")
	})
	l := NewLoader(nil).WithComponents(cs)

	p, err := l.Parse(`{{@figure("a.png", "Caption")}}` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := renderPage(t, p, ssg.Ctx{}); !strings.Contains(got, "<figure>a.png|Caption</figure>") {
		t.Errorf("output = %q", got)
	}
}

func TestComponentTwoArgsBareWordAndNumber(t *testing.T) {
	cs := NewComponents()
	Register2(cs, "pair", func(_ ssg.Ctx, name string, n int) templ.Component {
		return templ.Raw("<span>" + name + ":" + strconv.Itoa(n) + "</span>")
	})
	l := NewLoader(nil).WithComponents(cs)

	p, err := l.Parse(`{{@pair(hello, 5)}}` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := renderPage(t, p, ssg.Ctx{}); !strings.Contains(got, "<span>hello:5</span>") {
		t.Errorf("output = %q", got)
	}
}

func TestComponentTwoArgsWrongCount(t *testing.T) {
	cs := NewComponents()
	Register2(cs, "pair", func(_ ssg.Ctx, a, b string) templ.Component { return templ.Raw("") })
	l := NewLoader(nil).WithComponents(cs)

	_, err := l.Parse(`{{@pair(only-one)}}` + "\n")
	if err == nil || !strings.Contains(err.Error(), "expected 2 arguments") {
		t.Fatalf("err = %v", err)
	}
}

func TestComponentSingleArgWithComma(t *testing.T) {
	l := NewLoader(nil).WithComponents(testComponents())
	p, err := l.Parse(`{{@note("a, b")}}` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := renderPage(t, p, ssg.Ctx{}); !strings.Contains(got, "note: a, b") {
		t.Errorf("output = %q", got)
	}
}

func TestComponentThreeArgs(t *testing.T) {
	cs := NewComponents()
	Register3(cs, "triple", func(_ ssg.Ctx, a string, b int, c bool) templ.Component {
		return templ.Raw("<span>" + a + ":" + strconv.Itoa(b) + ":" + strconv.FormatBool(c) + "</span>")
	})
	l := NewLoader(nil).WithComponents(cs)

	p, err := l.Parse(`{{@triple(x, 2, true)}}` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := renderPage(t, p, ssg.Ctx{}); !strings.Contains(got, "<span>x:2:true</span>") {
		t.Errorf("output = %q", got)
	}
}

func TestExternalLinks(t *testing.T) {
	l := NewLoader(nil).WithRenderOptions(WithExternalLinks("_blank"))
	p, err := l.Parse("[Ext](https://example.com) [Int](/about/) <a href=\"//cdn.example.com/x\">cdn</a>\n")
	if err != nil {
		t.Fatal(err)
	}
	got := renderPage(t, p, ssg.Ctx{})

	if !strings.Contains(got, `href="https://example.com" target="_blank" rel="noopener noreferrer"`) {
		t.Errorf("external markdown link = %q", got)
	}
	if strings.Contains(got, `href="/about/" target=`) {
		t.Errorf("internal link was externalized: %q", got)
	}
	if !strings.Contains(got, `href="//cdn.example.com/x" target="_blank"`) {
		t.Errorf("protocol-relative link = %q", got)
	}
}

func TestExternalLinksExistingTarget(t *testing.T) {
	l := NewLoader(nil).WithRenderOptions(WithExternalLinks("_blank"))
	p, err := l.Parse(`<a href="https://x" target="_self">x</a>` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	got := renderPage(t, p, ssg.Ctx{})
	if strings.Contains(got, "_blank") {
		t.Errorf("existing target was overwritten: %q", got)
	}
	if !strings.Contains(got, `target="_self"`) {
		t.Errorf("output = %q", got)
	}
}

func TestExternalLinksCodeBlock(t *testing.T) {
	l := NewLoader(nil).WithRenderOptions(WithExternalLinks("_blank"))
	p, err := l.Parse("```\n<a href=\"https://x\">x</a>\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := renderPage(t, p, ssg.Ctx{}); strings.Contains(got, "target=") {
		t.Errorf("code block was rewritten: %q", got)
	}
}

func TestComponentInline(t *testing.T) {
	l := NewLoader(nil).WithComponents(testComponents())
	p, err := l.Parse("Before {{@note(hello)}} after.\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := renderPage(t, p, ssg.Ctx{}); !strings.Contains(got, "Before <span>note: hello</span> after.") {
		t.Errorf("output = %q", got)
	}
}

func TestComponentBlockDropsParagraph(t *testing.T) {
	l := NewLoader(nil).WithComponents(testComponents())
	p, err := l.Parse("{{@box()}}\n")
	if err != nil {
		t.Fatal(err)
	}
	got := renderPage(t, p, ssg.Ctx{})
	if strings.Contains(got, "<p>") {
		t.Errorf("component kept its paragraph: %q", got)
	}
	if !strings.Contains(got, "<div>box</div>") {
		t.Errorf("output = %q", got)
	}
}

func TestComponentNamedProps(t *testing.T) {
	l := NewLoader(nil).WithComponents(testComponents())
	p, err := l.Parse("{{@callout(kind=warning, text=\"Careful\")}}\n")
	if err != nil {
		t.Fatal(err)
	}
	got := renderPage(t, p, ssg.Ctx{Path: "about/"})
	if want := `<aside class="warning">Careful:about/</aside>`; !strings.Contains(got, want) {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestComponentJSONProps(t *testing.T) {
	l := NewLoader(nil).WithComponents(testComponents())
	p, err := l.Parse(`{{@callout({"kind": "note", "text": "Hi"})}}` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	got := renderPage(t, p, ssg.Ctx{})
	if want := `<aside class="note">Hi:</aside>`; !strings.Contains(got, want) {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestComponentUnknown(t *testing.T) {
	l := NewLoader(nil).WithComponents(testComponents())
	_, err := l.Parse("first\n\n{{@nope()}}\n")
	if err == nil || !strings.Contains(err.Error(), "unknown component") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("err = %v, want the line number", err)
	}
}

func TestComponentWithoutRegistry(t *testing.T) {
	_, err := Parse("{{@box()}}\n")
	if err == nil || !strings.Contains(err.Error(), "no component registry") {
		t.Fatalf("err = %v", err)
	}
}

func TestComponentBadProps(t *testing.T) {
	l := NewLoader(nil).WithComponents(testComponents())
	_, err := l.Parse("{{@callout(kind=1)}}\n")
	if err == nil {
		t.Fatal("expected a props error")
	}
}

func TestComponentInCodeBlockIsLiteral(t *testing.T) {
	l := NewLoader(nil).WithComponents(testComponents())
	p, err := l.Parse("```\n{{@box()}}\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	got := renderPage(t, p, ssg.Ctx{})
	if !strings.Contains(got, "{{@box()}}") {
		t.Errorf("code block was parsed: %q", got)
	}
}

func TestRootRelativeURLs(t *testing.T) {
	p, err := Parse("[About](/about/) and <img src=\"/img/x.png\">\n")
	if err != nil {
		t.Fatal(err)
	}
	got := renderPage(t, p, ssg.Ctx{Path: "updates/post/", Relative: true})
	if !strings.Contains(got, `href="../../about/"`) {
		t.Errorf("markdown link not rewritten: %q", got)
	}
	if !strings.Contains(got, `src="../../img/x.png"`) {
		t.Errorf("raw HTML not rewritten: %q", got)
	}
}

func TestRootRelativeURLsUseBase(t *testing.T) {
	p, err := Parse("[About](/about/)\n")
	if err != nil {
		t.Fatal(err)
	}
	got := renderPage(t, p, ssg.Ctx{Base: "/site"})
	if !strings.Contains(got, `href="/site/about/"`) {
		t.Errorf("output = %q", got)
	}
}

func TestAbsoluteURLsUntouched(t *testing.T) {
	p, err := Parse("[X](https://example.com) and <img src=\"//cdn.example.com/x.png\">\n")
	if err != nil {
		t.Fatal(err)
	}
	got := renderPage(t, p, ssg.Ctx{Path: "a/b/", Relative: true})
	if !strings.Contains(got, `href="https://example.com"`) {
		t.Errorf("https link changed: %q", got)
	}
	if !strings.Contains(got, `src="//cdn.example.com/x.png"`) {
		t.Errorf("protocol-relative URL changed: %q", got)
	}
}

func TestComponentOutputURLsAreNotRewritten(t *testing.T) {
	cs := NewComponents()
	Register(cs, "absolute", func(_ ssg.Ctx, _ struct{}) templ.Component {
		return templ.Raw(`<a href="/from-component">x</a>`)
	})
	l := NewLoader(nil).WithComponents(cs)
	p, err := l.Parse("{{@absolute()}}\n")
	if err != nil {
		t.Fatal(err)
	}
	got := renderPage(t, p, ssg.Ctx{Path: "a/b/", Relative: true})
	if !strings.Contains(got, `href="/from-component"`) {
		t.Errorf("component output was rewritten: %q", got)
	}
}
