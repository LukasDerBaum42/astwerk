package x

import (
	"encoding/json"
	"fmt"
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
	exprGt
	exprLt
	exprGte
	exprLte
	exprEq
	exprNe
)

var exprOp = map[exprKind]string{
	exprAdd: "+",
	exprSub: "-",
	exprMul: "*",
	exprDiv: "/",
	exprGt:  ">",
	exprLt:  "<",
	exprGte: ">=",
	exprLte: "<=",
	exprEq:  "===",
	exprNe:  "!==",
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
// the combinators in this package. Literals are escaped at construction, so
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

// Lit returns an expression for a constant, for setting a signal to a fixed
// value:
//
//	@x.El("button", x.On("click", count.SetTo(0))) {
//		reset
//	}
func Lit[T any](v T) Expr[T] { return literal(v) }

// Not returns an expression for the negation of v.
func Not(v Value[bool]) Expr[bool] {
	return Expr[bool]{e: &expr{kind: exprNot, a: v.Ref().e}, v: !v.Get()}
}

// num is the numeric constraint shared by the arithmetic operators.
type num interface{ int | int64 | float64 }

// ordered is what the comparisons accept: numbers and strings.
type ordered interface{ int | int64 | float64 | string }

// Add returns an expression for a + b. b is a literal, another value of the
// same type, or an expression — see [operand].
func Add[T num](a Value[T], b any) Expr[T] { return bin(exprAdd, a.Ref(), operand(a, b)) }

// Sub returns an expression for a - b.
func Sub[T num](a Value[T], b any) Expr[T] { return bin(exprSub, a.Ref(), operand(a, b)) }

// Mul returns an expression for a * b.
func Mul[T num](a Value[T], b any) Expr[T] { return bin(exprMul, a.Ref(), operand(a, b)) }

// Div returns an expression for a / b.
func Div[T num](a Value[T], b any) Expr[T] { return bin(exprDiv, a.Ref(), operand(a, b)) }

// Concat returns an expression for a + b on strings. Strings have their own
// combinator so [Add] stays strictly numeric.
func Concat(a Value[string], b any) Expr[string] {
	return bin(exprAdd, a.Ref(), operand(a, b))
}

// Gt returns an expression for a > b, true or false in the browser.
func Gt[T ordered](a Value[T], b any) Expr[bool] { return cmp(exprGt, a.Ref(), operand(a, b)) }

// Lt returns an expression for a < b.
func Lt[T ordered](a Value[T], b any) Expr[bool] { return cmp(exprLt, a.Ref(), operand(a, b)) }

// Gte returns an expression for a >= b.
func Gte[T ordered](a Value[T], b any) Expr[bool] { return cmp(exprGte, a.Ref(), operand(a, b)) }

// Lte returns an expression for a <= b.
func Lte[T ordered](a Value[T], b any) Expr[bool] { return cmp(exprLte, a.Ref(), operand(a, b)) }

// Eq returns an expression for a === b.
func Eq[T comparable](a Value[T], b any) Expr[bool] { return cmp(exprEq, a.Ref(), operand(a, b)) }

// Ne returns an expression for a !== b.
func Ne[T comparable](a Value[T], b any) Expr[bool] { return cmp(exprNe, a.Ref(), operand(a, b)) }

// bin builds an arithmetic expression, evaluating its build-time value as well
// as rendering it.
func bin[T any](kind exprKind, a, b Expr[T]) Expr[T] {
	return Expr[T]{
		e: &expr{kind: kind, a: a.e, b: b.e},
		v: arith(exprOp[kind], a.v, b.v).(T),
	}
}

// cmp builds a comparison expression, evaluating its build-time value.
func cmp[T comparable](kind exprKind, a, b Expr[T]) Expr[bool] {
	return Expr[bool]{
		e: &expr{kind: kind, a: a.e, b: b.e},
		v: compare(exprOp[kind], a.v, b.v),
	}
}

// operand turns the second argument of a combinator into an expression: a
// literal of T, another Value[T], or an Expr[T]. Anything else — a value of a
// different type, an expression of a different type, or a non-operand — is a
// build-time error. The stored [Value.Type] is what makes the check possible.
func operand[T any](a Value[T], b any) Expr[T] {
	if v, ok := b.(Value[T]); ok {
		return v.Ref()
	}
	if e, ok := b.(Expr[T]); ok {
		return e
	}
	if v, ok := b.(T); ok {
		return literal(v)
	}
	panic(fmt.Errorf("x: %T: operand %T does not match value type %v", a, b, a.Type()))
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
	case exprAdd, exprSub, exprMul, exprDiv, exprGt, exprLt, exprGte, exprLte, exprEq, exprNe:
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
	case exprAdd, exprSub, exprMul, exprDiv, exprGt, exprLt, exprGte, exprLte, exprEq, exprNe:
		e.a.render(p, b)
		b.WriteString(" " + exprOp[e.kind] + " ")
		e.b.render(p, b)
	}
}

