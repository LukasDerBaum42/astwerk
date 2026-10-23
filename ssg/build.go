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
//
// With [BuildOptions.Changed] set, the build is incremental: only nodes whose
// [Node.DependsOn] intersects the changed files are rebuilt, and OutDir is left
// in place.
func Build(root Node, opts BuildOptions) error {
	if opts.OutDir == "" {
		opts.OutDir = DefaultOutDir
	}
	cs := normalizeChanged(opts.Changed)
	if !opts.Dev && !cs.incremental() {
		if err := os.RemoveAll(opts.OutDir); err != nil {
			return fmt.Errorf("ssg: clean %s: %w", opts.OutDir, err)
		}
	}
	if err := os.MkdirAll(opts.OutDir, 0755); err != nil {
		return fmt.Errorf("ssg: create %s: %w", opts.OutDir, err)
	}
	if opts.Pre != nil {
		if err := opts.Pre(); err != nil {
			return fmt.Errorf("ssg: pre: %w", err)
		}
	}
	if err := buildNode(root, opts.OutDir, "", nil, nil, opts, cs); err != nil {
		return err
	}
	if opts.Post != nil {
		if err := opts.Post(); err != nil {
			return fmt.Errorf("ssg: post: %w", err)
		}
	}
	return nil
}

// buildNode writes one node. dir is its output directory, sitePath its path
// relative to the site root ("" at the root, "de/about/" deeper down), and loc
// the locale it was mounted under, if any. cs is the incremental changed set;
// an empty cs means a full build.
func buildNode(n Node, dir, sitePath string, loc *localeCtx, data any, opts BuildOptions, cs changedSet) error {
	if n.locale != nil {
		loc = n.locale
	}
	if n.Data != nil {
		data = n.Data
	}

	ctx := Ctx{
		Title:    n.Title,
		Path:     sitePath,
		Base:     opts.normalizeBase(),
		Relative: opts.RelativeURLs,
		Data:     data,
	}
	if loc != nil {
		ctx.Locale, ctx.Prefix = loc.code, loc.prefix
	}

	// A node renders its own output when the build is full, or when one of its
	// declared dependencies changed. Children are still walked either way, so an
	// affected descendant is reached even when its parent is not.
	affected := !cs.incremental() || nodeAffected(n, cs)

	if affected && (n.Page != nil || len(n.Files) > 0 || n.CopyFrom != "" || n.CompileFrom != "") {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("ssg: create %s: %w", dir, err)
		}
	}

	if affected {
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
	}

	children := n.Children
	if n.Generate != nil && affected {
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
		return buildNode(children[key], sub, joinPath(sitePath, key), loc, data, opts, cs)
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

// changedSet is the set of changed files for an incremental build. A nil slice
// means "full build": every node renders.
type changedSet []string

// normalizeChanged turns changed paths into cleaned absolute slash-separated
// paths, so [Node.DependsOn] entries match them regardless of how either was
// written. An empty or nil input yields a nil set (a full build).
func normalizeChanged(changed []string) changedSet {
	if len(changed) == 0 {
		return nil
	}
	out := make(changedSet, 0, len(changed))
	for _, p := range changed {
		if a, err := filepath.Abs(p); err == nil {
			out = append(out, filepath.ToSlash(filepath.Clean(a)))
		}
	}
	return out
}

func (cs changedSet) incremental() bool { return len(cs) > 0 }

// matches reports whether dep — a [Node.DependsOn] entry, which may name a file
// or a directory — is satisfied by any changed path. A directory entry matches
// every file beneath it.
func (cs changedSet) matches(dep string) bool {
	a, err := filepath.Abs(dep)
	if err != nil {
		return false
	}
	d := filepath.ToSlash(filepath.Clean(a))
	for _, c := range cs {
		if c == d || strings.HasPrefix(c, d+"/") {
			return true
		}
	}
	return false
}

func nodeAffected(n Node, cs changedSet) bool {
	for _, dep := range n.DependsOn {
		if cs.matches(dep) {
			return true
		}
	}
	return false
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
