package x

import (
	"context"
	"fmt"
	"io"

	"github.com/a-h/templ"
)

// Input renders a text input two-way bound to s: typing writes the signal,
// setting the signal updates the field.
func (s *StrSignal) Input() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		value := templ.EscapeString(s.Get())
		if p := programFrom(ctx); p != nil {
			id := p.nodeID()
			if _, err := fmt.Fprintf(w, `<input data-x="%s" value="%s">`, id, value); err != nil {
				return err
			}
			p.value(id, s.state())
			return nil
		}
		_, err := fmt.Fprintf(w, `<input value="%s">`, value)
		return err
	})
}

// Input is the package-function form of [StrSignal.Input].
func Input(s *StrSignal) templ.Component { return s.Input() }

// Number renders a number input two-way bound to s. Input that is not a number
// is ignored rather than clobbering the signal with zero.
func (s *NumSignal[T]) Number() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		value := templ.EscapeString(fmt.Sprint(s.Get()))
		if p := programFrom(ctx); p != nil {
			id := p.nodeID()
			if _, err := fmt.Fprintf(w, `<input type="number" data-x="%s" value="%s">`, id, value); err != nil {
				return err
			}
			p.number(id, s.state())
			return nil
		}
		_, err := fmt.Fprintf(w, `<input type="number" value="%s">`, value)
		return err
	})
}

// Number is the package-function form of [NumSignal.Number].
func Number[T int | int64 | float64](s *NumSignal[T]) templ.Component { return s.Number() }

// Checkbox renders a checkbox two-way bound to s.
func (s *BoolSignal) Checkbox() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		checked := ""
		if s.Get() {
			checked = " checked"
		}
		if p := programFrom(ctx); p != nil {
			id := p.nodeID()
			if _, err := fmt.Fprintf(w, `<input type="checkbox" data-x="%s"%s>`, id, checked); err != nil {
				return err
			}
			p.checked(id, s.state())
			return nil
		}
		_, err := fmt.Fprintf(w, `<input type="checkbox"%s>`, checked)
		return err
	})
}

// Checkbox is the package-function form of [BoolSignal.Checkbox].
func Checkbox(s *BoolSignal) templ.Component { return s.Checkbox() }
