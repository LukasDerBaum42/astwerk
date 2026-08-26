package x

import (
	"encoding/json"
	"strings"
)

type exprKind int

const (
	exprLiteral exprKind = iota
	exprGet
	exprAdd
	exprSub
	exprMul
	exprDiv
	exprNot
)

var exprOp = map[exprKind]string{
	exprAdd: "+",
	exprSub: "-",
	exprMul: "*",
	exprDiv: "/",
}

// expr is the type-erased half of an [Expr]: it renders to JavaScript without
// knowing the value's type. The typed [Expr] carries the build-time value
// alongside it, which is how [Computed] can derive a [Memo] without a
// transpiler.
type expr struct {
	kind exprKind
	a, b *expr
	sig  *state
	lit  string // JSON-encoded literal, for exprLiteral
}

// Expr is a JavaScript expression together with its build-time value, built by
// the combinators on [Signal]. Literals are escaped at construction, so
// composition never concatenates raw user data.
type Expr[T any] struct {
	e *expr
	v T
}

func literal[T any](v T) Expr[T] {
	return Expr[T]{e: &expr{kind: exprLiteral, lit: jsLiteral(v)}, v: v}
}

func getExpr[T any](s *state, v T) Expr[T] {
	return Expr[T]{e: &expr{kind: exprGet, sig: s}, v: v}
}

// bin builds an arithmetic expression, evaluating its build-time value as well
// as rendering it.
func bin[T any](kind exprKind, a, b Expr[T]) Expr[T] {
	return Expr[T]{
		e: &expr{kind: kind, a: a.e, b: b.e},
		v: arith(exprOp[kind], a.v, b.v).(T),
	}
}

// deps returns the states the expression reads, in first-use order.
func (e *expr) deps() []*state {
	seen := map[*state]bool{}
	var out []*state
	e.collect(seen, &out)
	return out
}

func (e *expr) collect(seen map[*state]bool, out *[]*state) {
	if e == nil {
		return
	}
	switch e.kind {
	case exprGet:
		if !seen[e.sig] {
			seen[e.sig] = true
			*out = append(*out, e.sig)
		}
	case exprNot:
		e.a.collect(seen, out)
	case exprAdd, exprSub, exprMul, exprDiv:
		e.a.collect(seen, out)
		e.b.collect(seen, out)
	}
}

func (e *expr) render(p *program, b *strings.Builder) {
	switch e.kind {
	case exprLiteral:
		b.WriteString(e.lit)
	case exprGet:
		b.WriteString(p.name(e.sig) + ".get()")
	case exprNot:
		b.WriteString("!")
		e.a.render(p, b)
	case exprAdd, exprSub, exprMul, exprDiv:
		e.a.render(p, b)
		b.WriteString(" " + exprOp[e.kind] + " ")
		e.b.render(p, b)
	}
}

// Action is one client-side operation, built at build time and rendered to
// JavaScript once the page is finalised.
type Action struct {
	kind actionKind
	sig  *state
	val  *expr
}

type actionKind int

const actionSet actionKind = iota

// deps returns the states the action reads or writes, in deterministic order.
func (a Action) deps() []*state {
	out := a.val.deps()
	if a.sig != nil {
		for _, d := range out {
			if d == a.sig {
				return out
			}
		}
		out = append([]*state{a.sig}, out...)
	}
	return out
}

func (a Action) render(p *program, b *strings.Builder) {
	b.WriteString(p.name(a.sig) + ".set(")
	a.val.render(p, b)
	b.WriteString(")")
}

// arith applies a numeric or string operator to two build-time values. It
// supports int, int64, float64 and (for +) string, which is the set the
// combinator set targets; anything else yields nil.
func arith(op string, a, b any) any {
	switch x := a.(type) {
	case int:
		y := b.(int)
		switch op {
		case "+":
			return x + y
		case "-":
			return x - y
		case "*":
			return x * y
		case "/":
			return x / y
		}
	case int64:
		y := b.(int64)
		switch op {
		case "+":
			return x + y
		case "-":
			return x - y
		case "*":
			return x * y
		case "/":
			return x / y
		}
	case float64:
		y := b.(float64)
		switch op {
		case "+":
			return x + y
		case "-":
			return x - y
		case "*":
			return x * y
		case "/":
			return x / y
		}
	case string:
		if op == "+" {
			return x + b.(string)
		}
	}
	return nil
}

// jsLiteral renders a Go value as a JavaScript literal. JSON is a valid
// JavaScript literal for every value it can encode, so this is the single
// place user data enters generated code — and it is escaped here.
func jsLiteral(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}
