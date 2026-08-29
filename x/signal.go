package x

import (
	"context"
	"io"
	"reflect"

	"github.com/a-h/templ"
)

// state is the identity half of a reactive value: the part the generated
// program tracks, so the program does not have to be generic. It carries the
// build-time value, the static type, and — for a [Memo] — the expression the
// value is derived from.
type state struct {
	name string       // explicit JavaScript name, or "" for an assigned one
	init any          // build-time value
	expr *expr        // non-nil for a Memo
	typ  reflect.Type // the static type, captured once at construction
}

// newState builds a state carrying the static type of T, which is what lets
// the expression combinators type-check their `any` operands and lets callers
// ask a value what type it holds without knowing T.
func newState[T any](name string, init T, e *expr) *state {
	return &state{name: name, init: init, expr: e, typ: reflect.TypeOf((*T)(nil)).Elem()}
}

// Value is a reactive value the program can bind to: a signal or a [Memo]. It
// is sealed — only this package produces values — and it satisfies
// [templ.Component], so a value renders as its bound text span when used bare
// in a templ component: @count and @count.Text() are the same thing.
type Value[T any] interface {
	Get() T
	Ref() Expr[T]
	Type() reflect.Type
	Text() templ.Component
	Render(ctx context.Context, w io.Writer) error
	state() *state // unexported — sealing
}

// State is the erased view of any signal or memo: what you can ask about
// without knowing T. Every value satisfies it, so a function can hold a signal
// of unknown type and act on its [Value.Type].
type State interface{ Type() reflect.Type }

// core is the shared signal body: identity (state) plus the current value.
// Every signal kind embeds it and adds only the methods meaningful for its
// value type.
type core[T any] struct {
	id state
	v  T
}

func (c *core[T]) Get() T                { return c.v }
func (c *core[T]) Ref() Expr[T]          { return getExpr(&c.id, c.v) }
func (c *core[T]) Type() reflect.Type    { return c.id.typ }
func (c *core[T]) state() *state         { return &c.id }
func (c *core[T]) Text() templ.Component { return Text(c) }
func (c *core[T]) Render(ctx context.Context, w io.Writer) error {
	return Text(c).Render(ctx, w)
}

// NumSignal holds a number: int, int64 or float64.
type NumSignal[T int | int64 | float64] struct{ core[T] }

// NewNumSignal returns a number signal holding v, under an assigned JavaScript
// name. Use [NamedNumSignal] when hand-written scripts or effects need to
// reference it.
func NewNumSignal[T int | int64 | float64](v T) *NumSignal[T] {
	return NamedNumSignal("", v)
}

// NamedNumSignal returns a number signal holding v under the explicit
// JavaScript name name. See [validIdent] for what a usable name looks like.
func NamedNumSignal[T int | int64 | float64](name string, v T) *NumSignal[T] {
	return &NumSignal[T]{core: core[T]{id: *newState(name, v, nil), v: v}}
}

// BoolSignal holds a bool.
type BoolSignal struct{ core[bool] }

// NewBoolSignal returns a bool signal holding v, under an assigned JavaScript
// name. Use [NamedBoolSignal] when hand-written scripts or effects need to
// reference it.
func NewBoolSignal(v bool) *BoolSignal { return NamedBoolSignal("", v) }

// NamedBoolSignal returns a bool signal holding v under the explicit
// JavaScript name name. See [validIdent] for what a usable name looks like.
func NamedBoolSignal(name string, v bool) *BoolSignal {
	return &BoolSignal{core: core[bool]{id: *newState(name, v, nil), v: v}}
}

// StrSignal holds a string.
type StrSignal struct{ core[string] }

// NewStrSignal returns a string signal holding v, under an assigned JavaScript
// name. Use [NamedStrSignal] when hand-written scripts or effects need to
// reference it.
func NewStrSignal(v string) *StrSignal { return NamedStrSignal("", v) }

// NamedStrSignal returns a string signal holding v under the explicit
// JavaScript name name. See [validIdent] for what a usable name looks like.
func NamedStrSignal(name string, v string) *StrSignal {
	return &StrSignal{core: core[string]{id: *newState(name, v, nil), v: v}}
}

// ListSignal holds a slice of items, for a keyed list.
type ListSignal[T any] struct{ core[[]T] }

