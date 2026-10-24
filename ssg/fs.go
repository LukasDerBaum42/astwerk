package ssg

import (
	"io/fs"
	"os"
	"path/filepath"
)

// copyFS copies src into dst, overwriting existing files. A full build starts
// from an empty directory, but an incremental build keeps the previous output,
// so this cannot assume the destination is empty (unlike os.CopyFS).
func copyFS(dst string, src fs.FS) error {
	return fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		_ = os.Remove(target)
		data, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}
