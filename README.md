# gossg

A minimal, no-magic static site generator library for Go + [templ](https://templ.guide).
No Node.js, no JS build tooling; optional client-side scripting via Go→WASM.

Design law: **abstract only the repeating boilerplate.** If a project needs
something the tree can't express, it falls back to plain Go — never a wall it
can't get past.

```
go get github.com/LukasDerBaum42/gossg
```

## Status

Implemented: §2 `ssg.Node`/`Build`, §3 `content`, §4 `BuildLocales`,
§5 `CompileScripts`. Still to come: §6 `wasmwrap`, §7 `starter/`,
§6a `wasmwrap/reactive`.

## The whole mechanism

Describe the output filesystem as a tree, then build it:

```go
func main() {
	dev := len(os.Args) > 1 && os.Args[1] == "--dev"

	root := ssg.Node{
		Page: RenderHome,
		Files: map[string]func() templ.Component{
			"404.html": RenderNotFound,
		},
		Children: map[string]ssg.Node{
			"links":    {Page: RenderLinks},
			"about":    {Page: RenderAbout},
			"projects": {Page: RenderProjectsIndex, Generate: GenerateProjects},
			"style":    {CopyFrom: "style"},
			"public":   {CopyFrom: "public"},
			"scripts":  {CompileFrom: "scripts"},
		},
	}
	root = ssg.BuildLocales(root, []ssg.Locale{
		{Code: "de", Prefix: "/de", Tree: GermanTree},
	})

	if err := ssg.Build(root, ssg.BuildOptions{Dev: dev}); err != nil {
		log.Fatal(err)
	}
}
```

### `ssg.Node`

Every field is optional; the zero value just recurses into `Children`.

| Field | Effect at this node's directory |
| --- | --- |
| `Page` | renders `index.html` |
| `Files` | renders extra files by name (`404.html`, `rss.xml`, `feed/rss.xml`) |
| `Generate` | returns children computed at build time; merged into `Children`, generated keys win |
| `CopyFrom` | copies a directory's contents in as-is |
| `CompileFrom` | compiles a scripts directory in (see below) |
| `Children` | static subdirectories, keyed by name |

`Build` visits children in sorted key order, so output is reproducible. Keys may
contain slashes (`"assets/style"`) but may not escape the output directory.

`BuildOptions.Dev` mirrors the usual `--dev` flag: skip the full clean of
`OutDir` and overwrite files in place, so a running dev server keeps its handles.

## Content

`content` reads markdown with TOML front matter between `+++` markers, and keeps
front matter generic so each project decodes it into its own struct:

```go
type FrontMatter struct {
	Title       string
	Description string
}

func GenerateProjects() map[string]ssg.Node {
	pages, err := content.LoadDir("content/projects")
	if err != nil {
		log.Fatal(err)
	}

	out := map[string]ssg.Node{}
	for _, slug := range content.Slugs(pages) { // sorted: map order would shuffle the index
		p := pages[slug]
		fm, _ := content.Decode[FrontMatter](p)
		out[slug] = ssg.Node{Page: func() templ.Component {
			return templates.BaseLayout(fm.Title, ..., src.ProjectPage(p.HTML))
		}}
	}
	return out
}
```

`LoadDir` is non-recursive and reads `*.md` only, keyed by filename without the
extension. Markdown renders through goldmark with raw HTML passed through, so a
page can drop into hand-written markup where markdown isn't enough.

## i18n

`BuildLocales` mounts each locale's subtree under `Children[Code]`, replacing a
hand-written `build_<lang>` function per language. `Locale.Prefix` is not read by
`BuildLocales` — it is there so a project can keep a locale's code, URL prefix
and tree in one literal and thread the prefix into its own layouts.

## Scripts

`CompileFrom` runs `ssg.CompileScripts`, which compiles a scripts directory:

- a top-level `.go` file carrying `//go:build js && wasm` → `<name>.wasm`
- a subdirectory containing at least one such file → `<dirname>.wasm`
- `.ts` files → `tsc`, in one batch

Anything else is ignored, so helper packages can live alongside. The build runs
from the entry's parent directory, so a script can import the project's own
packages. When a `.wasm` is produced, Go's `wasm_exec.js` glue is copied in
beside it.

The build constraint is required: without one, `go build ./...` at the project
root would also try to compile the script for the host platform.

## Non-goals

No config file format, no reflection magic, no enforced project structure beyond
the `Node` tree. Directory names, file layout and template structure stay
entirely up to the consuming project.

## License

MIT — see [LICENSE](LICENSE).
