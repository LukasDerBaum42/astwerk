package x

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/a-h/templ"
)

// Text renders the value's current value in a <span> and keeps that span's
// text in sync with it in the browser.
//
// The span is owned by the marker, so the binding is stamped with a data-x id
// at the exact spot it was written — there is no separate file or selector to
// keep in sync. The value is HTML-escaped at build time.
//
// Outside a [Document] the span is rendered without an id and no binding is
// emitted.
func Text[T any](s Value[T]) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		value := templ.EscapeString(fmt.Sprint(s.Get()))
		if p := programFrom(ctx); p != nil {
			id := p.nodeID()
			if _, err := fmt.Fprintf(w, `<span data-x="%s">%s</span>`, id, value); err != nil {
				return err
			}
			p.text(id, s.state())
			return nil
		}
		_, err := fmt.Fprintf(w, `<span>%s</span>`, value)
		return err
	})
}

// Script renders a <script> element around its children, written as raw
// JavaScript. It is the arbitrary-JavaScript escape hatch: the body is emitted
// into the page as written, outside the reactive system.
//
//	@x.Script() {
//		console.log("hello");
//	}
//
// When the page is reactive the runtime and state declarations load before the
// body, so the script can read aw and the signal variables directly.
func Script() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, "<script>"); err != nil {
			return err
		}
		if err := templ.GetChildren(ctx).Render(ctx, w); err != nil {
			return err
		}
		_, err := io.WriteString(w, "</script>")
		return err
	})
}

// JS emits a raw <script> element passed in as an argument, for arbitrary
// JavaScript authored as a templ component rather than children:
//
//	templ logging() {
//		<script>console.log("hello");</script>
//	}
//
//	@x.JS(logging())
//
// It renders script verbatim. Like [Script], the script can read aw and the
// signal variables when the page is reactive. It is the same as rendering the
// component directly; the wrapper exists to name the escape hatch explicitly.
func JS(script templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if script == nil {
			return nil
		}
		return script.Render(ctx, w)
	})
}

// Effect registers a browser-side effect: it runs once on load, then again
// whenever a signal it reads changes. Dependencies are discovered by
// execution — the body must read signals by name, so use [Named]:
//
//	count := x.NamedNumSignal("count", 0)
//
//	templ titleScript() {
//		<script>
//			document.title = "Count: " + count.get();
//		</script>
//	}
//
//	@x.Effect(titleScript())
//
// The body is a <script> element, given either as an argument or as children:
//
//	@x.Effect() {
//		<script>
//			document.title = "Count: " + count.get();
//		</script>
//	}
//
// Writing the script in templ renders its content raw, so it needs no
// escaping or quoting. The effect is emitted after the page body, so the DOM
// it touches already exists; it renders nothing into the HTML.
func Effect(scripts ...templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		p := programFrom(ctx)
		if p == nil {
			return nil
		}
		body := templ.GetChildren(ctx)
		if len(scripts) > 0 {
			body = scripts[0]
		}
		var buf bytes.Buffer
		if err := body.Render(ctx, &buf); err != nil {
			return err
		}
		p.effect(scriptBody(buf.String()))
		return nil
	})
}

// scriptBody strips one wrapping <script> element from raw JavaScript.
func scriptBody(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<script") {
		if i := strings.Index(s, ">"); i >= 0 {
			s = s[i+1:]
		}
	}
	if strings.HasSuffix(s, "</script>") {
		s = s[:len(s)-len("</script>")]
	}
	return strings.TrimSpace(s)
}
