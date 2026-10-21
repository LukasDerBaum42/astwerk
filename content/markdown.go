package content

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/LukasDerBaum42/astwerk/ssg"
	"github.com/a-h/templ"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Components is a name → templ component registry for use inside markdown.
//
// Markdown calls a component with {{@name()}}, or with props in the call:
//
//	{{@callout("note")}}
//	{{@counter(start=5)}}
//	{{@gallery({"type": "concept_art"})}}
//
// Props are decoded as JSON into the registered props type. A bare word is
// treated as a string, so callout(note) and callout("note") are the same.
type Components struct {
	byName map[string]componentResolver
}

type componentResolver func(args []byte) (func(ssg.Ctx) templ.Component, error)

// NewComponents returns an empty registry.
func NewComponents() *Components {
	return &Components{byName: make(map[string]componentResolver)}
}

// Register binds name to a templ component. It panics on an empty or duplicate
// name, which can only be a programming error at start-up.
func Register[T any](cs *Components, name string, render func(ssg.Ctx, T) templ.Component) {
	if cs == nil {
		panic("content: Register on a nil Components")
	}
	if name == "" {
		panic("content: Register with an empty name")
	}
	if _, exists := cs.byName[name]; exists {
		panic(fmt.Sprintf("content: component %q registered twice", name))
	}
	cs.byName[name] = func(args []byte) (func(ssg.Ctx) templ.Component, error) {
		var props T
		if len(bytes.TrimSpace(args)) > 0 {
			if err := json.Unmarshal(args, &props); err != nil {
				return nil, err
			}
		}
		return func(ctx ssg.Ctx) templ.Component { return render(ctx, props) }, nil
	}
}

// WithComponents attaches a registry to the loader. Markdown loaded through
// this loader can call the registered components.
func (l *Loader) WithComponents(cs *Components) *Loader {
	l.components = cs
	return l
}

// installComponentSupport adds the inline parser and node renderer to md.
func installComponentSupport(md goldmark.Markdown) {
	md.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(&componentParser{}, 500),
	))
	md.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&componentRenderer{}, 500),
	))
}

var kindComponent = ast.NewNodeKind("AstwerkComponent")

// componentNode is a parsed {{@name(...)}} call. token is filled in during
// Page.resolveComponents and is what the HTML renderer emits.
type componentNode struct {
	ast.BaseInline
	name  string
	args  []byte
	token string
}

func (n *componentNode) Kind() ast.NodeKind { return kindComponent }

func (n *componentNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{
		"Name":  n.name,
		"Token": n.token,
	}, nil)
}

// componentParser parses {{@name(...)}} on a single line.
type componentParser struct{}

func (p *componentParser) Trigger() []byte { return []byte{'{'} }

func (p *componentParser) Parse(_ ast.Node, reader text.Reader, _ parser.Context) ast.Node {
	line, segment := reader.PeekLine()
	if !bytes.HasPrefix(line, []byte("{{@")) {
		return nil
	}
	end := componentEnd(line)
	if end < 0 {
		return nil
	}
	name, args, ok := parseComponentCall(string(line[3:end]))
	if !ok {
		return nil
	}
	node := &componentNode{name: name, args: args}
	node.SetPos(segment.Start)
	reader.Advance(end + 2)
	return node
}

// componentEnd returns the index of the closing "}}" of a call, ignoring
// braces inside strings and nested JSON.
func componentEnd(line []byte) int {
	depth, inString, escaped := 0, false, false
	for i := 3; i+1 < len(line); i++ {
		c := line[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case '}':
			if depth > 0 {
				depth--
				continue
			}
			if line[i+1] == '}' {
				return i
			}
			return -1
		}
	}
	return -1
}

// parseComponentCall splits name(args) into its parts and turns args into a
// JSON value.
func parseComponentCall(call string) (string, []byte, bool) {
	call = strings.TrimSpace(call)
	if call == "" {
		return "", nil, false
	}
	open := strings.IndexByte(call, '(')
	if open < 0 {
		if !validComponentName(call) {
			return "", nil, false
		}
		return call, nil, true
	}
	if !strings.HasSuffix(call, ")") {
		return "", nil, false
	}
	name := strings.TrimSpace(call[:open])
	if !validComponentName(name) {
		return "", nil, false
	}
	args, err := componentArgs(strings.TrimSpace(call[open+1 : len(call)-1]))
	if err != nil {
		return "", nil, false
	}
	return name, args, true
}

func validComponentName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case i > 0 && (r >= '0' && r <= '9' || r == '-' || r == '.'):
		default:
			return false
		}
	}
	return true
}

// componentArgs turns the call's argument text into a JSON value for the
// props type: a JSON object, a single JSON literal, or key=value pairs.
func componentArgs(args string) ([]byte, error) {
	if args == "" {
		return nil, nil
	}
	if json.Valid([]byte(args)) {
		return []byte(args), nil
	}
	if strings.Contains(args, "=") {
		return namedComponentArgs(args)
	}
	// A bare word is a string: {{@callout(note)}}.
	b, err := json.Marshal(args)
	return b, err
}

