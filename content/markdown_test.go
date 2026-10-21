package content

import (
	"context"
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
