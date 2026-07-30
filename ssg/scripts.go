package ssg

import (
	"bufio"
	"fmt"
	"go/build/constraint"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CompileScripts glob-compiles a scripts directory into outDir.
//
// Every top-level entry of srcDir is compiled according to what it is:
//
//   - a .go file carrying a "//go:build js && wasm" constraint becomes
//     <name>.wasm, built with GOOS=js GOARCH=wasm;
//   - a subdirectory containing at least one such .go file becomes
//     <dirname>.wasm, built as a package;
//   - .ts files are handed to tsc in one batch, for projects not ready to
//     drop them.
//
// Anything else is ignored, so helper packages and non-wasm Go files can live
// in the same directory. When at least one .wasm is produced, Go's
// wasm_exec.js glue is copied into outDir alongside it.
//
// Each .wasm is compiled by its own go build process, and they run
// concurrently: they write to different files and share nothing but the build
// cache, which is already safe under concurrency. A compiler failure returns
// its combined output as part of the error, and with several failing the error
// is always the one earliest in directory order.
func CompileScripts(srcDir, outDir string) error {
	jobs, tsFiles, err := scriptJobs(srcDir, outDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}

	if err := inParallel(len(jobs), func(i int) error {
		return buildWasm(jobs[i].src, jobs[i].out)
	}); err != nil {
		return err
	}

	if len(tsFiles) > 0 {
		if err := compileTS(tsFiles, outDir); err != nil {
			return err
		}
	}
	if len(jobs) > 0 {
		if err := copyWasmExec(outDir); err != nil {
			return err
		}
	}
	return nil
}

// wasmJob is one .wasm to produce: a source file or package directory, and
// where its output goes.
type wasmJob struct{ src, out string }

// scriptJobs classifies srcDir's top-level entries. Reading build constraints
// is cheap and sequential; only the compilers themselves are worth fanning out.
func scriptJobs(srcDir, outDir string) (jobs []wasmJob, tsFiles []string, err error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return nil, nil, err
	}

	for _, e := range entries {
		name := e.Name()
		src := filepath.Join(srcDir, name)

		switch {
		case e.IsDir():
			ok, err := dirHasWasmFile(src)
			if err != nil {
				return nil, nil, err
			}
			if ok {
				jobs = append(jobs, wasmJob{src, filepath.Join(outDir, name+".wasm")})
			}

		case strings.HasSuffix(name, ".go"):
			ok, err := isWasmFile(src)
			if err != nil {
				return nil, nil, err
			}
			if ok {
				out := strings.TrimSuffix(name, ".go") + ".wasm"
				jobs = append(jobs, wasmJob{src, filepath.Join(outDir, out)})
			}

		case strings.HasSuffix(name, ".ts"):
			tsFiles = append(tsFiles, src)
		}
	}
	return jobs, tsFiles, nil
}

// buildWasm compiles a single Go file or package directory to a .wasm binary.
//
// The build runs from the entry's parent directory so it resolves against the
// consuming project's go.mod, letting a script import the project's own
// packages.
func buildWasm(src, out string) error {
	absOut, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return err
	}

	target := filepath.Base(absSrc)
	if !strings.HasSuffix(target, ".go") {
		// A package argument has to look like a path, or go build reads it as
		// a module path.
		target = "./" + target
	}

	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", absOut, target)
	cmd.Dir = filepath.Dir(absSrc)
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build %s: %w\n%s", src, err, b)
	}
	return nil
}

func compileTS(files []string, outDir string) error {
	args := []string{"--outDir", outDir, "--target", "ES2020", "--module", "ESNext"}
	cmd := exec.Command("tsc", append(args, files...)...)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tsc: %w\n%s", err, b)
	}
	return nil
}

// copyWasmExec copies Go's wasm_exec.js glue out of GOROOT into outDir.
func copyWasmExec(outDir string) error {
	root, err := goroot()
	if err != nil {
		return err
	}
	candidates := []string{
		filepath.Join(root, "lib", "wasm", "wasm_exec.js"), // Go 1.24+
		filepath.Join(root, "misc", "wasm", "wasm_exec.js"),
	}
	for _, src := range candidates {
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		return os.WriteFile(filepath.Join(outDir, "wasm_exec.js"), data, 0644)
	}
	return fmt.Errorf("wasm_exec.js not found under %s", root)
}

func goroot() (string, error) {
	if r := os.Getenv("GOROOT"); r != "" {
		return r, nil
	}
	b, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOROOT: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// dirHasWasmFile reports whether dir contains a top-level js/wasm Go file.
func dirHasWasmFile(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		ok, err := isWasmFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// isWasmFile reports whether path's build constraint is satisfied by js/wasm.
// A file with no constraint line does not count: without one it would also be
// compiled for the host platform by a plain `go build ./...`.
func isWasmFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if constraint.IsGoBuild(line) {
			expr, err := constraint.Parse(line)
			if err != nil {
				return false, fmt.Errorf("%s: %w", path, err)
			}
			return expr.Eval(func(tag string) bool {
				return tag == "js" || tag == "wasm"
			}), nil
		}
		// Build constraints live above the package clause; stop once it's passed.
		if strings.HasPrefix(line, "package ") {
			return false, nil
		}
	}
	return false, sc.Err()
}