// Action is one or more client-side operations, built at build time and
// rendered to JavaScript once the page is finalised. [Seq] and [If] compose
// the single [signal set] actions the signal mutators produce.
type Action struct {
	kind actionKind
	sig  *state
	val  *expr
	seq  []Action
	cond *state
	then *Action
	els  *Action
}

type actionKind int

const (
	actionSet actionKind = iota
	actionSeq
	actionIf
)

// Seq runs its actions in order, rendered as statements inside the handler.
func Seq(actions ...Action) Action { return Action{kind: actionSeq, seq: actions} }

// If runs then when cond is true in the browser, else when given.
func If(cond Value[bool], then Action, els ...Action) Action {
	a := Action{kind: actionIf, cond: cond.state(), then: &then}
	if len(els) > 0 {
		a.els = &els[0]
	}
	return a
}

// deps returns the states the action reads or writes, in deterministic order.
func (a Action) deps() []*state {
	switch a.kind {
	case actionSeq:
		groups := make([][]*state, len(a.seq))
		for i, sub := range a.seq {
			groups[i] = sub.deps()
		}
		return mergeDeps(groups...)
	case actionIf:
		var thenDeps, elsDeps []*state
		if a.then != nil {
			thenDeps = a.then.deps()
		}
		if a.els != nil {
			elsDeps = a.els.deps()
		}
		return mergeDeps([]*state{a.cond}, thenDeps, elsDeps)
	default:
		return mergeDeps([]*state{a.sig}, a.val.deps())
	}
}

// mergeDeps concatenates dependency lists in order, dropping duplicates so the
// first use of each state keeps its position.
func mergeDeps(groups ...[]*state) []*state {
	seen := map[*state]bool{}
	var out []*state
	for _, g := range groups {
		for _, s := range g {
			if s == nil || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func (a Action) render(p *program, b *strings.Builder) {
	switch a.kind {
	case actionSeq:
		for i, sub := range a.seq {
			if i > 0 {
				b.WriteString("; ")
			}
			sub.render(p, b)
		}
	case actionIf:
		b.WriteString("if (" + p.name(a.cond) + ".get()) { ")
		if a.then != nil {
			a.then.render(p, b)
		}
		b.WriteString(" }")
		if a.els != nil {
			b.WriteString(" else { ")
			a.els.render(p, b)
			b.WriteString(" }")
		}
	default:
		b.WriteString(p.name(a.sig) + ".set(")
		a.val.render(p, b)
		b.WriteString(")")
	}
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

// compare applies an ordered or equality operator to two build-time values of
// the same type. It supports int, int64, float64 and string for ordering, plus
// bool for equality — the scalar set the comparison combinators target.
// Anything else is a build-time error rather than a silently wrong static
// value.
func compare(op string, a, b any) bool {
	switch x := a.(type) {
	case int:
		y := b.(int)
		switch op {
		case ">":
			return x > y
		case "<":
			return x < y
		case ">=":
			return x >= y
		case "<=":
			return x <= y
		case "===":
			return x == y
		case "!==":
			return x != y
		}
	case int64:
		y := b.(int64)
		switch op {
		case ">":
			return x > y
		case "<":
			return x < y
		case ">=":
			return x >= y
		case "<=":
			return x <= y
		case "===":
			return x == y
		case "!==":
			return x != y
		}
	case float64:
		y := b.(float64)
		switch op {
		case ">":
			return x > y
		case "<":
			return x < y
		case ">=":
			return x >= y
		case "<=":
			return x <= y
		case "===":
			return x == y
		case "!==":
			return x != y
		}
	case string:
		y := b.(string)
		switch op {
		case ">":
			return x > y
		case "<":
			return x < y
		case ">=":
			return x >= y
		case "<=":
			return x <= y
		case "===":
			return x == y
		case "!==":
			return x != y
		}
	case bool:
		y := b.(bool)
		switch op {
		case "===":
			return x == y
		case "!==":
			return x != y
		}
	}
	panic(fmt.Errorf("x: cannot compare %T values at build time", a))
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
