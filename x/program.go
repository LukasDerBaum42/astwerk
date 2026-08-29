package x

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/a-h/templ"
)

// program is the build context for one page: the JavaScript the page's markers
// have requested, plus the bookkeeping needed to render it. It is threaded
// through the templ context by [Document], so markers can reach it from
// wherever they render without a package global.
type program struct {
	nodes int
	anon  int
	min   bool
	names map[*state]string
	taken map[string]bool
	order []*state
	stmts []func(*program, *strings.Builder)
	feat  map[string]bool
	named map[*state]bool
}

func newProgram() *program {
	return &program{
		names: map[*state]string{},
		taken: map[string]bool{},
		feat:  map[string]bool{},
		named: map[*state]bool{},
	}
}

// use pulls a runtime feature into the page, with its dependencies.
func (p *program) use(key string) {
	if p.feat[key] {
		return
	}
	p.feat[key] = true
	switch key {
	case "effect":
		p.use("track")
	}
}

// nodeID assigns the next DOM node id.
func (p *program) nodeID() string {
	id := p.nodes
	p.nodes++
	return strconv.Itoa(id)
}

// name returns the JavaScript variable for s, assigning one and queuing its
// declaration on first use. A memo's dependencies are named first, so a
// computed value is always declared after the signals it reads. An explicit
// valid name on s wins; otherwise an s0, s1... name is assigned.
func (p *program) name(s *state) string {
	if n, ok := p.names[s]; ok {
		return n
	}
	if s.expr != nil {
		p.use("computed")
		for _, d := range s.expr.deps() {
			p.name(d)
		}
	}
	n := s.name
	if n == "" || !validIdent(n) || reservedNames[n] || p.taken[n] {
		for {
			n = "s" + strconv.Itoa(p.anon)
			p.anon++
			if !p.taken[n] {
				break
			}
		}
	} else {
		// The explicit name survived: it is stable API, so record it in the
		// bridge's registry. Assigned names are not — they are build internals.
		p.named[s] = true
		p.use("register")
	}
	p.taken[n] = true
	p.names[s] = n
	p.order = append(p.order, s)
	return n
}

// text records a text binding: node id shows the signal's value.
func (p *program) text(id string, s *state) {
	p.use("text")
	p.name(s)
	p.stmts = append(p.stmts, func(p *program, b *strings.Builder) {
		b.WriteString("aw.text(" + jsLiteral(id) + ", " + p.name(s) + ");")
	})
}

// event records an event binding: node id runs the action when event fires.
func (p *program) event(id, event string, a Action) {
	p.use("on")
	for _, s := range a.deps() {
		p.name(s)
	}
	p.stmts = append(p.stmts, func(p *program, b *strings.Builder) {
		b.WriteString("aw.on(" + jsLiteral(id) + ", " + jsLiteral(event) + ", () => { ")
		a.render(p, b)
		b.WriteString("; });")
	})
}

// attr records a string-attribute binding; an empty value removes it.
func (p *program) attr(id, name string, s *state) {
	p.use("attr")
	p.name(s)
	p.stmts = append(p.stmts, func(p *program, b *strings.Builder) {
		b.WriteString("aw.attr(" + jsLiteral(id) + ", " + jsLiteral(name) + ", " + p.name(s) + ");")
	})
}

// boolAttr records a boolean-attribute binding (present when true).
func (p *program) boolAttr(id, name string, s *state) {
	p.use("boolAttr")
	p.name(s)
	p.stmts = append(p.stmts, func(p *program, b *strings.Builder) {
		b.WriteString("aw.boolAttr(" + jsLiteral(id) + ", " + jsLiteral(name) + ", " + p.name(s) + ");")
	})
}

// class records a single-class toggle binding.
func (p *program) class(id, name string, s *state) {
	p.use("cls")
	p.name(s)
	p.stmts = append(p.stmts, func(p *program, b *strings.Builder) {
		b.WriteString("aw.cls(" + jsLiteral(id) + ", " + jsLiteral(name) + ", " + p.name(s) + ");")
	})
}

// show records a visibility binding.
func (p *program) show(id string, s *state) {
	p.use("show")
	p.name(s)
	p.stmts = append(p.stmts, func(p *program, b *strings.Builder) {
		b.WriteString("aw.show(" + jsLiteral(id) + ", " + p.name(s) + ");")
	})
}

// value records a two-way string-input binding.
func (p *program) value(id string, s *state) {
	p.use("value")
	p.name(s)
	p.stmts = append(p.stmts, func(p *program, b *strings.Builder) {
		b.WriteString("aw.value(" + jsLiteral(id) + ", " + p.name(s) + ");")
	})
}

// number records a two-way numeric-input binding.
func (p *program) number(id string, s *state) {
	p.use("number")
	p.name(s)
	p.stmts = append(p.stmts, func(p *program, b *strings.Builder) {
		b.WriteString("aw.number(" + jsLiteral(id) + ", " + p.name(s) + ");")
	})
}

// checked records a two-way checkbox binding.
func (p *program) checked(id string, s *state) {
	p.use("checked")
	p.name(s)
	p.stmts = append(p.stmts, func(p *program, b *strings.Builder) {
		b.WriteString("aw.checked(" + jsLiteral(id) + ", " + p.name(s) + ");")
	})
}

// style records a single-CSS-property binding.
func (p *program) style(id, prop string, s *state) {
	p.use("style")
	p.name(s)
	p.stmts = append(p.stmts, func(p *program, b *strings.Builder) {
		b.WriteString("aw.style(" + jsLiteral(id) + ", " + jsLiteral(prop) + ", " + p.name(s) + ");")
	})
}