// NewListSignal returns a list signal holding v, under an assigned JavaScript
// name. Use [NamedListSignal] when hand-written scripts or effects need to
// reference it.
func NewListSignal[T any](v []T) *ListSignal[T] { return NamedListSignal("", v) }

// NamedListSignal returns a list signal holding v under the explicit
// JavaScript name name. See [validIdent] for what a usable name looks like.
func NamedListSignal[T any](name string, v []T) *ListSignal[T] {
	return &ListSignal[T]{core: core[[]T]{id: *newState(name, v, nil), v: v}}
}

// Set returns an action that stores e into the signal in the browser. It is
// the general form; [NumSignal.SetTo], [NumSignal.Inc] and [NumSignal.Dec] are
// the one-word forms for the common cases.
func (s *NumSignal[T]) Set(e Expr[T]) Action {
	return Action{kind: actionSet, sig: &s.id, val: e.e}
}

// SetTo returns an action that stores the literal v into the signal.
func (s *NumSignal[T]) SetTo(v T) Action { return s.Set(Lit(v)) }

// Inc returns an action that adds n to the signal. n is a literal or another
// value of the same type — count.Inc(1) or count.Inc(step) — so the step
// itself can be reactive.
func (s *NumSignal[T]) Inc(n any) Action { return s.Set(Add(s, n)) }

// Dec returns an action that subtracts n from the signal, with the same
// literal-or-signal amount rule as [NumSignal.Inc].
func (s *NumSignal[T]) Dec(n any) Action { return s.Set(Sub(s, n)) }

// Set returns an action that stores e into the signal in the browser. It is
// the general form; [BoolSignal.SetTo] and [BoolSignal.Toggle] are the
// one-word forms for the common cases.
func (s *BoolSignal) Set(e Expr[bool]) Action {
	return Action{kind: actionSet, sig: &s.id, val: e.e}
}

// SetTo returns an action that stores the literal v into the signal.
func (s *BoolSignal) SetTo(v bool) Action { return s.Set(Lit(v)) }

// Toggle returns an action that flips the signal.
func (s *BoolSignal) Toggle() Action { return s.Set(Not(s)) }

// Set returns an action that stores e into the signal in the browser. It is
// the general form; [StrSignal.SetTo] is the one-word form for the common
// case.
func (s *StrSignal) Set(e Expr[string]) Action {
	return Action{kind: actionSet, sig: &s.id, val: e.e}
}

// SetTo returns an action that stores the literal v into the signal.
func (s *StrSignal) SetTo(v string) Action { return s.Set(Lit(v)) }

// Set returns an action that stores e into the signal in the browser. It is
// the general form; [ListSignal.SetTo] is the one-word form for the common
// case.
func (s *ListSignal[T]) Set(e Expr[[]T]) Action {
	return Action{kind: actionSet, sig: &s.id, val: e.e}
}

// SetTo returns an action that stores the literal v into the signal.
func (s *ListSignal[T]) SetTo(v []T) Action { return s.Set(Lit(v)) }

// Memo is a value derived from other reactive values, recomputed in the
// browser whenever a signal it reads changes. It is read-only: there are no
// mutators.
type Memo[T any] struct{ core[T] }

// Computed returns a [Memo] whose value is e, recomputed in the browser
// whenever a signal e reads changes. e is evaluated at build time to supply
// the static value.
//
//	price := x.NewNumSignal(10)
//	qty := x.NewNumSignal(2)
//	total := x.Computed(x.Mul(price, qty))
func Computed[T any](e Expr[T]) *Memo[T] {
	return &Memo[T]{core: core[T]{id: *newState("", e.v, e.e), v: e.v}}
}

// validIdent reports whether s is a usable JavaScript variable name.
func validIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		alpha := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '$'
		digit := c >= '0' && c <= '9'
		if !alpha && !(digit && i > 0) {
			return false
		}
	}
	return true
}

// reservedNames are identifiers a signal must not take, so generated
// declarations cannot shadow the runtime or globals it relies on.
var reservedNames = map[string]bool{
	"aw": true, "globalThis": true, "document": true, "window": true,
	"class": true, "function": true, "var": true, "let": true, "const": true,
	"for": true, "while": true, "if": true, "else": true, "return": true,
	"new": true, "delete": true, "in": true, "of": true, "this": true,
	"undefined": true, "null": true, "true": true, "false": true,
}
