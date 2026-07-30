package ssg

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

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
//  1. Page and Files are rendered into p, receiving a [Ctx] carrying the
//     node's title, its site path, and its locale.
//  2. CopyFrom is copied into p.
//  3. CompileFrom is compiled into p.
//  4. Generate is called and merged into Children (generated keys win).
//  5. Children are walked, appending each key to p.
//
// Children are visited in sorted key order, so a build is reproducible and a
// failure always reports the same node first. [BuildOptions.Parallel] gives up
// the visit order but keeps both guarantees.
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
	return buildNode(root, opts.OutDir, "", nil, opts)
}

// buildNode writes one node. dir is its output directory, sitePath its path
// relative to the site root ("" at the root, "de/about/" deeper down), and loc
// the locale it was mounted under, if any.
func buildNode(n Node, dir, sitePath string, loc *localeCtx, opts BuildOptions) error {
	if n.locale != nil {
		loc = n.locale
	}

	ctx := Ctx{
		Title:    n.Title,
		Path:     sitePath,
		Base:     opts.normalizeBase(),
		Relative: opts.RelativeURLs,
	}
	if loc != nil {
		ctx.Locale, ctx.Prefix = loc.code, loc.prefix
	}

	if n.Page != nil || len(n.Files) > 0 || n.CopyFrom != "" || n.CompileFrom != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("ssg: create %s: %w", dir, err)
		}
	}

	if n.Page != nil {
		if err := renderFile(filepath.Join(dir, "index.html"), n.Page(ctx), opts.Dev); err != nil {
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
		if err := renderFile(path, fn(ctx), opts.Dev); err != nil {
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

	keys := slices.Sorted(maps.Keys(children))
	build := func(i int) error {
		key := keys[i]
		sub, err := resolve(dir, key)
		if err != nil {
			return fmt.Errorf("ssg: child %q of %s: %w", key, dir, err)
		}
		return buildNode(children[key], sub, joinPath(sitePath, key), loc, opts)
	}

	if !opts.Parallel {
		for i := range keys {
			if err := build(i); err != nil {
				return err
			}
		}
		return nil
	}
	return inParallel(len(keys), build)
}

// inParallel runs fn for every index at once, bounded by GOMAXPROCS, and
// returns the lowest-indexed error.
//
// Collecting errors by index rather than racing them into a channel is what
// keeps a parallel build's failure reporting identical to a sequential one's:
// two broken pages always name the same one first.
func inParallel(n int, fn func(int) error) error {
	if n == 0 {
		return nil
	}

	errs := make([]error, n)
	sem := make(chan struct{}, min(n, runtime.GOMAXPROCS(0)))

	var wg sync.WaitGroup
	for i := range n {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			errs[i] = fn(i)
		})
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// joinPath appends a tree key to a site path, keeping the trailing-slash form
// pages are addressed by: "" + "about" -> "about/".
func joinPath(base, key string) string {
	return base + strings.Trim(filepath.ToSlash(key), "/") + "/"
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
