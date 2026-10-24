package ssg

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Command returns a build step that runs an external command, treating a
// non-zero exit as a failure and the command's combined output as the error —
// so a compiler message ends up in the error text rather than only in the
// terminal.
//
// This is the form to use whenever a build depends on compiled Go: templ
// generate, or building the site itself. A running Go program cannot load new
// code, so a build that calls page functions in-process silently keeps
// rendering the versions compiled into the running binary.
//
//	Build: ssg.Command("go", "run", ".", "--out", "build")
func Command(name string, args ...string) func() error {
	return func() error {
		label := strings.Join(append([]string{name}, args...), " ")
		fmt.Fprintf(os.Stderr, "  $ %s\n", label)

		// Streamed and captured at once: the terminal shows a long build as it
		// happens, and the copy still goes into the error so a compiler message
		// reaches the caller rather than only scrolling past.
		var buf bytes.Buffer
		cmd := exec.Command(name, args...)
		cmd.Stdout = io.MultiWriter(os.Stderr, &buf)
		cmd.Stderr = cmd.Stdout

		if err := cmd.Run(); err != nil {
			if buf.Len() == 0 {
				return fmt.Errorf("%s: %w", label, err)
			}
			return fmt.Errorf("%s: %w\n\n%s", label, err, buf.String())
		}
		return nil
	}
}

// Steps runs build steps in order and stops at the first failure, for a build
// that takes more than one command:
//
//	Build: ssg.Steps(
//		ssg.Command("templ", "generate"),
//		ssg.Command("go", "run", ".", "--out", "build"),
//	)
//
// A nil step is skipped, so a step can be switched off by a flag without
// restructuring the call.
func Steps(steps ...func() error) func() error {
	return func() error {
		for _, step := range steps {
			if step == nil {
				continue
			}
			if err := step(); err != nil {
				return err
			}
		}
		return nil
	}
}