// effect records a computation that re-runs when the signals it reads change.
func (p *program) effect(body string) {
	p.use("effect")
	p.stmts = append(p.stmts, func(p *program, b *strings.Builder) {
		b.WriteString("aw.effect(() => { " + body + " });")
	})
}

// when records a conditional subtree that mounts and unmounts.
func (p *program) when(id, html string, s *state) {
	p.use("when")
	p.name(s)
	p.stmts = append(p.stmts, func(p *program, b *strings.Builder) {
		b.WriteString("aw.when(" + jsLiteral(id) + ", " + jsLiteral(html) + ", " + p.name(s) + ");")
	})
}

// list records a keyed list over a signal of items.
func (p *program) list(id, key, template string, s *state) {
	p.use("list")
	p.name(s)
	tmpl := "null"
	if template != "" && validIdent(template) && !reservedNames[template] {
		tmpl = template
	}
	p.stmts = append(p.stmts, func(p *program, b *strings.Builder) {
		b.WriteString("aw.list(" + jsLiteral(id) + ", " + p.name(s) + ", " + jsLiteral(key) + ", " + tmpl + ");")
	})
}

// header renders the runtime and state declarations, which must load before
// the page body so any <script> in it can read them. It is "" when the page
// has no reactivity. The runtime is assembled from fragments, so a page
// carries only the pieces it uses; with [Minified] it is emitted compact.
func (p *program) header() string {
	if len(p.stmts) == 0 {
		return ""
	}
	var rt strings.Builder
	rt.WriteString(runtimeCore)
	exports := []string{"signal"}
	for _, f := range runtimeFeatures {
		if !p.feat[f.key] {
			continue
		}
		rt.WriteString(f.code)
		if f.export != "" {
			exports = append(exports, f.export)
		}
	}
	if p.feat["register"] {
		// register is generated-code-only, so it is exported next to state but
		// is not part of the public aw surface a script should call.
		exports = append(exports, "register")
	}
	rt.WriteString("\tglobalThis.aw = { " + strings.Join(exports, ", ") + " };\n})();")
	code := rt.String()
	if p.min {
		code = minifyJS(code)
	}

	var b strings.Builder
	b.WriteString("<script>\n")
	b.WriteString(code)
	b.WriteString("\n\n")
	for _, s := range p.order {
		b.WriteString("const " + p.name(s) + " = ")
		if s.expr != nil {
			// The memo's dependencies are resolved at build time — the
			// expression lists them — so the runtime needs no tracking here.
			b.WriteString("aw.computed([")
			for i, d := range s.expr.deps() {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(p.name(d))
			}
			b.WriteString("], () => ")
			s.expr.render(p, &b)
			b.WriteString(");\n")
		} else {
			b.WriteString("aw.signal(" + jsLiteral(s.init) + ");\n")
		}
		if p.named[s] {
			b.WriteString("aw.register(" + jsLiteral(s.name) + ", " + p.name(s) + ");\n")
		}
	}
	b.WriteString("</script>\n")
	return b.String()
}

// footer renders the bindings, which must load after the body so the DOM nodes
// they target exist. It is "" when the page has no reactivity.
func (p *program) footer() string {
	if len(p.stmts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<script>\n")
	for _, st := range p.stmts {
		st(p, &b)
		b.WriteString("\n")
	}
	b.WriteString("</script>\n")
	return b.String()
}

type programKey struct{}

func programFrom(ctx context.Context) *program {
	if ctx == nil {
		return nil
	}
	p, _ := ctx.Value(programKey{}).(*program)
	return p
}

// Option tailors a [Document]'s output.
type Option func(*options)

type options struct {
	min bool
}

// Minified opts into production output: the runtime is emitted as compact,
// unindented JavaScript rather than the readable development form. Generated
// statements and declarations are unaffected.
func Minified() Option { return func(o *options) { o.min = true } }

// Document renders body with a fresh program on the context, emitting the
// runtime and state declarations before it and the bindings after it. It is
// the entry point for a reactive page.
//
//	Page: func(c ssg.Ctx) templ.Component {
//		return x.Document(src.Counter())
//	}
//
// The split matters: declarations load first so any <script> in the page can
// read reactive state, and bindings load last so the DOM nodes they target
// exist. Top-level const declarations in one script are visible to later
// scripts, so a script in the body can use aw and the signal variables
// directly.
//
// Markers inside body read the program from the context; rendered outside a
// Document they fall back to static HTML. A page with no markers emits no
// script at all.
//
// There is one signal namespace and one node-id space per page, owned by the
// single outermost Document: every marker on the page draws from the same
// counters, so reactive regions ("islands") never collide. A Document nested
// inside another renders its body inline and joins the outer program rather
// than declaring its own s0, s1... again.
//
// body is rendered into a buffer first, so the declarations can be discovered
// before anything is written. Pass [Minified] for production output.
func Document(body templ.Component, opts ...Option) templ.Component {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if programFrom(ctx) != nil {
			return body.Render(ctx, w)
		}

		p := newProgram()
		p.min = o.min
		ctx = context.WithValue(ctx, programKey{}, p)

		var buf bytes.Buffer
		if err := body.Render(ctx, &buf); err != nil {
			return err
		}

		if head := p.header(); head != "" {
			if _, err := io.WriteString(w, head); err != nil {
				return err
			}
		}
		if _, err := w.Write(buf.Bytes()); err != nil {
			return err
		}
		if foot := p.footer(); foot != "" {
			if _, err := io.WriteString(w, foot); err != nil {
				return err
			}
		}
		return nil
	})
}
