package x

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/a-h/templ"
)

// When renders subtree only while cond is true, mounting and unmounting it in
// the browser as cond changes. Unlike [Show], an unmounted subtree is gone
// from the DOM, and remounting rebuilds it — input focus and scroll position
// inside it do not survive.
//
// The subtree's HTML is captured at build time and embedded in the generated
// script, so it must be static: markers inside it render their static form and
// are not re-wired when it remounts. The subtree is wrapped in a container
// <div> that owns the mount point.
//
// Outside a [Document] it renders the subtree or nothing, statically.
func When(cond Value[bool], subtree templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		var buf bytes.Buffer
		if subtree != nil {
			if err := subtree.Render(context.WithValue(ctx, programKey{}, nil), &buf); err != nil {
				return err
			}
		}

		if p := programFrom(ctx); p != nil {
			id := p.nodeID()
			if cond.Get() {
				if _, err := fmt.Fprintf(w, `<div data-x="%s">%s</div>`, id, buf.String()); err != nil {
					return err
				}
			} else if _, err := fmt.Fprintf(w, `<div data-x="%s"></div>`, id); err != nil {
				return err
			}
			p.when(id, buf.String(), cond.state())
			return nil
		}

		if cond.Get() {
			_, err := w.Write(buf.Bytes())
			return err
		}
		return nil
	})
}

// List renders a signal of items as a keyed list: the current items become
// static HTML, and in the browser the list is kept in sync by moving existing
// rows, inserting new ones and removing gone ones — existing rows are never
// rebuilt, so input focus inside them survives.
//
// key names the JSON field each item is identified by. row renders one item's
// markup at build time; template names a global JavaScript function of the
// form (item) => html, used to render items that appear at runtime — items the
// build never saw. Without a template, only reordering and removal work.
//
// Each row is wrapped in a keyed <div>, and the list in a container <div>.
// Rows are static: markers inside row render their static form and are not
// re-wired. Outside a [Document] the list renders statically.
func List[T any](s *Signal[[]T], key string, row func(T) templ.Component, template string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		sub := context.WithValue(ctx, programKey{}, nil)

		var rows strings.Builder
		for _, item := range s.Get() {
			var buf bytes.Buffer
			if row != nil {
				if err := row(item).Render(sub, &buf); err != nil {
					return err
				}
			}
			k, err := fieldOf(item, key)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(&rows, `<div data-key="%s">%s</div>`, templ.EscapeString(k), buf.String()); err != nil {
				return err
			}
		}

		if p := programFrom(ctx); p != nil {
			id := p.nodeID()
			if _, err := fmt.Fprintf(w, `<div data-x="%s">%s</div>`, id, rows.String()); err != nil {
				return err
			}
			p.list(id, key, template, &s.id)
			return nil
		}

		_, err := io.WriteString(w, rows.String())
		return err
	})
}

// fieldOf reads one field of item by its JSON name, matching how the browser
// runtime reads the same key from the item's JSON representation.
func fieldOf[T any](item T, field string) (string, error) {
	b, err := json.Marshal(item)
	if err != nil {
		return "", err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return "", err
	}
	if v, ok := m[field]; ok {
		return fmt.Sprint(v), nil
	}
	return "", fmt.Errorf("x: no field %q in %T", field, item)
}
