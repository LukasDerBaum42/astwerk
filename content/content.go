// Package content reads markdown files with TOML front matter.
//
// Front matter is kept as a generic map rather than a fixed struct, so each
// project decodes it into whatever struct its own layouts need via [Decode].
package content

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/renderer/html"
)

// Delimiter marks the start and end of a front matter block.
const Delimiter = "+++"

// Page is one parsed markdown file.
type Page struct {
	// Meta is the TOML front matter found between +++ markers, or nil if the
	// file had none.
	Meta map[string]any

	// HTML is the markdown body rendered to HTML.
	HTML string

	// raw is the front matter source, kept so Decode can unmarshal it directly
	// instead of round-tripping through Meta.
	raw string
}

// DefaultMarkdown renders with raw HTML passed through, so a page can drop into
// hand-written markup where markdown isn't enough.
//
// Pass a different goldmark to [NewLoader] to add extensions — syntax
// highlighting, tables, footnotes, anchored headings.
func DefaultMarkdown() goldmark.Markdown {
	return goldmark.New(goldmark.WithRendererOptions(html.WithUnsafe()))
}

// Loader reads markdown with a particular goldmark configuration. The
// package-level [Load], [LoadDir] and [Parse] use [DefaultMarkdown]; make a
// Loader when you need extensions:
//
//	md := goldmark.New(
//		goldmark.WithExtensions(highlighting.NewHighlighting(...)),
//		goldmark.WithRendererOptions(html.WithUnsafe()),
//	)
//	pages, err := content.NewLoader(md).LoadDir("content/docs")
type Loader struct {
	markdown goldmark.Markdown
}

// NewLoader returns a Loader using md. A nil md means [DefaultMarkdown].
func NewLoader(md goldmark.Markdown) *Loader {
	if md == nil {
		md = DefaultMarkdown()
	}
	return &Loader{markdown: md}
}

var defaultLoader = NewLoader(nil)

// Load reads one markdown file, splits its front matter and converts the body.
func Load(path string) (Page, error) { return defaultLoader.Load(path) }

// Parse does Load's work on an in-memory document.
func Parse(src string) (Page, error) { return defaultLoader.Parse(src) }

// LoadDir reads every *.md file directly inside dir (non-recursive) and returns
// them keyed by file name without the extension — the page's slug.
func LoadDir(dir string) (map[string]Page, error) { return defaultLoader.LoadDir(dir) }

// Load reads one markdown file, splits its front matter and converts the body.
func (l *Loader) Load(path string) (Page, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Page{}, err
	}
	p, err := l.Parse(string(b))
	if err != nil {
		return Page{}, fmt.Errorf("content: %s: %w", path, err)
	}
	return p, nil
}

// Parse does Load's work on an in-memory document.
func (l *Loader) Parse(src string) (Page, error) {
	raw, body := splitFrontMatter(src)

	var meta map[string]any
	if raw != "" {
		if _, err := toml.Decode(raw, &meta); err != nil {
			return Page{}, fmt.Errorf("front matter: %w", err)
		}
	}

	var buf bytes.Buffer
	if err := l.markdown.Convert([]byte(body), &buf); err != nil {
		return Page{}, fmt.Errorf("markdown: %w", err)
	}

	return Page{Meta: meta, HTML: buf.String(), raw: raw}, nil
}

// LoadDir reads every *.md file directly inside dir (non-recursive) and returns
// them keyed by file name without the extension — the page's slug.
func (l *Loader) LoadDir(dir string) (map[string]Page, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	pages := make(map[string]Page)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p, err := l.Load(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		pages[strings.TrimSuffix(e.Name(), ".md")] = p
	}
	return pages, nil
}

// Slugs returns the keys of a [LoadDir] result in sorted order.
//
// Ranging over the map directly gives a different order on every build, which
// shuffles any index page built from it; range over Slugs instead.
func Slugs(pages map[string]Page) []string {
	return slices.Sorted(maps.Keys(pages))
}

// Decode unmarshals p's front matter into a caller-defined struct. A page with
// no front matter decodes to the zero value of T without error.
func Decode[T any](p Page) (T, error) {
	var out T
	if p.raw != "" {
		_, err := toml.Decode(p.raw, &out)
		return out, err
	}
	if len(p.Meta) == 0 {
		return out, nil
	}
	// Page was built by hand rather than parsed; go back through TOML so the
	// caller's struct tags still apply.
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(p.Meta); err != nil {
		return out, fmt.Errorf("content: re-encode front matter: %w", err)
	}
	_, err := toml.Decode(buf.String(), &out)
	return out, err
}

// splitFrontMatter returns the raw front matter and the markdown body. A
// document without a leading +++ block is all body.
func splitFrontMatter(src string) (front, body string) {
	rest := strings.TrimLeft(src, " \t\r\n")
	if !strings.HasPrefix(rest, Delimiter) {
		return "", src
	}
	rest = rest[len(Delimiter):]

	end := strings.Index(rest, Delimiter)
	if end < 0 {
		return "", src // unterminated block: treat the whole file as content
	}
	return rest[:end], rest[end+len(Delimiter):]
}
