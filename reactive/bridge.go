//go:build js && wasm

package reactive

import (
	"encoding/json"
	"syscall/js"
)

// The bridge lets a wasm program reach the cells an x page already declared
// (x.NamedNumSignal("count", 0) and friends), as the same state objects a local
// signal is. XNum("count") returns a *NumSignal[int] that is a mirror of the x
// cell: reads come from it, writes go to it, and both sides' bindings update
// live through the cell's onChange. Equality checks on both sides make the
// mirror loop terminate.

// stateCell returns the named x cell, or an undefined js.Value when the page
// has no such cell — the wasm module loaded on a page without the x runtime, or
// the name doesn't match any Named* signal.
func stateCell(name string) js.Value {
	aw := js.Global().Get("aw")
	if !aw.Truthy() {
		return js.Undefined()
	}
	return aw.Call("state", name)
}

// mirror wires s to cell in both directions. read converts a JS value into a
// Go value; write converts back. It is only called when cell is a real cell.
func mirror[T any](s *Signal[T], cell js.Value, read func(js.Value) T, write func(T) any) {
	s.Set(read(cell.Call("get")))

	// JS -> Go: the cell's onChange fires s.Set, which notifies local
	// subscribers and effects.
	sub := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			s.Set(read(args[0]))
		}
		return nil
	})
	// x's sub fires immediately with the current value (equal, a no-op) and
	// again on every change.
	cell.Call("sub", sub)

	// Go -> JS: a local write goes to the cell. The cell's own equality check
	// stops the round trip (and s.Set's equality check stops the reverse).
	s.Subscribe(func(v T) { cell.Call("set", write(v)) })
}

// XNum returns the named x numeric cell as a *NumSignal[T].
func XNum[T int | int64 | float64](name string) *NumSignal[T] {
	cell := stateCell(name)
	if !cell.Truthy() {
		return Num[T](0)
	}
	var zero T
	read := func(v js.Value) T {
		switch any(zero).(type) {
		case int:
			return T(v.Int())
		case int64:
			return T(int64(v.Float()))
		default:
			return T(v.Float())
		}
	}
	s := Num[T](0)
	mirror(&s.Signal, cell, read, func(v T) any { return js.ValueOf(v) })
	return s
}

// XBool returns the named x bool cell as a *BoolSignal.
func XBool(name string) *BoolSignal {
	cell := stateCell(name)
	if !cell.Truthy() {
		return Bool(false)
	}
	s := Bool(false)
	mirror(&s.Signal, cell, func(v js.Value) bool { return v.Bool() }, func(v bool) any { return js.ValueOf(v) })
	return s
}

// XStr returns the named x string cell as a *StrSignal.
func XStr(name string) *StrSignal {
	cell := stateCell(name)
	if !cell.Truthy() {
		return Str("")
	}
	s := Str("")
	mirror(&s.Signal, cell, func(v js.Value) string { return v.String() }, func(v string) any { return js.ValueOf(v) })
	return s
}

// XList returns the named x list cell as a *ListSignal[T]. Non-primitive values
// cross as JSON — the honest fallback for a slice of structs.
func XList[T any](name string) *ListSignal[T] {
	cell := stateCell(name)
	if !cell.Truthy() {
		return List[T](nil)
	}
	read := func(v js.Value) []T {
		if !v.Truthy() {
			return nil
		}
		var out []T
		if err := json.Unmarshal([]byte(js.Global().Get("JSON").Call("stringify", v).String()), &out); err != nil {
			return nil
		}
		return out
	}
	write := func(v []T) any {
		b, err := json.Marshal(v)
		if err != nil {
			return js.Undefined()
		}
		return js.Global().Get("JSON").Call("parse", string(b))
	}
	s := List[T](nil)
	mirror(&s.Signal, cell, read, write)
	return s
}
