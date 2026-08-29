package x

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/a-h/templ"
)

// Binding is a directive applied to an element [El] or [Wrap] owns. Each
// binding renders its static contribution to the element's start tag, then
// records its reactive wiring against the element's node id.
//
// The set is closed: only the constructor functions in this package create
// bindings, so the mapping from binding to runtime call stays in one place.
type Binding interface {
	bindAttr(w io.Writer) error
	bindJS(p *program, id string)
}

// HtmlAttr is a static attribute carried through verbatim, so an element keeps
// its class and id when it gains reactive bindings. Unlike [Attr] it is not
// reactive: the value is fixed at build time.
func HtmlAttr(name, value string) Binding { return htmlAttrBind{name, value} }

type htmlAttrBind struct {
	name  string
	value string
}

func (b htmlAttrBind) bindAttr(w io.Writer) error {
	_, err := fmt.Fprintf(w, ` %s="%s"`, b.name, templ.EscapeString(b.value))
	return err
}

func (htmlAttrBind) bindJS(*program, string) {}

// Attr binds a string attribute of the element. An empty value removes the
// attribute in the browser, which is what string-driven boolean attributes
// like "disabled" need.
func Attr(name string, s Value[string]) Binding { return attrBind{name, s} }

type attrBind struct {
	name string
	s    Value[string]
}

func (b attrBind) bindAttr(w io.Writer) error {
	_, err := fmt.Fprintf(w, ` %s="%s"`, b.name, templ.EscapeString(b.s.Get()))
	return err
}

func (b attrBind) bindJS(p *program, id string) { p.attr(id, b.name, b.s.state()) }

// Class toggles one class on the element.
func Class(name string, s Value[bool]) Binding { return classBind{name, s} }

type classBind struct {
	name string
	s    Value[bool]
}

func (b classBind) bindAttr(w io.Writer) error {
	if !b.s.Get() {
		return nil
	}
	_, err := fmt.Fprintf(w, ` class="%s"`, templ.EscapeString(b.name))
	return err
}

func (b classBind) bindJS(p *program, id string) { p.class(id, b.name, b.s.state()) }

// Disabled toggles the boolean disabled attribute.
func Disabled(s Value[bool]) Binding { return boolAttrBind{name: "disabled", s: s} }

type boolAttrBind struct {
	name string
	s    Value[bool]
}

func (b boolAttrBind) bindAttr(w io.Writer) error {
	if !b.s.Get() {
		return nil
	}
	_, err := fmt.Fprintf(w, ` %s`, b.name)
	return err
}

func (b boolAttrBind) bindJS(p *program, id string) { p.boolAttr(id, b.name, b.s.state()) }

// Show hides the element while the signal is false.
func Show(s Value[bool]) Binding { return showBind{s} }

type showBind struct{ s Value[bool] }

func (b showBind) bindAttr(w io.Writer) error {
	if b.s.Get() {
		return nil
	}
	_, err := io.WriteString(w, ` style="display:none"`)
	return err
}

func (b showBind) bindJS(p *program, id string) { p.show(id, b.s.state()) }

// Style binds one CSS property of the element. The value is applied with
// style.setProperty, so it cannot break out of the property into the rest of
// the stylesheet.
//
// The static form renders a style attribute with the value HTML-escaped; full
// CSS sanitization of untrusted values is out of scope for now.
func Style(prop string, s Value[string]) Binding { return styleBind{prop, s} }

type styleBind struct {
	prop string
	s    Value[string]
}

func (b styleBind) bindAttr(w io.Writer) error {
	if b.s.Get() == "" {
		return nil
	}
	_, err := fmt.Fprintf(w, ` style="%s: %s"`, b.prop, templ.EscapeString(b.s.Get()))
	return err
}

func (b styleBind) bindJS(p *program, id string) { p.style(id, b.prop, b.s.state()) }

// On binds an event handler to the element.
//
//	@x.El("button", x.On("click", count.Inc(1))) {
//		+
//	}
func On(event string, a Action) Binding { return onBind{event, a} }

type onBind struct {
	event  string
	action Action
}

func (onBind) bindAttr(io.Writer) error { return nil }

func (b onBind) bindJS(p *program, id string) { p.event(id, b.event, b.action) }

// El owns an element of any tag and applies bindings to it. It is the general
// form the convenience markers specialise, for elements that carry reactive
// attributes, styles or events rather than a value as text:
//
//	@x.El("a", x.Attr("href", url)) {
//		link text
//	}
//
// Children come from the templ context. Outside a [Document] the bindings
// render their static form and no wiring is emitted.
func El(tag string, bs ...Binding) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		p := programFrom(ctx)
		var id string
		if p != nil {
			id = p.nodeID()
			if _, err := fmt.Fprintf(w, `<%s data-x="%s"`, tag, id); err != nil {
				return err
			}
		} else if _, err := fmt.Fprintf(w, `<%s`, tag); err != nil {
			return err
		}
		for _, b := range bs {
			if err := b.bindAttr(w); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, `>`); err != nil {
			return err
		}
		if err := templ.GetChildren(ctx).Render(ctx, w); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, `</%s>`, tag); err != nil {
			return err
		}
		if p != nil {
			for _, b := range bs {
				b.bindJS(p, id)
			}
		}
		return nil
	})
}

