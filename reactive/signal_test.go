package reactive

import (
	"fmt"
	"testing"
)

func TestSignalGetSet(t *testing.T) {
	s := NewSignal(1)
	if s.Get() != 1 {
		t.Errorf("Get = %d", s.Get())
	}
	s.Set(2)
	if s.Get() != 2 {
		t.Errorf("after Set, Get = %d", s.Get())
	}
}

func TestEffectRunsAndReruns(t *testing.T) {
	s := NewSignal("a")
	var seen []string
	dispose := Effect(func() { seen = append(seen, s.Get()) })
	defer dispose()

	s.Set("b")
	s.Set("c")

	want := []string{"a", "b", "c"}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Errorf("seen = %v, want %v", seen, want)
	}
}

func TestEffectDisposeStopsReruns(t *testing.T) {
	s := NewSignal(0)
	runs := 0
	dispose := Effect(func() { s.Get(); runs++ })

	s.Set(1)
	dispose()
	s.Set(2)
	dispose() // idempotent

	if runs != 2 {
		t.Errorf("effect ran %d times, want 2", runs)
	}
}

// Writing an equal value must not wake anything up.
func TestSetEqualValueIsNoop(t *testing.T) {
	s := NewSignal(5)
	runs := 0
	defer Effect(func() { s.Get(); runs++ })()

	s.Set(5)
	s.Set(5)
	if runs != 1 {
		t.Errorf("effect ran %d times, want 1", runs)
	}

	s.Set(6)
	if runs != 2 {
		t.Errorf("after a real change, runs = %d, want 2", runs)
	}
}

// A non-comparable value type must still work, erring towards re-running.
func TestSetNonComparableAlwaysNotifies(t *testing.T) {
	s := NewSignal([]int{1})
	runs := 0
	defer Effect(func() { s.Get(); runs++ })()

	s.Set([]int{1}) // deep-equal, but slices aren't comparable
	if runs != 2 {
		t.Errorf("runs = %d, want 2 (slices must be treated as changed)", runs)
	}
}

func TestPeekDoesNotTrack(t *testing.T) {
	s := NewSignal(0)
	runs := 0
	defer Effect(func() { s.Peek(); runs++ })()

	s.Set(1)
	if runs != 1 {
		t.Errorf("Peek created a dependency: runs = %d, want 1", runs)
	}
}

func TestUpdate(t *testing.T) {
	s := NewSignal(10)
	s.Update(func(v int) int { return v * 3 })
	if s.Get() != 30 {
		t.Errorf("Get = %d, want 30", s.Get())
	}
}

func TestSubscribeFiresOnChangeOnly(t *testing.T) {
	s := NewSignal("x")
	var seen []string
	unsub := s.Subscribe(func(v string) { seen = append(seen, v) })

	if len(seen) != 0 {
		t.Errorf("Subscribe fired immediately: %v", seen)
	}
	s.Set("y")
	unsub()
	s.Set("z")

	if fmt.Sprint(seen) != fmt.Sprint([]string{"y"}) {
		t.Errorf("seen = %v, want [y]", seen)
	}
}

// Dependencies are re-collected each run, so a branch not taken stops tracking.
func TestDynamicDependencies(t *testing.T) {
	useA := NewSignal(true)
	a, b := NewSignal("a"), NewSignal("b")

	runs := 0
	defer Effect(func() {
		runs++
		if useA.Get() {
			a.Get()
		} else {
			b.Get()
		}
	})()

	if runs != 1 {
		t.Fatalf("initial runs = %d", runs)
	}

	b.Set("b2") // not a dependency yet
	if runs != 1 {
		t.Errorf("untracked branch triggered a re-run: runs = %d", runs)
	}

	a.Set("a2")
	if runs != 2 {
		t.Errorf("tracked signal did not trigger: runs = %d", runs)
	}

	useA.Set(false) // run 3, now depends on b instead of a
	a.Set("a3")
	if runs != 3 {
		t.Errorf("stale dependency on a survived: runs = %d, want 3", runs)
	}
	b.Set("b3")
	if runs != 4 {
		t.Errorf("new dependency on b not picked up: runs = %d, want 4", runs)
	}
}

func TestBatchCoalesces(t *testing.T) {
	a, b := NewSignal(1), NewSignal(1)
	runs := 0
	defer Effect(func() { a.Get(); b.Get(); runs++ })()

	Batch(func() {
		a.Set(2)
		b.Set(2)
		a.Set(3)
	})

	if runs != 2 {
		t.Errorf("effect ran %d times, want 2 (once initially, once for the batch)", runs)
	}
}

func TestBatchNests(t *testing.T) {
	s := NewSignal(0)
	runs := 0
	defer Effect(func() { s.Get(); runs++ })()

	Batch(func() {
		s.Set(1)
		Batch(func() { s.Set(2) })
		if runs != 1 {
			t.Errorf("inner batch flushed early: runs = %d", runs)
		}
	})
	if runs != 2 {
		t.Errorf("runs = %d, want 2", runs)
	}
}

func TestOnCleanupRunsBeforeRerunAndOnDispose(t *testing.T) {
	s := NewSignal(0)
	var log []string

	dispose := Effect(func() {
		v := s.Get()
		log = append(log, fmt.Sprintf("run%d", v))
		OnCleanup(func() { log = append(log, fmt.Sprintf("clean%d", v)) })
	})

	s.Set(1)
	dispose()

	want := "[run0 clean0 run1 clean1]"
	if fmt.Sprint(log) != want {
		t.Errorf("log = %v, want %s", log, want)
	}
}

