+++
Title = "Getting started"
Description = "Install astwerk, write a tree, generate a site."
Order = 10
+++

astwerk generates a static site from a Go program. You write a tree describing
the output directory; it walks the tree and writes files. Templates are
[templ](https://templ.guide) components — real Go, type-checked at compile time.

## Install

```
go get github.com/LukasDerBaum42/astwerk
```

You also need the templ CLI to compile `.templ` files into Go:

```
go install github.com/a-h/templ/cmd/templ@latest
```

That's the whole toolchain. No Node.js, no bundler, no plugin system.

## A minimal site

Three files. First a template, `views/page.templ`:

```templ
package views

templ Page(title string, path string, prefix string) {
	<!DOCTYPE html>
	<html lang="en">
		<head>
			<meta charset="utf-8"/>
			<title>{ title }</title>
		</head>
		<body>
			<h1>{ title }</h1>
			{ children... }
		</body>
	</html>
}
```

Then `main.go`:

```go
package main

import (
	"log"
	"os"

	"github.com/LukasDerBaum42/astwerk/ssg"
	"myproject/views"
)

func main() {
	dev := len(os.Args) > 1 && os.Args[1] == "--dev"

	root := ssg.Node{
		Title: "Home",
		Page:  ssg.Templ(views.Page),
		Children: map[string]ssg.Node{
			"about": {Title: "About", Page: ssg.Templ(views.Page)},
			"style": {CopyFrom: "style"},
		},
	}

	if err := ssg.Build(root, ssg.BuildOptions{Dev: dev}); err != nil {
		log.Fatal(err)
	}
}
```

Then build:

```
templ generate
go run .
```

You get:

```
build/
  index.html
  about/index.html
  style/…
```

## What just happened

`Build` walked the tree. For each node it made the directory, rendered `Page`
into `index.html`, and recursed into `Children` using the map keys as directory
names. `CopyFrom` copied a directory in as-is.

`ssg.Templ` is the adapter that makes `views.Page` usable as a page. The walker
knows each page's path and locale, so it fills those arguments in — you supply
only the title. That's covered in [The Node tree](../the-node-tree/).

## Dev mode

`BuildOptions.Dev` skips deleting the output directory and overwrites files in
place, so a running dev server keeps its file handles and inodes:

```
go run . --dev
```

Without it, `Build` removes the output directory first so stale files can't
survive a rename.

## Serving it locally

Any static file server works. Python's is already installed on most systems:

```
cd build && python3 -m http.server 8000
```

## Where to go next

[The Node tree](../the-node-tree/) is the one page worth reading in full —
everything else builds on it. After that, pick what you need:
[markdown content](../content-and-collections/), [locales](../i18n/),
[client-side Go](../wasmwrap/).
