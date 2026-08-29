package ssg

import (
	"errors"
	"strings"
	"testing"
)

func TestCommandReportsOutputOnFailure(t *testing.T) {
	if err := Command("sh", "-c", "exit 0")(); err != nil {
		t.Errorf("successful command returned %v", err)
	}

	err := Command("sh", "-c", "echo 'main.go:12: undefined: foo' >&2; exit 1")()
	if err == nil {
		t.Fatal("expected an error from a failing command")
	}
	// The compiler's message is the useful part, and it has to survive into the
	// error so the caller (or a dev-server overlay) can show it.
	if !strings.Contains(err.Error(), "undefined: foo") {
		t.Errorf("error dropped the command output: %v", err)
	}
	if !strings.Contains(err.Error(), "sh -c") {
		t.Errorf("error does not name the command: %v", err)
	}
}

func TestCommandReportsAMissingBinary(t *testing.T) {
	err := Command("astwerk-no-such-binary")()
	if err == nil || !strings.Contains(err.Error(), "astwerk-no-such-binary") {
		t.Errorf("err = %v, want it to name the missing binary", err)
	}
}

func TestStepsRunInOrderAndStopAtFailure(t *testing.T) {
	var ran []string
	step := func(name string, err error) func() error {
		return func() error { ran = append(ran, name); return err }
	}

	err := Steps(
		step("one", nil),
		nil, // skipped, not a panic
		step("two", errors.New("boom")),
		step("three", nil),
	)()

	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want boom", err)
	}
	if len(ran) != 2 || ran[0] != "one" || ran[1] != "two" {
		t.Errorf("ran = %v, want [one two] — three must not run after a failure", ran)
	}
	if err := Steps()(); err != nil {
		t.Errorf("Steps() with no steps = %v, want nil", err)
	}
}