// Wrap renders component, finds its outermost element, and applies bindings to
// that element. Bindings reach only the root — nothing inside is touched — and
// the component's own attributes and children are preserved. This is for
// components you already have that you want to make reactive without rewriting
// their markup:
//
//	templ PrimaryButton(label string) {
//		<button class="btn btn-primary">{ label }</button>
//	}
//
//	@x.Wrap(PrimaryButton("+"), x.On("click", count.Inc(1)))
//
// The component must have exactly one root element; a text-first or multi-root
// component fails loudly at build time. Markers inside the component still
// render live, so Wrap composes with inner markers. Outside a [Document] — or
// with no bindings — the component renders unchanged.
//
// Wrap uses a deliberately small scanner rather than an HTML parser: raw '<'
// inside a <script> or <style> body is not handled, and is the user's
// responsibility.
func Wrap(component templ.Component, bs ...Binding) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if component == nil {
			return nil
		}
		p := programFrom(ctx)
		if p == nil || len(bs) == 0 {
			return component.Render(ctx, w)
		}

		var buf bytes.Buffer
		if err := component.Render(ctx, &buf); err != nil {
			return err
		}
		doc := buf.String()

		prefix, tag, rest, err := firstElement(doc)
		if err != nil {
			return err
		}
		if err := singleRoot(rest, tag); err != nil {
			return err
		}

		id := p.nodeID()
		if _, err := io.WriteString(w, prefix); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, `<%s data-x="%s"`, tag, id); err != nil {
			return err
		}
		for _, b := range bs {
			if err := b.bindAttr(w); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, rest); err != nil {
			return err
		}
		for _, b := range bs {
			b.bindJS(p, id)
		}
		return nil
	})
}

// firstElement returns the content before the first element (leading text or
// whitespace), the element's tag name, and the rest of doc starting just after
// that tag name. It skips comments, doctypes and processing instructions, and
// fails loudly when the component has no element.
func firstElement(doc string) (prefix, tag, rest string, err error) {
	i := 0
	for {
		lt := strings.IndexByte(doc[i:], '<')
		if lt < 0 {
			return "", "", "", fmt.Errorf("x: Wrap: no element found in component")
		}
		lt += i
		after := lt + 1

		switch {
		case strings.HasPrefix(doc[after:], "!--"):
			if end := strings.Index(doc[after+3:], "-->"); end < 0 {
				return "", "", "", fmt.Errorf("x: Wrap: unterminated comment in component")
			} else {
				i = after + 3 + end + 3
			}
		case strings.HasPrefix(doc[after:], "!"), strings.HasPrefix(doc[after:], "?"):
			if end := strings.IndexByte(doc[after:], '>'); end < 0 {
				return "", "", "", fmt.Errorf("x: Wrap: unterminated declaration in component")
			} else {
				i = after + end + 1
			}
		case strings.HasPrefix(doc[after:], "/"):
			if end := strings.IndexByte(doc[after:], '>'); end < 0 {
				return "", "", "", fmt.Errorf("x: Wrap: unterminated tag in component")
			} else {
				i = after + end + 1
			}
		default:
			j := after
			for j < len(doc) && !isTagNameEnd(doc[j]) {
				j++
			}
			if j == after {
				return "", "", "", fmt.Errorf("x: Wrap: malformed element in component")
			}
			return doc[:lt], doc[after:j], doc[j:], nil
		}
	}
}

func isTagNameEnd(c byte) bool {
	return c == '>' || c == '/' || c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// singleRoot reports whether the component has exactly one root element: after
// the first element closes, only whitespace may remain.
func singleRoot(rest, tag string) error {
	end := startTagEnd(rest)
	if end < 0 {
		return fmt.Errorf("x: Wrap: unterminated element in component")
	}
	selfClosing := end >= 2 && rest[end-2] == '/'
	if selfClosing || isVoidElement(tag) {
		if strings.TrimSpace(rest[end:]) != "" {
			return fmt.Errorf("x: Wrap: component has multiple root elements")
		}
		return nil
	}

	depth := 1
	for i := end; i < len(rest); {
		switch rest[i] {
		case '\'', '"':
			i = skipQuoted(rest, i)
		case '<':
			switch {
			case strings.HasPrefix(rest[i:], "<!--"):
				if j := strings.Index(rest[i+4:], "-->"); j >= 0 {
					i += 4 + j + 3
				} else {
					i = len(rest)
				}
			case strings.HasPrefix(rest[i:], "</"):
				depth--
				if depth == 0 {
					j := strings.IndexByte(rest[i:], '>')
					if j < 0 {
						return nil // unterminated: give up, the component rendered fine
					}
					if strings.TrimSpace(rest[i+j+1:]) != "" {
						return fmt.Errorf("x: Wrap: component has multiple root elements")
					}
					return nil
				}
				if j := strings.IndexByte(rest[i:], '>'); j >= 0 {
					i += j + 1
				} else {
					i = len(rest)
				}
			default:
				j := strings.IndexByte(rest[i:], '>')
				if j < 0 {
					i = len(rest)
					continue
				}
				inner := rest[i+1 : i+j]
				if !selfClosingTag(inner) && !isVoidElement(tagName(inner)) {
					depth++
				}
				i += j + 1
			}
		default:
			i++
		}
	}
	return nil
}

// startTagEnd returns the index just after the '>' closing a start tag,
// honoring quoted attribute values, or -1 if none is found.
func startTagEnd(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'', '"':
			i = skipQuoted(s, i) - 1
		case '>':
			return i + 1
		}
	}
	return -1
}

func skipQuoted(s string, i int) int {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		if s[j] == q {
			return j + 1
		}
	}
	return len(s)
}

func tagName(inner string) string {
	for i := 0; i < len(inner); i++ {
		if isTagNameEnd(inner[i]) {
			return inner[:i]
		}
	}
	return inner
}

func selfClosingTag(inner string) bool {
	return strings.HasSuffix(inner, "/")
}

var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

func isVoidElement(tag string) bool { return voidElements[tag] }
