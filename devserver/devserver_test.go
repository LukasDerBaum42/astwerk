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
	"regexp"
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
	h := newHub(newLogger(log.New(io.Discard, "", 0), true))
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
	h := newHub(newLogger(log.New(io.Discard, "", 0), true))
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
	h := newHub(newLogger(log.New(io.Discard, "", 0), true))
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

// The three kinds must be told apart, because the log reports them differently
// and a deletion is not an edit.
func TestWatcherReportsCreateModifyDelete(t *testing.T) {
	dir := t.TempDir()
	w := newWatcher(Config{Dir: filepath.Join(dir, "build"), Watch: []string{dir},
		Exts: DefaultExts, Ignore: DefaultIgnore})
	w.scan()

	only := func(t *testing.T, kind string, got []string, other ...[]string) {
		t.Helper()
		if len(got) != 1 {
			t.Fatalf("%s: got %v, want exactly one path", kind, got)
		}
		for _, o := range other {
			if len(o) != 0 {
				t.Fatalf("%s: also reported %v", kind, o)
			}
		}
	}

	page := filepath.Join(dir, "content", "post.md")
	writeFile(t, page, "one")
	c := w.scan()
	only(t, "create", c.added, c.modified, c.removed)
	if c.added[0] != page {
		t.Fatalf("create: added = %v, want [%s]", c.added, page)
	}

	if c := w.scan(); !c.empty() {
		t.Fatalf("second scan reported %d changes, want none", c.count())
	}

	// Size is part of the stamp, so a same-second edit of a different length
	// registers without waiting for the clock.
	writeFile(t, page, "one two three")
	c = w.scan()
	only(t, "modify", c.modified, c.added, c.removed)

	if err := os.Remove(page); err != nil {
		t.Fatal(err)
	}
	c = w.scan()
	only(t, "delete", c.removed, c.added, c.modified)
	if c.first() != page {
		t.Errorf("first() = %q, want %q", c.first(), page)
	}
}

// first() names a modification ahead of an addition, since an edit-save loop is
// what the summary line is usually about.
func TestChangesFirstPrefersAModification(t *testing.T) {
	c := changes{added: []string{"new.md"}, modified: []string{"edited.md"}, removed: []string{"gone.md"}}
	if got := c.first(); got != "edited.md" {
		t.Errorf("first() = %q, want edited.md", got)
	}
	if got := (changes{added: []string{"new.md"}}).first(); got != "new.md" {
		t.Errorf("first() with only an addition = %q", got)
	}
	if got := (changes{}).first(); got != "" {
		t.Errorf("first() on nothing = %q, want empty", got)
	}
	if !(changes{}).empty() || (changes{added: []string{"x"}}).empty() {
		t.Error("empty() is wrong")
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

	if c := w.scan(); !c.empty() {
		t.Errorf("changed = %d files, want nothing", c.count())
	}
}

func TestWatcherWatchesADotDirectoryAskedForByName(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, ".github")
	writeFile(t, filepath.Join(root, "ci.yml"), "name: CI")

	w := newWatcher(Config{Dir: filepath.Join(dir, "build"), Watch: []string{root},
		Exts: DefaultExts, Ignore: DefaultIgnore})
	if c := w.scan(); c.count() != 1 {
		t.Fatalf("first scan = %d files, want the one file in the watched root", c.count())
	}
}

func TestWatcherSurvivesAMissingRoot(t *testing.T) {
	w := newWatcher(Config{Dir: "build", Watch: []string{filepath.Join(t.TempDir(), "nope")},
		Exts: DefaultExts, Ignore: DefaultIgnore})
	if c := w.scan(); !c.empty() {
		t.Errorf("changed = %d files, want nothing", c.count())
	}
}

func TestLogFormat(t *testing.T) {
	var buf strings.Builder
	lg := newLogger(log.New(&buf, "", 0), true)
	lg.event(colorGreen, tagBuild, "ok (%s)", 42*time.Millisecond)

	line := strings.TrimRight(buf.String(), "\n")
	if strings.Contains(line, "\033[") {
		t.Errorf("colour leaked into a non-terminal log: %q", line)
	}
	// [15:04:05] [BUILD] ok (42ms)
	want := regexp.MustCompile(`^\[\d\d:\d\d:\d\d\] \[BUILD\] ok \(42ms\)$`)
	if !want.MatchString(line) {
		t.Errorf("line = %q, want it to match %s", line, want)
	}
}

func TestLogColorIsOffWhenNotATerminal(t *testing.T) {
	if newLogger(log.New(io.Discard, "", 0), false).color {
		t.Error("colour enabled for a non-file writer")
	}
	if newLogger(log.New(os.Stderr, "", 0), true).color {
		t.Error("NoColor was ignored")
	}
	t.Setenv("NO_COLOR", "1")
	if newLogger(log.New(os.Stderr, "", 0), false).color {
		t.Error("NO_COLOR was ignored")
	}
}

