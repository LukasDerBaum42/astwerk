package ssg

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/a-h/templ"
)

// DefaultOutDir is the output directory used when [BuildOptions.OutDir] is empty.
const DefaultOutDir = "build"

// Build walks the tree from root and writes the whole site to opts.OutDir.
//
// Unless opts.Dev is set, OutDir is removed first so the build starts clean.
//
// For each node, at its resolved output path p:
//
//  1. Page and Files are rendered into p.
//  2. CopyFrom is copied into p.
//  3. CompileFrom is compiled into p.
//  4. Generate is called and merged into Children (generated keys win).
//  5. Children are walked, appending each key to p.
//
// Children are visited in sorted key order, so a build is reproducible and a
// failure always reports the same node first.
func Build(root Node, opts BuildOptions) error {
	if opts.OutDir == "" {
		opts.OutDir = DefaultOutDir
	}
	if !opts.Dev {
		if err := os.RemoveAll(opts.OutDir); err != nil {
			return fmt.Errorf("ssg: clean %s: %w", opts.OutDir, err)
		}
	}
	if err := os.MkdirAll(opts.OutDir, 0755); err != nil {
		return fmt.Errorf("ssg: create %s: %w", opts.OutDir, err)
	}
	return buildNode(root, opts.OutDir, opts)
}

func buildNode(n Node, dir string, opts BuildOptions) error {
	if n.Page != nil || len(n.Files) > 0 || n.CopyFrom != "" || n.CompileFrom != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("ssg: create %s: %w", dir, err)
		}
	}

	if n.Page != nil {
		if err := renderFile(filepath.Join(dir, "index.html"), n.Page(), opts.Dev); err != nil {
			return err
		}
	}

	for _, name := range slices.Sorted(maps.Keys(n.Files)) {
		fn := n.Files[name]
		if fn == nil {
			continue
		}
		path, err := resolve(dir, name)
		if err != nil {
			return fmt.Errorf("ssg: file %q in %s: %w", name, dir, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("ssg: create %s: %w", filepath.Dir(path), err)
		}
		if err := renderFile(path, fn(), opts.Dev); err != nil {
			return err
		}
	}

	if n.CopyFrom != "" {
		if err := devCopyFS(dir, os.DirFS(n.CopyFrom), opts.Dev); err != nil {
			return fmt.Errorf("ssg: copy %s -> %s: %w", n.CopyFrom, dir, err)
		}
	}

	if n.CompileFrom != "" {
		if err := CompileScripts(n.CompileFrom, dir); err != nil {
			return fmt.Errorf("ssg: compile %s -> %s: %w", n.CompileFrom, dir, err)
		}
	}

	children := n.Children
	if n.Generate != nil {
		merged := make(map[string]Node, len(n.Children))
		maps.Copy(merged, n.Children)
		maps.Copy(merged, n.Generate()) // generated keys win on collision
		children = merged
	}

	for _, key := range slices.Sorted(maps.Keys(children)) {
		sub, err := resolve(dir, key)
		if err != nil {
			return fmt.Errorf("ssg: child %q of %s: %w", key, dir, err)
		}
		if err := buildNode(children[key], sub, opts); err != nil {
			return err
		}
	}

	return nil
}

// renderFile renders c into path, creating or truncating it per dev mode.
func renderFile(path string, c templ.Component, dev bool) error {
	if c == nil {
		return fmt.Errorf("ssg: nil component for %s", path)
	}
	f, err := devCreate(path, dev)
	if err != nil {
		return fmt.Errorf("ssg: create %s: %w", path, err)
	}
	defer f.Close()
	if err := c.Render(context.Background(), f); err != nil {
		return fmt.Errorf("ssg: render %s: %w", path, err)
	}
	return f.Close()
}

// resolve joins a tree key onto dir, rejecting anything that would escape it.
// Trailing slashes are accepted, since directory keys are commonly written
// that way ("projects/").
func resolve(dir, key string) (string, error) {
	slashed := strings.TrimRight(filepath.ToSlash(key), "/")
	if slashed == "" || slashed == "." {
		return "", fmt.Errorf("empty name")
	}
	clean, err := filepath.Localize(slashed)
	if err != nil {
		return "", fmt.Errorf("not a relative path inside the output directory")
	}
	return filepath.Join(dir, clean), nil
}
