package devserver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

// buildDir is a small site the way ssg.Build would leave one.
func buildDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.html"), "<html><body><h1>home</h1></body></html>")
	writeFile(t, filepath.Join(dir, "about", "index.html"), "<html><body>about</body></html>")
	writeFile(t, filepath.Join(dir, "404.html"), "<html><body>not found</body></html>")
	writeFile(t, filepath.Join(dir, "style", "site.css"), "body{color:red}")
	return dir
}

func get(t *testing.T, srv *httptest.Server, path string) (*http.Response, string) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(b)
}

func testServer(t *testing.T, dir string) (*httptest.Server, *hub) {
	t.Helper()
	h := newHub()
	srv := httptest.NewServer(handler(dir, h))
	t.Cleanup(srv.Close)
	t.Cleanup(h.close)
	return srv, h
}

func TestServesPagesWithTheReloadScriptInjected(t *testing.T) {
	srv, _ := testServer(t, buildDir(t))

	resp, body := get(t, srv, "/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "<h1>home</h1>") {
		t.Errorf("page content missing:\n%s", body)
	}
	if !strings.Contains(body, ReloadPath) {
		t.Errorf("reload script not injected:\n%s", body)
	}
	if i, j := strings.Index(body, ReloadPath), strings.Index(body, "</body>"); i > j {
		t.Errorf("script injected after </body> (at %d, body closes at %d)", i, j)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestServesNestedIndexAndRedirectsDirectories(t *testing.T) {
	dir := buildDir(t)
	srv, _ := testServer(t, dir)

	_, body := get(t, srv, "/about/")
	if !strings.Contains(body, "about") {
		t.Errorf("nested index not served:\n%s", body)
	}

	// A directory without the trailing slash must redirect, or the page's
	// relative asset URLs resolve one level too high.
	client := *srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Get(srv.URL + "/about")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/about/" {
		t.Errorf("Location = %q, want /about/", got)
	}
}

func TestServesAssetsUntouched(t *testing.T) {
	srv, _ := testServer(t, buildDir(t))

	resp, body := get(t, srv, "/style/site.css")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if body != "body{color:red}" {
		t.Errorf("css body = %q, want it unmodified", body)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/css") {
		t.Errorf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}
}

func TestMissingPathServesTheSites404(t *testing.T) {
	srv, _ := testServer(t, buildDir(t))

	resp, body := get(t, srv, "/nope/")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if !strings.Contains(body, "not found") {
		t.Errorf("404.html not served:\n%s", body)
	}
	if !strings.Contains(body, ReloadPath) {
		t.Error("404 page did not get the reload script, so fixing the link needs a manual refresh")
	}
}

func TestMissingPathWithout404File(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.html"), "<html><body>home</body></html>")
	srv, _ := testServer(t, dir)

	resp, _ := get(t, srv, "/nope/")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestPathTraversalIsRefused(t *testing.T) {
	dir := buildDir(t)
	secret := filepath.Join(filepath.Dir(dir), "secret.html")
	writeFile(t, secret, "<html><body>secret</body></html>")
	srv, _ := testServer(t, dir)

	for _, p := range []string{"/../secret.html", "/about/../../secret.html", "/%2e%2e/secret.html"} {
		resp, body := get(t, srv, p)
		if strings.Contains(body, "secret") {
			t.Errorf("%s escaped the build directory (status %d)", p, resp.StatusCode)
		}
	}
}

func TestInjectPlacesTheScript(t *testing.T) {
	cases := []struct{ name, in string }{
		{"lowercase", "<html><body>x</body></html>"},
		{"uppercase", "<HTML><BODY>x</BODY></HTML>"},
		{"no body tag", "<h1>fragment</h1>"},
		{"non-ascii before the tag", "<html><body>Über İstanbul</body></html>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := string(inject([]byte(c.in)))
			if !strings.Contains(out, ReloadPath) {
				t.Fatalf("script not injected:\n%s", out)
			}
			// The original markup must survive intact around the insertion.
			stripped := strings.Replace(out, clientScript, "", 1)
			if stripped != c.in {
				t.Errorf("markup changed:\n got %q\nwant %q", stripped, c.in)
			}
		})
	}
}

// readEvent reads one SSE frame: an event name and its (possibly multi-line)
// data, terminated by a blank line.
func readEvent(t *testing.T, r *bufio.Reader) (name, data string) {
	t.Helper()
	var lines []string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("reading SSE stream: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if name != "" || len(lines) > 0 {
				return name, strings.Join(lines, "\n")
			}
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			lines = append(lines, strings.TrimPrefix(line, "data: "))
		}
	}
}