func TestNestedEffectsAreDisposedWithParent(t *testing.T) {
	outer := NewSignal(0)
	inner := NewSignal(0)
	innerRuns := 0

	dispose := Effect(func() {
		outer.Get()
		Effect(func() { inner.Get(); innerRuns++ })
	})

	if innerRuns != 1 {
		t.Fatalf("innerRuns = %d, want 1", innerRuns)
	}

	inner.Set(1)
	if innerRuns != 2 {
		t.Fatalf("innerRuns = %d, want 2", innerRuns)
	}

	// Re-running the outer effect must dispose the old inner one, not stack up.
	outer.Set(1)
	if innerRuns != 3 {
		t.Fatalf("innerRuns = %d, want 3 (fresh inner effect)", innerRuns)
	}
	inner.Set(2)
	if innerRuns != 4 {
		t.Errorf("innerRuns = %d, want 4 — old inner effect was not disposed", innerRuns)
	}

	dispose()
	inner.Set(3)
	if innerRuns != 4 {
		t.Errorf("innerRuns = %d after parent dispose, want 4", innerRuns)
	}
}

func TestUntracked(t *testing.T) {
	tracked, hidden := NewSignal(0), NewSignal(0)
	runs := 0

	defer Effect(func() {
		runs++
		tracked.Get()
		Untracked(func() int { return hidden.Get() })
	})()

	hidden.Set(1)
	if runs != 1 {
		t.Errorf("Untracked read created a dependency: runs = %d", runs)
	}
	tracked.Set(1)
	if runs != 2 {
		t.Errorf("runs = %d, want 2", runs)
	}
}

// Untracked must not orphan effects created inside it — they still belong to
// the enclosing effect and must be disposed with it.
func TestUntrackedKeepsOwnership(t *testing.T) {
	s := NewSignal(0)
	innerRuns := 0

	dispose := Effect(func() {
		Untracked(func() any {
			Effect(func() { s.Get(); innerRuns++ })
			return nil
		})
	})

	s.Set(1)
	if innerRuns != 2 {
		t.Fatalf("innerRuns = %d, want 2", innerRuns)
	}

	dispose()
	s.Set(2)
	if innerRuns != 2 {
		t.Errorf("innerRuns = %d after dispose, want 2 — inner effect was orphaned", innerRuns)
	}
}

func TestComputed(t *testing.T) {
	first, last := NewSignal("Ada"), NewSignal("Lovelace")
	full := Computed(func() string { return first.Get() + " " + last.Get() })
	defer full.Dispose()

	if got := full.Get(); got != "Ada Lovelace" {
		t.Errorf("Get = %q", got)
	}
	last.Set("Byron")
	if got := full.Get(); got != "Ada Byron" {
		t.Errorf("after Set, Get = %q", got)
	}
}

func TestComputedChains(t *testing.T) {
	n := NewSignal(2)
	double := Computed(func() int { return n.Get() * 2 })
	quad := Computed(func() int { return double.Get() * 2 })
	defer double.Dispose()
	defer quad.Dispose()

	if quad.Get() != 8 {
		t.Fatalf("quad = %d, want 8", quad.Get())
	}
	n.Set(3)
	if quad.Get() != 12 {
		t.Errorf("quad = %d, want 12", quad.Get())
	}
}

func TestComputedDrivesEffect(t *testing.T) {
	n := NewSignal(1)
	even := Computed(func() bool { return n.Get()%2 == 0 })
	defer even.Dispose()

	var seen []bool
	defer Effect(func() { seen = append(seen, even.Get()) })()

	n.Set(2)
	n.Set(4) // still even: the memo doesn't change, so no re-run

	if fmt.Sprint(seen) != fmt.Sprint([]bool{false, true}) {
		t.Errorf("seen = %v, want [false true]", seen)
	}
}

func TestComputedDisposeStops(t *testing.T) {
	n := NewSignal(1)
	m := Computed(func() int { return n.Get() * 10 })

	m.Dispose()
	n.Set(2)
	if got := m.Peek(); got != 10 {
		t.Errorf("Peek = %d, want the stale 10 after Dispose", got)
	}
}

// An effect that writes a signal it also reads must settle, not spin forever.
func TestSelfWritingEffectDoesNotLoop(t *testing.T) {
	s := NewSignal(0)
	runs := 0

	defer Effect(func() {
		runs++
		if v := s.Get(); v < 3 {
			s.Set(v + 1)
		}
	})()

	if runs > 10 {
		t.Fatalf("effect ran %d times — it is looping", runs)
	}
	if s.Get() == 0 {
		t.Error("effect never wrote")
	}
}

func TestScopeDisposesChildren(t *testing.T) {
	s := NewSignal(0)
	runs := 0

	dispose := Scope(func() {
		Effect(func() { s.Get(); runs++ })
		Effect(func() { s.Get(); runs++ })
	})

	if runs != 2 {
		t.Fatalf("runs = %d, want 2", runs)
	}
	s.Set(1)
	if runs != 4 {
		t.Fatalf("runs = %d, want 4", runs)
	}

	dispose()
	s.Set(2)
	if runs != 4 {
		t.Errorf("runs = %d after scope dispose, want 4", runs)
	}
}

// A scope is ownership only: a signal read directly inside it must not make the
// whole scope rebuild.
func TestScopeDoesNotTrack(t *testing.T) {
	s := NewSignal(0)
	scopeRuns := 0

	dispose := Scope(func() { scopeRuns++; s.Get() })
	defer dispose()

	s.Set(1)
	if scopeRuns != 1 {
		t.Errorf("scope re-ran: %d times, want 1", scopeRuns)
	}
}
