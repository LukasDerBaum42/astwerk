// Package assets scans a directory tree and pairs each regular file with the
// metadata a sidecar file provides for it.
//
// The simple path uses a JSON or TOML map in each directory, named "meta" by
// default (meta.json or meta.toml), from file name to metadata:
//
//	{
//	  "cover.png": {"credit": "anzeal", "license": "CC-BY-4.0"},
//	  "poster.jpg": {"credit": "ollie", "license": "CC-BY-4.0"}
//	}
//
// Files without an entry still appear; their Meta is the zero value and
// HasMeta is false. Set [Options.RequireMeta] to keep only catalogued files.
//
// Two seams keep the package open to other shapes:
//
//   - [Options.Sidecar] replaces how metadata is found for a directory, so a
//     different file name, a per-directory object, inheritance from a parent,
//     or a remote source all fit without touching this package.
//   - [Walk] yields directories and their files with no metadata logic at all,
//     for a scan that needs a completely different model.
package assets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// File is one regular file found during a scan.
type File struct {
	// Path is relative to the scan root, slash-separated: "posters/cover.png".
	Path string
	// Name is the base name, "cover.png".
	Name string
	// Ext is the lower-case extension including the dot, ".png".
	Ext string
	// Size is the file size in bytes.
	Size int64
}

// Entry is one file and its sidecar metadata.
type Entry[T any] struct {
	File File
	Meta T
	// HasMeta reports whether the sidecar had an entry for this file.
	HasMeta bool
}

// WalkOptions filters a [Walk].
type WalkOptions struct {
	// Include limits the scan to file names matching one of these
	// filepath.Match patterns. Empty means every regular file.
	Include []string

	// Exclude drops file names matching one of these filepath.Match patterns.
	Exclude []string

	// SkipDirs lists directory names to skip, e.g. ".git".
	SkipDirs []string
}

// Options configures [LoadTree] and [LoadDir].
type Options[T any] struct {
	// SidecarName is the base name of the metadata file, without extension.
	// Defaults to "meta". Ignored when Sidecar is set.
	SidecarName string

	// Sidecar replaces the built-in sidecar lookup. It receives the OS path of
	// a directory and returns metadata keyed by file name. A nil map (or a
	// missing key) means "no metadata for this file".
	//
	// Use this for a different file layout, per-directory metadata, inherited
	// metadata, or a source other than a file.
	Sidecar func(dir string) (map[string]T, error)

	// Include limits the scan to file names matching one of these
	// filepath.Match patterns. Empty means every regular file.
	Include []string

	// Exclude drops file names matching one of these filepath.Match patterns.
	// The built-in sidecar files are excluded automatically.
	Exclude []string

	// SkipDirs lists directory names to skip, e.g. ".git".
	SkipDirs []string

	// RequireMeta omits files the sidecar does not describe.
	RequireMeta bool
}

// LoadTree scans root recursively and returns every matching file in path
// order.
func LoadTree[T any](root string, opts Options[T]) ([]Entry[T], error) {
	return load(root, opts, true)
}

// LoadDir scans only the files directly inside dir.
func LoadDir[T any](dir string, opts Options[T]) ([]Entry[T], error) {
	return load(dir, opts, false)
}

func load[T any](root string, opts Options[T], recursive bool) ([]Entry[T], error) {
	walkOpts := WalkOptions{Include: opts.Include, Exclude: opts.Exclude, SkipDirs: opts.SkipDirs}
	if opts.Sidecar == nil {
		name := opts.sidecarName()
		walkOpts.Exclude = append(walkOpts.Exclude, name+".json", name+".toml")
	}

	var entries []Entry[T]
	err := walk(root, walkOpts, recursive, func(dir string, files []File) error {
		meta, err := opts.meta(dir)
		if err != nil {
			return err
		}
		for _, file := range files {
			value, ok := meta[file.Name]
			if !ok && opts.RequireMeta {
				continue
			}
			entries = append(entries, Entry[T]{File: file, Meta: value, HasMeta: ok})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].File.Path < entries[j].File.Path })
	return entries, nil
}

// Walk calls visit for every directory under root (including root), in sorted
// depth-first order, with the regular files that pass opts. A directory with no
// matching files is still visited, so a caller can inspect its sidecars.
//
// root and each dir are OS paths; File.Path is relative to root. Walk does no
// metadata work — use it when [LoadTree]'s model does not fit and build your
// own entries.
func Walk(root string, opts WalkOptions, visit func(dir string, files []File) error) error {
	return walk(root, opts, true, visit)
}

