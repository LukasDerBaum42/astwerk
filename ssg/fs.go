package ssg

import (
	"io/fs"
	"os"
	"path/filepath"
)

// devCreate opens path for writing. In dev mode it truncates in place instead
// of replacing the file, so anything holding the path (a dev server, a watcher)
// keeps seeing the same inode.
func devCreate(path string, dev bool) (*os.File, error) {
	if dev {
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	}
	return os.Create(path)
}

// devCopyFS copies src into dst. In dev mode it walks and overwrites file by
// file, because os.CopyFS refuses to overwrite an existing file and dev builds
// deliberately keep the previous output around.
func devCopyFS(dst string, src fs.FS, dev bool) error {
	if dev {
		return fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			target := filepath.Join(dst, path)
			if d.IsDir() {
				return os.MkdirAll(target, 0755)
			}
			os.Remove(target)
			data, err := fs.ReadFile(src, path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, 0644)
		})
	}
	return os.CopyFS(dst, src)
}