func openStream(t *testing.T, srv *httptest.Server) *bufio.Reader {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+ReloadPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	resp, err := srv.Client().Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })

	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	r := bufio.NewReader(resp.Body)
	// The server sends a retry hint first; consume it so the reader is parked
	// on the stream before the test publishes anything.
	if _, err := r.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReloadEventReachesTheBrowser(t *testing.T) {
	srv, h := testServer(t, buildDir(t))

	// The handler subscribes before it writes anything, so a stream that has
	// delivered its retry hint is already registered.
	r := openStream(t, srv)
	h.reload()

	name, data := readEvent(t, r)
	if name != "reload" {
		t.Errorf("event = %q, want reload", name)
	}
	if data != "ok" {
		t.Errorf("data = %q, want ok", data)
	}
}

// A browser connecting while the build is broken has to be told, or it shows a
// stale page with no explanation.
func TestBuildErrorIsReplayedToANewSubscriber(t *testing.T) {
	srv, h := testServer(t, buildDir(t))
	h.buildError("main.go:12: undefined: foo\nmain.go:13: undefined: bar")

	name, data := readEvent(t, openStream(t, srv))
	if name != "builderror" {
		t.Fatalf("event = %q, want builderror", name)
	}
	if data != "main.go:12: undefined: foo\nmain.go:13: undefined: bar" {
		t.Errorf("data = %q, want both error lines", data)
	}
}

func TestSuccessfulBuildClearsTheStoredError(t *testing.T) {
	h := newHub()
	defer h.close()

	h.buildError("boom")
	h.reload()

	events, release := h.subscribe()
	defer release()
	select {
	case e := <-events:
		t.Errorf("new subscriber got a replayed %s after a successful build", e.name)
	default:
	}
}

func TestHubDeliversToEverySubscriber(t *testing.T) {
	h := newHub()
	defer h.close()

	a, releaseA := h.subscribe()
	b, releaseB := h.subscribe()
	defer releaseA()
	defer releaseB()

	h.reload()
	for i, ch := range []<-chan event{a, b} {
		select {
		case e := <-ch:
			if e.name != "reload" {
				t.Errorf("subscriber %d got %q", i, e.name)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d got nothing", i)
		}
	}

	releaseA()
	h.reload() // must not panic on the closed subscriber
}

func TestWatcherReportsCreateModifyDelete(t *testing.T) {
	dir := t.TempDir()
	w := newWatcher(Config{Dir: filepath.Join(dir, "build"), Watch: []string{dir},
		Exts: DefaultExts, Ignore: DefaultIgnore})
	w.scan()

	page := filepath.Join(dir, "content", "post.md")
	writeFile(t, page, "one")
	if got := w.scan(); len(got) != 1 || got[0] != page {
		t.Fatalf("create: changed = %v, want [%s]", got, page)
	}
	if got := w.scan(); len(got) != 0 {
		t.Fatalf("second scan reported %v, want nothing", got)
	}

	// Size is part of the stamp, so a same-second edit of a different length
	// registers without waiting for the clock.
	writeFile(t, page, "one two three")
	if got := w.scan(); len(got) != 1 || got[0] != page {
		t.Fatalf("modify: changed = %v, want [%s]", got, page)
	}

	if err := os.Remove(page); err != nil {
		t.Fatal(err)
	}
	if got := w.scan(); len(got) != 1 || got[0] != page {
		t.Fatalf("delete: changed = %v, want [%s]", got, page)
	}
}

func TestWatcherIgnoresUnwatchedFilesAndDirs(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "build")
	w := newWatcher(Config{Dir: out, Watch: []string{dir}, Exts: DefaultExts, Ignore: DefaultIgnore})
	w.scan()

	// The build's own output must not trigger the next build.
	writeFile(t, filepath.Join(out, "index.html"), "<html></html>")
	writeFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/master")
	writeFile(t, filepath.Join(dir, "node_modules", "x", "index.js"), "module.exports={}")
	writeFile(t, filepath.Join(dir, "notes.txt"), "not a source file")
	writeFile(t, filepath.Join(dir, "photo.png"), "\x89PNG")

	if got := w.scan(); len(got) != 0 {
		t.Errorf("changed = %v, want nothing", got)
	}
}

