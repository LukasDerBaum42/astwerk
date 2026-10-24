package devserver

import (
	"io"
	"log"
	"strings"
	"testing"
)

func TestChangesPaths(t *testing.T) {
	c := changes{
		added:    []string{"b.md"},
		modified: []string{"a.md"},
		removed:  []string{"c.md"},
	}
	got := strings.Join(c.paths(), ",")
	if want := "a.md,b.md,c.md"; got != want {
		t.Errorf("paths = %q, want %q", got, want)
	}
}

func TestBuildChangedCallback(t *testing.T) {
	var changed []string
	var full bool
	calls := 0

	cfg := Config{BuildChanged: func(c []string, f bool) error {
		calls++
		changed = c
		full = f
		return nil
	}}
	if err := cfg.applyDefaults(); err != nil {
		t.Fatal(err)
	}
	lg := newLogger(log.New(io.Discard, "", 0), true)
	hub := newHub(lg)
	defer hub.close()

	if ok := runBuild(cfg, lg, hub, nil, true); !ok {
		t.Fatal("initial build failed")
	}
	if calls != 1 || !full || changed != nil {
		t.Errorf("initial: calls=%d full=%v changed=%v", calls, full, changed)
	}

	if ok := runBuild(cfg, lg, hub, []string{"a.md"}, false); !ok {
		t.Fatal("incremental build failed")
	}
	if calls != 2 || full || len(changed) != 1 || changed[0] != "a.md" {
		t.Errorf("incremental: calls=%d full=%v changed=%v", calls, full, changed)
	}
}

func TestConfigBuildValidation(t *testing.T) {
	var cfg Config
	if err := cfg.applyDefaults(); err == nil {
		t.Fatal("expected an error with neither Build nor BuildChanged")
	}

	cfg.Build = func() error { return nil }
	if err := cfg.applyDefaults(); err != nil {
		t.Fatalf("Build alone should be valid: %v", err)
	}

	cfg.BuildChanged = func([]string, bool) error { return nil }
	if err := cfg.applyDefaults(); err == nil {
		t.Fatal("expected an error with both Build and BuildChanged")
	}
}
