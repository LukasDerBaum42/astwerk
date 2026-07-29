package templates

import (
	"html"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// Formatter is shared with the markdown pipeline so snippets embedded in templ
// and snippets in .md files come out looking identical. Classes rather than
// inline styles, so one stylesheet drives both and dark mode can be a media
// query.
var Formatter = chromahtml.New(chromahtml.WithClasses(true))

// StyleName is the chroma style the stylesheet in style/code.css was generated
// from. Change both together.
const StyleName = "github"

// HighlightGo renders Go source as highlighted HTML.
//
// It falls back to escaped plain text rather than returning an error: a snippet
// chroma can't lex should still be readable, and a docs page shouldn't fail to
// build over syntax colouring.
func HighlightGo(src string) string {
	src = strings.TrimSpace(src)
	fallback := "<pre class=\"chroma\"><code>" + html.EscapeString(src) + "</code></pre>"

	lexer := lexers.Get("go")
	style := styles.Get(StyleName)
	if lexer == nil || style == nil {
		return fallback
	}

	it, err := lexer.Tokenise(nil, src)
	if err != nil {
		return fallback
	}

	var sb strings.Builder
	if err := Formatter.Format(&sb, style, it); err != nil {
		return fallback
	}
	return sb.String()
}
