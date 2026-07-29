package ssg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeModule gives a fixture directory a go.mod, so `go build` inside it
// resolves the same way it would in a real project.
func writeModule(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "go.mod"), "module scriptfixture\n\ngo 1.24\n")
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestIsWasmFile(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"js && wasm", "//go:build js && wasm\n\npackage main\n", true},
		{"wasm && js", "//go:build wasm && js\n\npackage main\n", true},
		{"js only", "//go:build js\n\npackage main\n", true},
		{"linux", "//go:build linux\n\npackage main\n", false},
		{"negated", "//go:build !js\n\npackage main\n", false},
		{"no constraint", "package main\n", false},
		{"after package clause", "package main\n\n//go:build js && wasm\n", false},
		{"with doc comment", "// Package x does things.\n//go:build js && wasm\n\npackage main\n", true},
	}

	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(dir, c.name+".go")
			writeFile(t, path, c.body)
			got, err := isWasmFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("isWasmFile(%q) = %v, want %v", c.body, got, c.want)
			}
		})
	}
}

func TestCompileScriptsIgnoresNonWasm(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "scripts")
	out := filepath.Join(dir, "out")

	writeFile(t, filepath.Join(src, "helper.go"), "package scripts\n\nfunc Helper() {}\n")
	writeFile(t, filepath.Join(src, "notes.md"), "not a script")
	writeFile(t, filepath.Join(src, "shared", "util.go"), "package shared\n\nfunc U() {}\n")

	if err := CompileScripts(src, out); err != nil {
		t.Fatal(err)
	}
	if got := tree(t, out); len(got) != 0 {
		t.Errorf("compiled %v, want nothing", got)
	}
}

func TestCompileScriptsMissingDir(t *testing.T) {
	dir := t.TempDir()
	if err := CompileScripts(filepath.Join(dir, "nope"), filepath.Join(dir, "out")); err == nil {
		t.Fatal("expected an error for a missing scripts directory")
	}
}

func TestCompileScriptsReportsCompilerOutput(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir)
	src := filepath.Join(dir, "scripts")
	writeFile(t, filepath.Join(src, "broken.go"), "//go:build js && wasm\n\npackage main\n\nfunc main() { this is not go }\n")

	err := CompileScripts(src, filepath.Join(dir, "out"))
	if err == nil {
		t.Fatal("expected a build error")
	}
	if !strings.Contains(err.Error(), "broken.go") {
		t.Errorf("error does not name the failing file: %v", err)
	}
}

// TestCompileScriptsBuildsWasm shells out to the real toolchain, so it is
// skipped under -short.
func TestCompileScriptsBuildsWasm(t *testing.T) {
	if testing.Short() {
		t.Skip("invokes the go toolchain")
	}

	dir := t.TempDir()
	writeModule(t, dir)
	src := filepath.Join(dir, "scripts")
	out := filepath.Join(dir, "out")

	writeFile(t, filepath.Join(src, "nav.go"),
		"//go:build js && wasm\n\npackage main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(src, "game", "main.go"),
		"//go:build js && wasm\n\npackage main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(src, "ignored.go"), "package scripts\n")

	if err := CompileScripts(src, out); err != nil {
		t.Fatal(err)
	}

	want := []string{"game.wasm", "nav.wasm", "wasm_exec.js"}
	if got := tree(t, out); !equal(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	for _, name := range []string{"nav.wasm", "game.wasm"} {
		fi, err := os.Stat(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}
}

// A CompileFrom node runs the same step through the walker.
func TestBuildCompileFrom(t *testing.T) {
	if testing.Short() {
		t.Skip("invokes the go toolchain")
	}

	dir := t.TempDir()
	writeModule(t, dir)
	src := filepath.Join(dir, "scripts")
	out := filepath.Join(dir, "build")
	writeFile(t, filepath.Join(src, "nav.go"), "//go:build js && wasm\n\npackage main\n\nfunc main() {}\n")

	root := Node{
		Page:     text("home"),
		Children: map[string]Node{"scripts": {CompileFrom: src}},
	}
	if err := Build(root, BuildOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}

	want := []string{"index.html", "scripts/nav.wasm", "scripts/wasm_exec.js"}
	if got := tree(t, out); !equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}

func TestCopyWasmExec(t *testing.T) {
	out := t.TempDir()
	if err := copyWasmExec(out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "wasm_exec.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "globalThis.Go") {
		t.Errorf("copied file does not look like wasm_exec.js")
	}
}