func TestWatcherWatchesADotDirectoryAskedForByName(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, ".github")
	writeFile(t, filepath.Join(root, "ci.yml"), "name: CI")

	w := newWatcher(Config{Dir: filepath.Join(dir, "build"), Watch: []string{root},
		Exts: DefaultExts, Ignore: DefaultIgnore})
	if got := w.scan(); len(got) != 1 {
		t.Fatalf("first scan = %v, want the one file in the watched root", got)
	}
}

func TestWatcherSurvivesAMissingRoot(t *testing.T) {
	w := newWatcher(Config{Dir: "build", Watch: []string{filepath.Join(t.TempDir(), "nope")},
		Exts: DefaultExts, Ignore: DefaultIgnore})
	if got := w.scan(); len(got) != 0 {
		t.Errorf("changed = %v, want nothing", got)
	}
}

func TestRunRequiresABuildFunc(t *testing.T) {
	err := Run(t.Context(), Config{})
	if err == nil || !strings.Contains(err.Error(), "Build") {
		t.Errorf("err = %v, want a complaint about Config.Build", err)
	}
}

// The whole loop: build, serve, notice an edit, rebuild, tell the browser.
func TestRunRebuildsAndReloads(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "content", "home.md")
	out := filepath.Join(dir, "build")
	writeFile(t, src, "first")

	var mu sync.Mutex
	builds := 0
	build := func() error {
		mu.Lock()
		defer mu.Unlock()
		builds++
		body, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		writeFile(t, filepath.Join(out, "index.html"),
			fmt.Sprintf("<html><body>%s</body></html>", body))
		return nil
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	addrs := make(chan string, 1)
	go Run(ctx, Config{
		Build:    build,
		Dir:      out,
		Addr:     "localhost:0",
		Watch:    []string{dir},
		Interval: 10 * time.Millisecond,
		Log:      log.New(io.Discard, "", 0),
		Ready:    func(addr string) { addrs <- addr },
	})

	var addr string
	select {
	case addr = <-addrs:
	case <-time.After(10 * time.Second):
		t.Fatal("server never became ready")
	}
	base := "http://" + addr

	body := fetch(t, base+"/")
	if !strings.Contains(body, "first") {
		t.Fatalf("initial page = %q, want the first build", body)
	}

	writeFile(t, src, "second edit")

	deadline := time.Now().Add(10 * time.Second)
	for {
		if body := fetch(t, base+"/"); strings.Contains(body, "second edit") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the edit was never picked up")
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	got := builds
	mu.Unlock()
	if got < 2 {
		t.Errorf("ran %d builds, want at least 2", got)
	}
}

// A failing build keeps the server up and reports the failure to the browser.
func TestRunSurvivesAFailingBuild(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "build")
	writeFile(t, filepath.Join(out, "index.html"), "<html><body>stale</body></html>")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	addrs := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{
			Build:    func() error { return errors.New("undefined: foo") },
			Dir:      out,
			Addr:     "localhost:0",
			Watch:    []string{dir},
			Interval: 10 * time.Millisecond,
			Log:      log.New(io.Discard, "", 0),
			Ready:    func(addr string) { addrs <- addr },
		})
	}()

	var addr string
	select {
	case addr = <-addrs:
	case err := <-done:
		t.Fatalf("Run returned early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("server never became ready")
	}

	if body := fetch(t, "http://"+addr+"/"); !strings.Contains(body, "stale") {
		t.Errorf("server stopped serving after a failed build: %q", body)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+ReloadPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	r := bufio.NewReader(resp.Body)
	r.ReadString('\n')
	r.ReadString('\n')
	name, data := readEvent(t, r)
	if name != "builderror" || !strings.Contains(data, "undefined: foo") {
		t.Errorf("event = %q %q, want the build error", name, data)
	}
}

func fetch(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