func namedComponentArgs(args string) ([]byte, error) {
	parts, err := splitTopLevel(args, ',')
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, part := range parts {
		key, value, found := strings.Cut(part, "=")
		if !found {
			return nil, fmt.Errorf("argument %q is not key=value", part)
		}
		key = strings.TrimSpace(key)
		if !validComponentName(key) {
			return nil, fmt.Errorf("argument name %q is invalid", key)
		}
		if i > 0 {
			buf.WriteByte(',')
		}
		encKey, _ := json.Marshal(key)
		buf.Write(encKey)
		buf.WriteByte(':')
		value = strings.TrimSpace(value)
		if json.Valid([]byte(value)) {
			buf.WriteString(value)
		} else {
			encValue, _ := json.Marshal(value)
			buf.Write(encValue)
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// splitTopLevel splits s on sep, ignoring separators inside quotes, braces and
// brackets.
func splitTopLevel(s string, sep byte) ([]string, error) {
	var parts []string
	start, depth := 0, 0
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		case sep:
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	if inString || depth != 0 {
		return nil, fmt.Errorf("unbalanced arguments")
	}
	parts = append(parts, s[start:])
	return parts, nil
}

// componentRenderer writes the token that Page.Render replaces with the
// component's output.
type componentRenderer struct{}

func (r *componentRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindComponent, func(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			_, _ = w.WriteString(n.(*componentNode).token)
		}
		return ast.WalkContinue, nil
	})
}

var tokenSeq atomic.Uint64

// prepare resolves components and renders the markdown body to HTML. The
// component calls stay as tokens until [Page.Render], where the page's ctx is
// known.
func (p *Page) prepare() error {
	if err := p.resolveComponents(); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := p.loader.markdown.Renderer().Render(&buf, p.source, p.doc); err != nil {
		return fmt.Errorf("markdown: %w", err)
	}
	p.html = buf.String()
	return nil
}

// resolveComponents resolves names and props, assigns tokens, and records the
// component calls in document order.
func (p *Page) resolveComponents() error {
	if p.doc == nil {
		return nil
	}
	var components []func(ssg.Ctx) templ.Component
	nonce := fmt.Sprintf("aw%d", tokenSeq.Add(1))
	var resolveErr error

	ast.Walk(p.doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		call, ok := n.(*componentNode)
		if !ok {
			return ast.WalkContinue, nil
		}
		line := lineOf(p.source, call.Pos())
		if p.loader == nil || p.loader.components == nil {
			resolveErr = fmt.Errorf("line %d: component %q: no component registry configured", line, call.name)
			return ast.WalkStop, resolveErr
		}
		resolve, ok := p.loader.components.byName[call.name]
		if !ok {
			resolveErr = fmt.Errorf("line %d: unknown component %q", line, call.name)
			return ast.WalkStop, resolveErr
		}
		render, err := resolve(call.args)
		if err != nil {
			resolveErr = fmt.Errorf("line %d: component %q: %w", line, call.name, err)
			return ast.WalkStop, resolveErr
		}
		call.token = fmt.Sprintf("<!--%s:c:%d-->", nonce, len(components))
		components = append(components, render)
		return ast.WalkContinue, nil
	})
	if resolveErr != nil {
		return resolveErr
	}

	p.components = components
	p.nonce = nonce
	p.unwrapComponentParagraphs()
	return nil
}

// unwrapComponentParagraphs drops the <p> around a paragraph that contains
// only one component, so block-level components do not nest inside a
// paragraph.
func (p *Page) unwrapComponentParagraphs() {
	var paragraphs []*ast.Paragraph
	_ = ast.Walk(p.doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if para, ok := n.(*ast.Paragraph); ok {
				paragraphs = append(paragraphs, para)
			}
		}
		return ast.WalkContinue, nil
	})
	for _, para := range paragraphs {
		if para.ChildCount() != 1 {
			continue
		}
		child := para.FirstChild()
		if _, ok := child.(*componentNode); !ok {
			continue
		}
		parent := para.Parent()
		if parent != nil {
			parent.ReplaceChild(parent, para, child)
		}
	}
}

// Render returns the page body as a templ component. Components are called
// here, with the page's ssg.Ctx, and root-relative URLs are rewritten for the
// page's depth and base path.
func (p Page) Render(ctx ssg.Ctx) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		if p.html == "" {
			return nil
		}
		rest := p.html
		for i := 0; ; i++ {
			marker := fmt.Sprintf("<!--%s:c:%d-->", p.nonce, i)
			at := strings.Index(rest, marker)
			if at < 0 {
				_, err := io.WriteString(w, rewriteRootURLs(rest, ctx))
				return err
			}
			if _, err := io.WriteString(w, rewriteRootURLs(rest[:at], ctx)); err != nil {
				return err
			}
			if err := p.components[i](ctx).Render(context.Background(), w); err != nil {
				return err
			}
			rest = rest[at+len(marker):]
		}
	})
}

var rootURLPattern = regexp.MustCompile(`(?i)\b(src|href|poster)="/([^"]*)"`)

// rewriteRootURLs rewrites root-relative URLs in rendered markdown to the URL
// form the page needs: base-prefixed, or relative to the page's depth when
// RelativeURLs is on. Markdown is authored against the site root, so
// /img/x.png and /about/ both work from any page.
//
// Protocol-relative URLs (//host/...) are left alone.
func rewriteRootURLs(s string, ctx ssg.Ctx) string {
	return rootURLPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := rootURLPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		path := parts[2]
		if strings.HasPrefix(path, "/") {
			return match
		}
		return parts[1] + `="` + html.EscapeString(ctx.Asset(path)) + `"`
	})
}

func lineOf(source []byte, pos int) int {
	if pos < 0 {
		return 0
	}
	if pos > len(source) {
		pos = len(source)
	}
	return bytes.Count(source[:pos], []byte("\n")) + 1
}
