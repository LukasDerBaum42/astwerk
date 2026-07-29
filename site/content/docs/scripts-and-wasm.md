+++
Title = "Scripts & WebAssembly"
Description = "Compiling Go to wasm as part of the build, with no JS toolchain."
Order = 50
+++

A node with `CompileFrom` compiles a scripts directory into its output
directory. No bundler, no `package.json`.

```go
"scripts": {CompileFrom: "scripts"},
```

## What gets compiled

`ssg.CompileScripts(srcDir, outDir)` looks at each top-level entry:

| Entry | Output |
| --- | --- |
| a `.go` file with `//go:build js && wasm` | `<name>.wasm` |
| a subdirectory containing at least one such file | `<dirname>.wasm` |
| `.ts` files | handed to `tsc` in one batch |

Anything else is ignored, so helper packages and non-wasm Go files can live in
the same directory without confusing the step.

When at least one `.wasm` is produced, Go's own `wasm_exec.js` glue is copied in
beside it from `$(go env GOROOT)/lib/wasm/`.

## The build constraint is required

A script must carry it:

```go
//go:build js && wasm

package main

func main() {
	// …
	select {} // keep the module alive
}
```

Without the constraint, `go build ./...` at your project root would also try to
compile the script for your host platform, where `syscall/js` doesn't exist.
Files lacking it are skipped rather than failing the build, so an ordinary Go
file sitting in `scripts/` is harmless.

## `select {}` at the end of main

Note the `select {}`. When `main` returns, the WASM instance exits and every
event handler dies with it. A script that binds handlers has to block forever.

## Compilation runs in your module

`go build` is invoked from the script's parent directory, so a script can import
your project's own packages:

```go
//go:build js && wasm

package main

import "myproject/shared"
```

## Loading it

Emit the glue and the module. `wasm_exec.js` defines `globalThis.Go`, so it has
to load first — a plain script tag, not a module import:

```templ
templ WasmScript(c ssg.Ctx, name string) {
	<script src={ c.Asset("scripts/wasm_exec.js") }></script>
	<script data-wasm={ c.Asset("scripts/" + name + ".wasm") }>
		(() => {
			const src = document.currentScript.dataset.wasm;
			const go = new Go();
			WebAssembly.instantiateStreaming(fetch(src), go.importObject)
				.then(res => go.run(res.instance))
				.catch(err => console.error("failed to start " + src, err));
		})();
	</script>
}
```

Two details in there are load-bearing:

**The path travels in a `data-` attribute.** templ treats `<script>` contents as
raw text, so an interpolated `{ name }` inside the script body is emitted
literally rather than substituted.

**`document.currentScript` is read synchronously.** It is `null` once a promise
resolves, so it has to be captured before the first `await` or `.then`.

`starter/wasm/wasm_script.templ` ships this component ready to copy.

## Size

A Go wasm binary starts around 2 MB, and a script using `reactive` lands nearer
5 MB. That is the Go runtime, and it compresses to roughly a quarter of that over
the wire. It is a real cost — if a page only needs a nav toggle, ten lines of
hand-written JavaScript is genuinely the better engineering choice. Reach for
wasm when the logic is substantial enough that writing it in Go pays for the
runtime.
