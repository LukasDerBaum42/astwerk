package x

import (
	"context"
	"fmt"
	"io"

	"github.com/a-h/templ"
)

// Binding is a directive applied to an element [El] owns. Each binding renders
// its static contribution to the element's start tag, then records its
// reactive wiring against the element's node id.
//
// The set is closed: only the constructor functions in this package create
// bindings, so the mapping from binding to runtime call stays in one place.
type Binding interface {
	bindAttr(w io.Writer) error
	bindJS(p *program, id string)
}

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
//	@x.El("button", x.On("click", count.Set(count.Add(1)))) {
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
