package devserver

import (
	"fmt"
	"os/exec"
	"strings"
)

// Command returns a [Config.Build] function that runs an external command,
// treating a non-zero exit as a build failure and the command's combined output
// as the error — so a compiler message ends up in the browser overlay rather
// than only in the terminal.
//
// This is the form to use whenever your pages are compiled Go: templ
// components, layouts, anything the building program has baked in. See
// [Config.Build] for why an in-process build cannot pick those up.
//
//	Build: devserver.Command("go", "run", ".", "--out", "build", "--dev")
func Command(name string, args ...string) func() error {
	return func() error {
		out, err := exec.Command(name, args...).CombinedOutput()
		if err == nil {
			return nil
		}
		label := strings.Join(append([]string{name}, args...), " ")
		if len(out) == 0 {
			return fmt.Errorf("%s: %w", label, err)
		}
		return fmt.Errorf("%s: %w\n\n%s", label, err, out)
	}
}

// Steps runs build steps in order and stops at the first failure, for a build
// that takes more than one command:
//
//	Build: devserver.Steps(
//		devserver.Command("templ", "generate"),
//		devserver.Command("go", "run", ".", "--out", "build", "--dev"),
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
