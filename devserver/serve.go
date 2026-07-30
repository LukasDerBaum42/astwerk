package devserver

import (
	"bytes"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// handler serves dir, with the live-reload endpoint mounted alongside it.
func handler(dir string, h *hub) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(ReloadPath, h.serveEvents)
	mux.Handle("/", &files{dir: dir})
	return mux
}

// files serves the build directory the way a static host does — a directory
// means its index.html, a miss means the site's own 404 page — with the reload
// script injected into every HTML response.
type files struct{ dir string }

func (f *files) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Nothing here is cacheable: the whole point is seeing the last build.
	w.Header().Set("Cache-Control", "no-store")

	full, ok := f.resolve(r.URL.Path)
	if !ok {
		f.notFound(w, r)
		return
	}

	info, err := os.Stat(full)
	if err != nil {
		f.notFound(w, r)
		return
	}

	if info.IsDir() {
		// Without the trailing slash the browser resolves the page's relative
		// URLs against its parent, which breaks every asset on the page.
		if !strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			return
		}
		full = filepath.Join(full, "index.html")
		if info, err = os.Stat(full); err != nil || info.IsDir() {
			f.notFound(w, r)
			return
		}
	}

	if !isHTML(full) {
		http.ServeFile(w, r, full)
		return
	}
	body, err := os.ReadFile(full)
	if err != nil {
		f.notFound(w, r)
		return
	}
	writeHTML(w, http.StatusOK, inject(body))
}

// notFound serves the site's own 404.html when it has one, so the page a
// visitor would really get is the page you see while developing.
func (f *files) notFound(w http.ResponseWriter, r *http.Request) {
	body, err := os.ReadFile(filepath.Join(f.dir, "404.html"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writeHTML(w, http.StatusNotFound, inject(body))
}

// resolve maps a URL path onto a path inside dir, refusing anything that would
// escape it.
func (f *files) resolve(urlPath string) (string, bool) {
	rel := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	if rel == "" {
		return f.dir, true
	}
	local, err := filepath.Localize(rel)
	if err != nil {
		return "", false
	}
	return filepath.Join(f.dir, local), true
}

func writeHTML(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(body)
}

func isHTML(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".html" || ext == ".htm"
}

var bodyClose = []byte("</body>")

// inject puts the reload script into a page, before the closing body tag where
// there is one and at the end otherwise — a fragment without <body> is still
// something a browser will run a script in.
func inject(body []byte) []byte {
	script := []byte(clientScript)
	i := lastIndexFold(body, bodyClose)
	if i < 0 {
		return append(body, script...)
	}
	out := make([]byte, 0, len(body)+len(script))
	out = append(out, body[:i]...)
	out = append(out, script...)
	return append(out, body[i:]...)
}

// lastIndexFold is bytes.LastIndex, case-insensitively. Lowercasing the whole
// page first would be simpler and wrong: bytes.ToLower can change the length of
// non-ASCII text, and every index after that point would be off.
func lastIndexFold(haystack, needle []byte) int {
	for i := len(haystack) - len(needle); i >= 0; i-- {
		if bytes.EqualFold(haystack[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}

// clientScript is vendored, not fetched: it is a file astwerk ships and you can
// read in one sitting, in the same spirit as wasm_exec.js being copied in.
//
// It reconnects on its own — EventSource does that natively, and the retry hint
// the server sends makes the gap short — so restarting the dev server does not
// mean touching the browser.
const clientScript = `<script>
(function () {
	var overlay;

	function show(message) {
		if (!overlay) {
			overlay = document.createElement("pre");
			overlay.setAttribute("data-astwerk-devserver", "error");
			overlay.style.cssText =
				"position:fixed;inset:0;z-index:2147483647;margin:0;padding:2rem;" +
				"overflow:auto;white-space:pre-wrap;background:#1b1b1f;color:#ffb4a2;" +
				"font:14px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace";
			document.body.appendChild(overlay);
		}
		overlay.textContent = "Build failed\n\n" + message;
	}

	var source = new EventSource("` + ReloadPath + `");
	source.addEventListener("reload", function () { location.reload(); });
	source.addEventListener("builderror", function (e) { show(e.data); });
})();
</script>
`