func walk(root string, opts WalkOptions, recursive bool, visit func(dir string, files []File) error) error {
	return walkDir(root, root, opts, recursive, visit)
}

func walkDir(root, dir string, opts WalkOptions, recursive bool, visit func(dir string, files []File) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var files []File
	var subdirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			if recursive && !opts.skipsDir(entry.Name()) {
				subdirs = append(subdirs, filepath.Join(dir, entry.Name()))
			}
			continue
		}
		if !entry.Type().IsRegular() || !opts.includes(entry.Name()) || opts.excludes(entry.Name()) {
			continue
		}
		rel, err := filepath.Rel(root, filepath.Join(dir, entry.Name()))
		if err != nil {
			return fmt.Errorf("assets: %s: %w", entry.Name(), err)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("assets: %s: %w", entry.Name(), err)
		}
		files = append(files, File{
			Path: filepath.ToSlash(rel),
			Name: entry.Name(),
			Ext:  strings.ToLower(filepath.Ext(entry.Name())),
			Size: info.Size(),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	if err := visit(dir, files); err != nil {
		return err
	}
	for _, sub := range subdirs {
		if err := walkDir(root, sub, opts, recursive, visit); err != nil {
			return err
		}
	}
	return nil
}

// sidecarName is the built-in sidecar base name.
func (o Options[T]) sidecarName() string {
	if o.SidecarName == "" {
		return "meta"
	}
	return o.SidecarName
}

// meta returns the metadata for dir using the configured source.
func (o Options[T]) meta(dir string) (map[string]T, error) {
	if o.Sidecar != nil {
		return o.Sidecar(dir)
	}
	return readSidecarPair[T](filepath.Join(dir, o.sidecarName()))
}

// MetaSidecar returns a Sidecar implementation for a named JSON or TOML file.
// It is the built-in lookup, made explicit for use with a custom Options.Sidecar:
//
//	assets.Options[Media]{Sidecar: assets.MetaSidecar[Media]("credits")}
func MetaSidecar[T any](name string) func(dir string) (map[string]T, error) {
	return func(dir string) (map[string]T, error) {
		return readSidecarPair[T](filepath.Join(dir, name))
	}
}

// ReadSidecar reads one JSON or TOML file into a map keyed by file name. Use
// it when the sidecar has a different name or lives outside the scanned
// directory:
//
//	opts.Sidecar = func(dir string) (map[string]Media, error) {
//		return assets.ReadSidecar[Media](filepath.Join("credits", filepath.Base(dir)+".json"))
//	}
func ReadSidecar[T any](path string) (map[string]T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("assets: %s: %w", path, err)
	}
	var meta map[string]T
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, fmt.Errorf("assets: %s: %w", path, err)
		}
	case ".toml":
		if err := toml.Unmarshal(data, &meta); err != nil {
			return nil, fmt.Errorf("assets: %s: %w", path, err)
		}
	default:
		return nil, fmt.Errorf("assets: %s: expected .json or .toml", path)
	}
	if meta == nil {
		meta = map[string]T{}
	}
	return meta, nil
}

func readSidecarPair[T any](base string) (map[string]T, error) {
	jsonPath := base + ".json"
	tomlPath := base + ".toml"

	jsonExists := fileExists(jsonPath)
	tomlExists := fileExists(tomlPath)
	switch {
	case jsonExists && tomlExists:
		return nil, fmt.Errorf("assets: %s and %s both exist", jsonPath, tomlPath)
	case jsonExists:
		return ReadSidecar[T](jsonPath)
	case tomlExists:
		return ReadSidecar[T](tomlPath)
	default:
		return map[string]T{}, nil
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (o WalkOptions) includes(name string) bool {
	if len(o.Include) == 0 {
		return true
	}
	for _, pattern := range o.Include {
		if ok, _ := filepath.Match(pattern, name); ok {
			return true
		}
	}
	return false
}

func (o WalkOptions) skipsDir(name string) bool {
	for _, skip := range o.SkipDirs {
		if name == skip {
			return true
		}
	}
	return false
}

func (o WalkOptions) excludes(name string) bool {
	for _, pattern := range o.Exclude {
		if ok, _ := filepath.Match(pattern, name); ok {
			return true
		}
	}
	return false
}
