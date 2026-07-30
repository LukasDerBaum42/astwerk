package devserver

import (
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// watcher reports which watched files changed since the last scan.
//
// It compares a snapshot of size and modification time rather than hashing
// contents: a false positive costs one wasted rebuild, and reading every file
// on every tick to avoid that is a far worse trade on a poll loop.
type watcher struct {
	roots  []string
	exts   []string
	ignore []string
	prev   map[string]stamp
}

type stamp struct {
	size    int64
	modUnix int64
}

func newWatcher(cfg Config) *watcher {
	ignore := slices.Clone(cfg.Ignore)
	// Without this the build's own output would trigger the next build.
	if dir := filepath.Clean(cfg.Dir); dir != "" && dir != "." {
		ignore = append(ignore, filepath.Base(dir))
	}
	return &watcher{
		roots:  slices.Clone(cfg.Watch),
		exts:   slices.Clone(cfg.Exts),
		ignore: ignore,
		prev:   map[string]stamp{},
	}
}

// scan returns the paths that were added, modified or deleted since the last
// call, sorted, and adopts the new state. The first call reports everything, so
// callers prime it once at startup.
func (w *watcher) scan() []string {
	now := make(map[string]stamp, len(w.prev))

	for _, root := range w.roots {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// A file deleted mid-walk, or a directory we can't read, is not
				// worth stopping the whole watch for.
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				// A watched root is watched even if its name would otherwise be
				// skipped — asking for ".github" and getting nothing is worse
				// than the rule it breaks.
				if path != root && w.skipDir(path, d.Name()) {
					return fs.SkipDir
				}
				return nil
			}
			if !w.watchExt(d.Name()) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			now[path] = stamp{size: info.Size(), modUnix: info.ModTime().UnixNano()}
			return nil
		})
	}

	var changed []string
	for path, s := range now {
		if old, ok := w.prev[path]; !ok || old != s {
			changed = append(changed, path)
		}
	}
	for path := range w.prev {
		if _, ok := now[path]; !ok {
			changed = append(changed, path)
		}
	}

	w.prev = now
	sort.Strings(changed)
	return changed
}

// skipDir reports whether a directory should not be descended into. Names are
// matched anywhere in the tree, so "node_modules" catches a nested one too;
// a configured entry containing a separator is matched against the path.
func (w *watcher) skipDir(path, name string) bool {
	if strings.HasPrefix(name, ".") && name != "." && name != ".." {
		return true
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	for _, ig := range w.ignore {
		if ig == "" {
			continue
		}
		if strings.ContainsAny(ig, `/\`) {
			if clean == filepath.ToSlash(filepath.Clean(ig)) {
				return true
			}
			continue
		}
		if name == ig {
			return true
		}
	}
	return false
}

func (w *watcher) watchExt(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return slices.Contains(w.exts, ext)
}
