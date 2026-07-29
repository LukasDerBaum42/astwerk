package reactive

// This file is deliberately free of build constraints: the reactive core is
// pure Go, so it is testable on the host. Everything that touches the DOM lives
// in the js/wasm-only files alongside it.

// source is the type-erased half of a Signal — the part the dependency graph
// tracks, so the graph doesn't have to be generic.
type source struct {
	subs []*effect
}

// effect is one reactive computation: a function plus the sources it read while
// running, so it can be re-run when any of them change.
type effect struct {
	fn       func()
	sources  []*source
	cleanups []func()
	children []*effect
	disposed bool
	running  bool
}

// Ownership and dependency tracking are separate concerns, and conflating them
// is a subtle bug: Untracked has to stop dependencies being recorded without
// orphaning effects created inside it.
//
//	owner    — who disposes what. Nested effects attach here.
//	observer — who depends on what. Signal reads register here.
var (
	owner    *effect
	observer *effect

	batchDepth int
	pending    []*effect
)

// Signal holds a value and notifies whatever read it when the value changes.
type Signal[T any] struct {
	src source
	v   T
}

// NewSignal returns a signal holding initial.
func NewSignal[T any](initial T) *Signal[T] {
	return &Signal[T]{v: initial}
}

// Get returns the current value and, inside an [Effect], records that the
// effect depends on this signal.
func (s *Signal[T]) Get() T {
	if observer != nil {
		link(observer, &s.src)
	}
	return s.v
}

// Peek returns the current value without recording a dependency.
func (s *Signal[T]) Peek() T { return s.v }

// Set stores v and re-runs every effect that read this signal. Writing a value
// equal to the current one changes nothing and notifies no one.
func (s *Signal[T]) Set(v T) {
	if equalValues(s.v, v) {
		return
	}
	s.v = v
	notify(&s.src)
}

// Update applies fn to the current value and stores the result. Use it for
// read-modify-write, so the read doesn't create a spurious dependency.
func (s *Signal[T]) Update(fn func(T) T) { s.Set(fn(s.v)) }

// Subscribe calls fn whenever the value changes, outside the dependency graph.
// It does not fire for the current value; call fn yourself first if you need
// that. The returned function unsubscribes.
func (s *Signal[T]) Subscribe(fn func(T)) (unsubscribe func()) {
	e := &effect{fn: func() { fn(s.v) }}
	// Linked by hand rather than by running it: a subscriber fires on the next
	// change, not immediately.
	link(e, &s.src)
	return func() { disposeEffect(e) }
}

// equalValues reports whether two values are equal for change-detection
// purposes.
//
// T is unconstrained, so == isn't available at compile time. Comparing through
// any works for comparable dynamic types and panics for the rest (slices, maps,
// funcs) — for those we recover and report "not equal", which is the safe
// answer: a spurious re-run is merely wasteful, a missed one is a bug.
func equalValues[T any](a, b T) (eq bool) {
	defer func() {
		if recover() != nil {
			eq = false
		}
	}()
	return any(a) == any(b)
}

// --- Dependency graph ---

func link(e *effect, s *source) {
	for _, existing := range e.sources {
		if existing == s {
			return
		}
	}
	e.sources = append(e.sources, s)
	s.subs = append(s.subs, e)
}

func unlinkAll(e *effect) {
	for _, s := range e.sources {
		for i, sub := range s.subs {
			if sub == e {
				s.subs = append(s.subs[:i], s.subs[i+1:]...)
				break
			}
		}
	}
	e.sources = nil
}

func notify(s *source) {
	// Copy: running an effect re-links it, mutating s.subs underneath us.
	subs := make([]*effect, len(s.subs))
	copy(subs, s.subs)
	for _, e := range subs {
		schedule(e)
	}
}

func schedule(e *effect) {
	if e.disposed {
		return
	}
	if batchDepth > 0 {
		for _, p := range pending {
			if p == e {
				return
			}
		}
		pending = append(pending, e)
		return
	}
	runEffect(e)
}