// A big batch is summarised rather than scrolling the terminal away.
func TestLogChangesSummarisesLargeBatches(t *testing.T) {
	var buf strings.Builder
	lg := newLogger(log.New(&buf, "", 0), true)

	var many changes
	for i := range 25 {
		many.modified = append(many.modified, fmt.Sprintf("f%02d.md", i))
	}
	logChanges(lg, many)

	if n := strings.Count(buf.String(), "\n"); n != 1 {
		t.Errorf("logged %d lines for 25 files, want a single summary", n)
	}
	if !strings.Contains(buf.String(), "25 files") {
		t.Errorf("summary does not give the count: %q", buf.String())
	}

	buf.Reset()
	logChanges(lg, changes{added: []string{"a.md"}, modified: []string{"b.md"}, removed: []string{"c.md"}})
	out := buf.String()
	for _, tag := range []string{"[NEW] a.md", "[CHANGED] b.md", "[DELETED] c.md"} {
		if !strings.Contains(out, tag) {
			t.Errorf("missing %q in:\n%s", tag, out)
		}
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

func TestCommandReportsOutputOnFailure(t *testing.T) {
	if err := Command("sh", "-c", "exit 0")(); err != nil {
		t.Errorf("successful command returned %v", err)
	}

	err := Command("sh", "-c", "echo 'main.go:12: undefined: foo' >&2; exit 1")()
	if err == nil {
		t.Fatal("expected an error from a failing command")
	}
	// The compiler's message is the useful part, and it has to survive into the
	// error so the browser overlay can show it.
	if !strings.Contains(err.Error(), "undefined: foo") {
		t.Errorf("error dropped the command output: %v", err)
	}
	if !strings.Contains(err.Error(), "sh -c") {
		t.Errorf("error does not name the command: %v", err)
	}
}

func TestCommandReportsAMissingBinary(t *testing.T) {
	err := Command("astwerk-no-such-binary")()
	if err == nil || !strings.Contains(err.Error(), "astwerk-no-such-binary") {
		t.Errorf("err = %v, want it to name the missing binary", err)
	}
}

func TestStepsRunInOrderAndStopAtFailure(t *testing.T) {
	var ran []string
	step := func(name string, err error) func() error {
		return func() error { ran = append(ran, name); return err }
	}

	err := Steps(
		step("one", nil),
		nil, // skipped, not a panic
		step("two", errors.New("boom")),
		step("three", nil),
	)()

	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want boom", err)
	}
	if len(ran) != 2 || ran[0] != "one" || ran[1] != "two" {
		t.Errorf("ran = %v, want [one two] — three must not run after a failure", ran)
	}
	if err := Steps()(); err != nil {
		t.Errorf("Steps() with no steps = %v, want nil", err)
	}
}

// The regression this whole subprocess business exists for: a build that
// recompiles must actually change what the server serves.
func TestRunPicksUpACodeChangeViaSubprocess(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "build")

	// A tiny generator program standing in for a templ-backed site: the page
	// text is compiled in, not read at runtime, so only a recompile can change
	// it.
	writeFile(t, filepath.Join(dir, "go.mod"), "module gen\n\ngo 1.25\n")
	gen := filepath.Join(dir, "main.go")
	page := func(text string) string {
		return "package main\n\nimport (\"os\"; \"path/filepath\")\n\n" +
			"const page = \"" + text + "\"\n\n" +
			"func main() {\n" +
			"\tos.MkdirAll(filepath.Join(os.Args[1]), 0755)\n" +
			"\tos.WriteFile(filepath.Join(os.Args[1], \"index.html\"), []byte(\"<html><body>\"+page+\"</body></html>\"), 0644)\n" +
			"}\n"
	}
	writeFile(t, gen, page("first"))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	addrs := make(chan string, 1)
	go Run(ctx, Config{
		Build:    Command("go", "run", gen, out),
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
	case <-time.After(60 * time.Second):
		t.Fatal("server never became ready")
	}
	base := "http://" + addr

	if body := fetch(t, base+"/"); !strings.Contains(body, "first") {
		t.Fatalf("initial page = %q", body)
	}

	// Change the compiled-in constant. An in-process build would keep serving
	// "first" here forever.
	writeFile(t, gen, page("second"))

	deadline := time.Now().Add(60 * time.Second)
	for {
		if body := fetch(t, base+"/"); strings.Contains(body, "second") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a recompiled code change never reached the served page")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A build that writes into a watched directory must not trigger itself.
func TestRunDoesNotLoopOnItsOwnOutput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "build")
	writeFile(t, filepath.Join(dir, "src.md"), "start")

	var mu sync.Mutex
	builds := 0
	generated := filepath.Join(dir, "generated.go") // watched, and written by the build

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	addrs := make(chan string, 1)
	go Run(ctx, Config{
		Build: func() error {
			mu.Lock()
			builds++
			n := builds
			mu.Unlock()
			// Rewrite a watched file with fresh content every time, the way a
			// code generator would.
			writeFile(t, generated, fmt.Sprintf("package main // build %d", n))
			writeFile(t, filepath.Join(out, "index.html"), "<html><body>ok</body></html>")
			return nil
		},
		Dir:      out,
		Addr:     "localhost:0",
		Watch:    []string{dir},
		Interval: 10 * time.Millisecond,
		Log:      log.New(io.Discard, "", 0),
		Ready:    func(addr string) { addrs <- addr },
	})

	select {
	case <-addrs:
	case <-time.After(10 * time.Second):
		t.Fatal("server never became ready")
	}

	writeFile(t, filepath.Join(dir, "src.md"), "edited")
	time.Sleep(600 * time.Millisecond) // ~60 poll intervals

	mu.Lock()
	got := builds
	mu.Unlock()
	// One at startup, one for the edit. A few more would mean the debounce is
	// loose; a runaway count means the build is retriggering on its own output.
	if got > 4 {
		t.Errorf("ran %d builds for one edit — the build is retriggering itself", got)
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
