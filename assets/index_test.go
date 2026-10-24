package assets

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type mediaMeta struct {
	Credit string
	Tags   []string
}

func TestIndex(t *testing.T) {
	entries := []Entry[mediaMeta]{
		{File: File{Path: "img/a.png", Name: "a.png"}, Meta: mediaMeta{Tags: []string{"logo", "art"}}},
		{File: File{Path: "img/b.png", Name: "b.png"}, Meta: mediaMeta{Tags: []string{"art"}}},
	}

	index, err := NewIndex(entries, IndexOptions[mediaMeta]{
		Tags: func(entry Entry[mediaMeta]) []string { return entry.Meta.Tags },
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := index.Len(); got != 2 {
		t.Errorf("Len = %d", got)
	}
	if got, ok := index.Get("a.png"); !ok || got.File.Path != "img/a.png" {
		t.Errorf("Get(a.png) = %+v, %v", got, ok)
	}
	if _, ok := index.Get("nope.png"); ok {
		t.Error("Get(nope.png) should be false")
	}
	if got := index.All(); len(got) != 2 || got[0].File.Name != "a.png" || got[1].File.Name != "b.png" {
		t.Errorf("All = %+v", got)
	}
	if got, want := index.Tags(), []string{"art", "logo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Tags = %v, want %v", got, want)
	}
}

func TestIndexDuplicateID(t *testing.T) {
	entries := []Entry[mediaMeta]{
		{File: File{Path: "img/a.png", Name: "a.png"}},
		{File: File{Path: "other/a.png", Name: "a.png"}},
	}
	_, err := NewIndex(entries, IndexOptions[mediaMeta]{})
	if err == nil || !strings.Contains(err.Error(), "duplicate ID") {
		t.Fatalf("err = %v", err)
	}
}

func TestIndexCustomKeyAndValidate(t *testing.T) {
	entries := []Entry[mediaMeta]{
		{File: File{Path: "img/a.png", Name: "a.png"}, Meta: mediaMeta{Credit: "ok"}},
		{File: File{Path: "img/b.png", Name: "b.png"}, Meta: mediaMeta{Credit: "bad"}},
	}
	opts := IndexOptions[mediaMeta]{
		Key: func(entry Entry[mediaMeta]) string { return entry.File.Path },
		Validate: func(entry Entry[mediaMeta]) error {
			if entry.Meta.Credit == "bad" {
				return errBadCredit
			}
			return nil
		},
	}
	_, err := NewIndex(entries, opts)
	if err == nil || !strings.Contains(err.Error(), "img/b.png") || !strings.Contains(err.Error(), "bad credit") {
		t.Fatalf("err = %v", err)
	}

	entries[1].Meta.Credit = "ok"
	index, err := NewIndex(entries, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := index.Get("img/b.png"); !ok {
		t.Error("custom path key missing")
	}
}

func TestIndexEmptyID(t *testing.T) {
	entries := []Entry[mediaMeta]{{File: File{Path: "a.png", Name: "a.png"}}}
	_, err := NewIndex(entries, IndexOptions[mediaMeta]{
		Key: func(Entry[mediaMeta]) string { return "" },
	})
	if err == nil || !strings.Contains(err.Error(), "empty ID") {
		t.Fatalf("err = %v", err)
	}
}

func TestIndexNoTags(t *testing.T) {
	index, err := NewIndex([]Entry[mediaMeta]{{File: File{Path: "a.png", Name: "a.png"}}}, IndexOptions[mediaMeta]{})
	if err != nil {
		t.Fatal(err)
	}
	if got := index.Tags(); got != nil {
		t.Errorf("Tags = %v, want nil", got)
	}
}

var errBadCredit = errors.New("bad credit")