func runEffect(e *effect) {
	if e.disposed || e.running {
		// running guards an effect that writes a signal it also reads: skipping
		// the re-entrant run stops an infinite loop, and the outer run still
		// finishes with the new value.
		return
	}

	disposeChildren(e)
	runCleanups(e)
	unlinkAll(e)

	prevOwner, prevObserver := owner, observer
	owner, observer = e, e
	e.running = true
	defer func() {
		e.running = false
		owner, observer = prevOwner, prevObserver
	}()

	e.fn()
}

func runCleanups(e *effect) {
	for i := len(e.cleanups) - 1; i >= 0; i-- {
		e.cleanups[i]()
	}
	e.cleanups = nil
}

func disposeChildren(e *effect) {
	for _, c := range e.children {
		disposeEffect(c)
	}
	e.children = nil
}

func disposeEffect(e *effect) {
	if e.disposed {
		return
	}
	e.disposed = true
	disposeChildren(e)
	runCleanups(e)
	unlinkAll(e)
}

// --- Public control ---

// Effect runs fn immediately, then again whenever any signal it read changes.
//
// Effects nest: one created inside another is disposed and rebuilt when the
// outer re-runs, so nested bindings don't leak. The returned function disposes
// it and everything it owns.
func Effect(fn func()) (dispose func()) {
	e := &effect{fn: fn}
	if owner != nil {
		owner.children = append(owner.children, e)
	}
	runEffect(e)
	return func() { disposeEffect(e) }
}

// OnCleanup registers fn to run just before the enclosing effect re-runs, and
// when it is disposed. Use it to unbind listeners and cancel timers.
//
// Outside an effect it does nothing, since there is nothing to clean up after.
func OnCleanup(fn func()) {
	if owner != nil {
		owner.cleanups = append(owner.cleanups, fn)
	}
}

// Batch runs fn with notifications held back, so a group of writes triggers each
// affected effect once instead of once per write. Batches nest.
func Batch(fn func()) {
	batchDepth++
	defer func() {
		batchDepth--
		if batchDepth == 0 {
			flush()
		}
	}()
	fn()
}

func flush() {
	// Effects may schedule further effects, so drain rather than range once.
	for len(pending) > 0 {
		queue := pending
		pending = nil
		for _, e := range queue {
			runEffect(e)
		}
	}
}

// Untracked runs fn without recording dependencies on the signals it reads.
// Effects created inside it are still owned by the enclosing effect.
func Untracked[T any](fn func() T) T {
	prev := observer
	observer = nil
	defer func() { observer = prev }()
	return fn()
}

// detached runs fn with neither an owner nor an observer, so effects created
// inside it belong to nobody and must be disposed explicitly by the caller.
//
// Needed by keyed list rendering: an item's bindings have to survive the list
// effect re-running, which would otherwise dispose them as its own children.
func detached[T any](fn func() T) T {
	prevOwner, prevObserver := owner, observer
	owner, observer = nil, nil
	defer func() { owner, observer = prevOwner, prevObserver }()
	return fn()
}

// Memo is a value derived from other signals, recomputed when they change.
type Memo[T any] struct {
	s       *Signal[T]
	dispose func()
}

// Computed returns a [Memo] that re-evaluates fn whenever a signal it read
// changes. fn runs once immediately.
//
// Reading a Memo inside an effect registers a dependency on it, so derived
// values chain.
func Computed[T any](fn func() T) *Memo[T] {
	var zero T
	m := &Memo[T]{s: NewSignal(zero)}
	m.dispose = Effect(func() { m.s.Set(fn()) })
	return m
}

// Get returns the current value and records a dependency, like [Signal.Get].
func (m *Memo[T]) Get() T { return m.s.Get() }

// Peek returns the current value without recording a dependency.
func (m *Memo[T]) Peek() T { return m.s.Peek() }

// Subscribe calls fn on every change. See [Signal.Subscribe].
func (m *Memo[T]) Subscribe(fn func(T)) (unsubscribe func()) { return m.s.Subscribe(fn) }

// Dispose stops recomputing the value.
func (m *Memo[T]) Dispose() { m.dispose() }

// Scope groups effects under one owner so they can be disposed together.
// Anything created inside fn is torn down by the returned function.
//
// Unlike [Effect], a scope never re-runs: signals read directly inside fn are
// not tracked, because a scope is about ownership, not reactivity.
func Scope(fn func()) (dispose func()) {
	return Effect(func() {
		Untracked(func() any {
			fn()
			return nil
		})
	})
}
