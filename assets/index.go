package assets

import (
	"fmt"
	"sort"
	"strings"
)

// IndexOptions configures [NewIndex]. All fields are optional.
type IndexOptions[T any] struct {
	// Key is the entry's ID. Defaults to Entry.File.Name, so a file name must be
	// unique across the scanned tree. Use Entry.File.Path for path-as-ID.
	Key func(Entry[T]) string

	// Tags extracts tags from an entry, for [Index.Tags]. Nil means Tags returns
	// nil.
	Tags func(Entry[T]) []string

	// Validate runs for each entry before it is indexed. A returned error stops
	// NewIndex and names the file and ID.
	Validate func(Entry[T]) error
}

// Index is an ID-indexed view of a scan, with duplicate detection. It is the
// generic part of an asset library: the key, tag extraction and validation are
// the caller's policy.
type Index[T any] struct {
	byID  map[string]Entry[T]
	ids   []string
	tags  func(Entry[T]) []string
	count int
}

// NewIndex builds an index over entries. Duplicate IDs and validation errors
// stop it with an error naming both files.
//
//	entries, _ := assets.LoadTree[Media]("public/img", assets.Options[Media]{})
//	index, err := assets.NewIndex(entries, assets.IndexOptions[Media]{
//		Tags: func(e assets.Entry[Media]) []string { return e.Meta.Tags },
//	})
func NewIndex[T any](entries []Entry[T], opts IndexOptions[T]) (*Index[T], error) {
	key := opts.Key
	if key == nil {
		key = func(entry Entry[T]) string { return entry.File.Name }
	}
	index := &Index[T]{
		byID: make(map[string]Entry[T], len(entries)),
		tags: opts.Tags,
	}

	for _, entry := range entries {
		id := key(entry)
		if id == "" {
			return nil, fmt.Errorf("assets: %s has an empty ID", entry.File.Path)
		}
		if previous, exists := index.byID[id]; exists {
			return nil, fmt.Errorf("assets: duplicate ID %q: %s and %s",
				id, previous.File.Path, entry.File.Path)
		}
		if opts.Validate != nil {
			if err := opts.Validate(entry); err != nil {
				return nil, fmt.Errorf("assets: %s (%s): %w", entry.File.Path, id, err)
			}
		}
		index.byID[id] = entry
		index.ids = append(index.ids, id)
	}

	sort.Strings(index.ids)
	index.count = len(index.ids)
	return index, nil
}

// Get returns the entry with the given ID.
func (i *Index[T]) Get(id string) (Entry[T], bool) {
	entry, ok := i.byID[id]
	return entry, ok
}

// All returns every entry, sorted by ID.
func (i *Index[T]) All() []Entry[T] {
	out := make([]Entry[T], 0, len(i.ids))
	for _, id := range i.ids {
		out = append(out, i.byID[id])
	}
	return out
}

// Tags returns the distinct tags from IndexOptions.Tags, trimmed and sorted.
// It returns nil when no Tags function was configured.
func (i *Index[T]) Tags() []string {
	if i.tags == nil {
		return nil
	}
	seen := make(map[string]bool)
	for _, id := range i.ids {
		for _, tag := range i.tags(i.byID[id]) {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				seen[tag] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for tag := range seen {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

// Len returns the number of indexed entries.
func (i *Index[T]) Len() int { return i.count }
