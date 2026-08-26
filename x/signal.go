package x

// state is the identity half of a reactive value: the part the generated
// program tracks, so the program does not have to be generic. It carries the
// build-time value and, for a [Memo], the expression it is derived from.
type state struct {
	name string // explicit JavaScript name, or "" for an assigned one
	init any    // build-time value
	expr *expr  // non-nil for a Memo
}

// Value is a reactive value the program can bind to: a [Signal] or a [Memo].
// It is sealed — only this package produces values.
type Value[T any] interface {
	Get() T
	Ref() Expr[T]
	state() *state
}

// Signal holds a value that exists both as a build-time Go value and as a live
// JavaScript state cell in the browser. It generates JavaScript only once it
// is actually used in markup, so an unused signal disappears from the output.
type Signal[T any] struct {
	id state
	v  T
}

// NewSignal returns a signal holding initial, under an assigned JavaScript
// name. Use [Named] when hand-written scripts or effects need to reference it.
func NewSignal[T any](initial T) *Signal[T] {
	return Named("", initial)
}

// Named returns a signal holding initial under the explicit JavaScript name
// name, so hand-written scripts and effects can reference it directly:
//
//	count := x.Named("count", 0)
//
//	@x.Script() {
//		aw.effect(() => console.log(count.get()));
//	}
//
// The name must be a valid JavaScript identifier and not a reserved one;
// otherwise it falls back to an assigned name.
func Named[T any](name string, initial T) *Signal[T] {
	return &Signal[T]{id: state{name: name, init: initial}, v: initial}
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

// Get returns the current build-time value.
func (s *Signal[T]) Get() T        { return s.v }
func (s *Signal[T]) state() *state { return &s.id }

// Ref returns an expression that reads the signal's current value, for
// composing a [Computed].
func (s *Signal[T]) Ref() Expr[T] { return getExpr(&s.id, s.v) }

// Set returns an action that stores e into the signal in the browser. Building
// it does nothing at build time; it runs only when the generated handler it
// belongs to fires.
func (s *Signal[T]) Set(e Expr[T]) Action {
	return Action{kind: actionSet, sig: &s.id, val: e.e}
}

// Add returns an expression for s.get() + v, where v is a literal or another
// reactive value of the same type. It is meaningful only for types where + is
// defined in JavaScript — numbers and strings.
func (s *Signal[T]) Add(v any) Expr[T] { return bin(exprAdd, s.Ref(), s.operand(v)) }

// Lit returns an expression for a constant, for setting a signal to a fixed
// value:
//
//	@x.El("button", x.On("click", count.Set(x.Lit(0)))) {
//		reset
//	}
func Lit[T any](v T) Expr[T] { return literal[T](v) }

// Not returns an expression for the negation of v, for toggling:
//
//	@x.El("button", x.On("click", open.Set(x.Not(open)))) {
//		toggle
//	}
func Not(v Value[bool]) Expr[bool] {
	return Expr[bool]{e: &expr{kind: exprNot, a: v.Ref().e}, v: !v.Get()}
}

// Sub returns an expression for s.get() - v.
func (s *Signal[T]) Sub(v any) Expr[T] { return bin(exprSub, s.Ref(), s.operand(v)) }

// Mul returns an expression for s.get() * v.
func (s *Signal[T]) Mul(v any) Expr[T] { return bin(exprMul, s.Ref(), s.operand(v)) }

// Div returns an expression for s.get() / v.
func (s *Signal[T]) Div(v any) Expr[T] { return bin(exprDiv, s.Ref(), s.operand(v)) }

// operand turns a literal or a same-typed reactive value into an expression.
func (s *Signal[T]) operand(v any) Expr[T] {
	if other, ok := v.(*Signal[T]); ok {
		return other.Ref()
	}
	if other, ok := v.(*Memo[T]); ok {
		return other.Ref()
	}
	return literal[T](v.(T))
}

// Memo is a value derived from other reactive values, recomputed when they
// change.
type Memo[T any] struct {
	id state
	v  T
}

// Computed returns a [Memo] whose value is e, recomputed in the browser
// whenever a signal e reads changes. e is evaluated at build time to supply
// the static value.
//
//	price := x.NewSignal(10)
//	qty := x.NewSignal(2)
//	total := x.Computed(price.Mul(qty))
func Computed[T any](e Expr[T]) *Memo[T] {
	return &Memo[T]{id: state{expr: e.e}, v: e.v}
}

// Get returns the current build-time value.
func (m *Memo[T]) Get() T        { return m.v }
func (m *Memo[T]) state() *state { return &m.id }

// Ref returns an expression that reads the memo's current value.
func (m *Memo[T]) Ref() Expr[T] { return getExpr(&m.id, m.v) }
