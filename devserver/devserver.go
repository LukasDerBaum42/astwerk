// Package devserver is the loop you leave running while you work: it serves the
// build directory, watches your sources, rebuilds when they change, and reloads
// the browser.
//
// It knows nothing about [ssg] — you hand it a Build function, and it calls it.
// That keeps it a general watch-rebuild-reload loop rather than a second entry
// point into the walker, and it means the same server works for a project whose
// build does more than call [ssg.Build].
//
// Build your site in a subprocess. A running Go program cannot load new code,
// so a build that calls your page functions in-process will silently keep
// rendering the components that were compiled into it — see [Config.Build].
// [Command] and [Steps] exist for this:
//
//	func main() {
//		flag.Parse()
//		if !*serve {
//			build(*dev)   // the one-shot build; also what --serve shells out to
//			return
//		}
//		devserver.Run(context.Background(), devserver.Config{
//			Build: devserver.Steps(
//				devserver.Command("templ", "generate"),
//				devserver.Command("go", "run", ".", "--out", "build", "--dev"),
//			),
//		})
//	}
//
// Have that inner build set [ssg.BuildOptions.Dev], so the output directory is
// overwritten in place rather than removed and recreated — a browser fetching
// an asset mid-rebuild then never sees a missing file.
//
// [ssg]: https://pkg.go.dev/github.com/LukasDerBaum42/astwerk/ssg
// [ssg.Build]: https://pkg.go.dev/github.com/LukasDerBaum42/astwerk/ssg#Build
// [ssg.BuildOptions.Dev]: https://pkg.go.dev/github.com/LukasDerBaum42/astwerk/ssg#BuildOptions
package devserver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// Defaults applied to a zero-valued [Config] field.
const (
	DefaultDir      = "build"
	DefaultAddr     = "localhost:8080"
	DefaultInterval = 300 * time.Millisecond
)

// DefaultExts are the file extensions a rebuild is triggered by. Anything that
// can change what the build produces is in the list; compiled output is not,
// since the build writes that itself.
var DefaultExts = []string{
	".go", ".templ", ".md", ".html", ".css", ".js", ".ts",
	".toml", ".json", ".yaml", ".yml", ".svg",
}

// DefaultIgnore are directory names never descended into. The output directory
// is added to this list automatically — without that, every build would trigger
// the next one.
var DefaultIgnore = []string{".git", "node_modules", ".cache", "vendor"}

// Config describes one dev server. Every field except Build has a default.
type Config struct {
	// Build regenerates the site. It is required, and it is called once before
	// the server starts listening, so a broken build is reported immediately
	// rather than after the first edit.
	//
	// Use [Command] or [Steps] unless you are certain you don't need to. A Go
	// process cannot load new code, so a Build closure that calls your render
	// functions directly will keep rendering the versions that were compiled
	// into the running binary. Edit a .templ file and the watcher fires, the
	// build "succeeds", the browser reloads — and the page is unchanged, which
	// is worse than nothing happening at all, because it looks like it worked.
	// Rebuilding out of process is what makes a code change take effect:
	//
	//	Build: devserver.Steps(
	//		devserver.Command("templ", "generate"),
	//		devserver.Command("go", "run", ".", "--out", "build", "--dev"),
	//	)
	//
	// An in-process closure is correct only when everything the build reads is
	// data — markdown, CSS, images — and never compiled Go. It is faster, and
	// it is a trap the moment a .templ file enters the picture.
	Build func() error

	// Dir is the directory to serve. Defaults to [DefaultDir].
	Dir string

	// Addr is the address to listen on. Defaults to [DefaultAddr].
	//
	// Use "localhost:0" to let the OS pick a port; the chosen address is logged
	// and reported through [Config.Ready].
	Addr string

	// Watch are the directories watched for changes. Defaults to the working
	// directory.
	Watch []string

	// Exts are the file extensions that trigger a rebuild, leading dot included.
	// Defaults to [DefaultExts].
	Exts []string

	// Ignore are directory names skipped anywhere in the watched tree.
	// Defaults to [DefaultIgnore]; Dir is always skipped.
	Ignore []string

	// Interval is how often the watcher polls. Defaults to [DefaultInterval].
	//
	// Polling rather than OS file events is the deliberate trade: it costs a
	// sub-second delay before a rebuild starts, and it saves the project a
	// dependency and the platform-specific failure modes that come with one.
	Interval time.Duration

	// Log receives build, watch and reload messages, one tagged line each:
	//
	//	[23:28:12] [CHANGED] templates/layout.templ
	//	[23:28:12] [BUILD]   rebuilding
	//	[23:28:15] [BUILD]   ok (3.701s)
	//	[23:28:15] [RELOAD]  sent to 1 client
	//
	// Defaults to a logger writing to standard error with no flags of its own,
	// since the timestamp is part of the line. A logger you supply keeps its
	// own prefix and flags, so it can slot into a project's existing logging —
	// at the cost of a second timestamp if it has one.
	Log *log.Logger

	// NoColor turns off ANSI colour. Colour is off automatically when the log
	// is not a terminal, and when NO_COLOR is set in the environment.
	NoColor bool

	// Ready, if set, is called with the address the server actually bound once
	// it is listening. Mainly for tests.
	Ready func(addr string)
}

func (c *Config) applyDefaults() error {
	if c.Build == nil {
		return errors.New("devserver: Config.Build is required")
	}
	if c.Dir == "" {
		c.Dir = DefaultDir
	}
	if c.Addr == "" {
		c.Addr = DefaultAddr
	}
	if len(c.Watch) == 0 {
		c.Watch = []string{"."}
	}
	if len(c.Exts) == 0 {
		c.Exts = DefaultExts
	}
	if c.Ignore == nil {
		c.Ignore = DefaultIgnore
	}
	if c.Interval <= 0 {
		c.Interval = DefaultInterval
	}
	if c.Log == nil {
		c.Log = log.New(os.Stderr, "", 0)
	}
	return nil
}

// Run builds the site, serves it, and rebuilds on every change until ctx is
// cancelled.
//
// A build that fails does not stop the server: the error is logged and pushed
// to every connected browser as an overlay, and the next change tries again.
// Stopping would mean losing the reload connections and having to restart by
// hand for what is usually a typo.
func Run(ctx context.Context, cfg Config) error {
	if err := cfg.applyDefaults(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	lg := newLogger(cfg.Log, cfg.NoColor)
	hub := newHub(lg)
	defer hub.close()

	// The first build is reported but not fatal: a project with a broken build
	// still wants the server up, showing the error, so fixing it reloads.
	lg.event(colorCyan, tagStart, "initial build")
	runBuild(cfg, lg, hub)

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		lg.event(colorRed, tagError, "cannot listen on %s: %v", cfg.Addr, err)
		return fmt.Errorf("devserver: listen on %s: %w", cfg.Addr, err)
	}
	defer ln.Close()

	if _, err := os.Stat(cfg.Dir); err != nil {
		// Not fatal — the next successful build may create it — but silence
		// here means every request 404s for a reason that isn't obvious.
		lg.event(colorYellow, tagError,
			"%s/ does not exist yet; check that the build writes there", cfg.Dir)
	}

	srv := &http.Server{Handler: handler(cfg.Dir, hub)}
	errc := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	lg.event(colorGreen, tagServer, "http://%s  (serving %s/)", ln.Addr(), cfg.Dir)
	lg.event(colorCyan, tagWatch, "%s  (every %s)",
		strings.Join(cfg.Watch, ", "), cfg.Interval)
	lg.event(colorCyan, tagWatch, "extensions: %s", strings.Join(cfg.Exts, " "))
	lg.event(colorCyan, tagWatch, "ignoring: %s, %s/", strings.Join(cfg.Ignore, ", "), cfg.Dir)
	lg.event(colorCyan, tagInfo, "press Ctrl+C to stop")

	if cfg.Ready != nil {
		cfg.Ready(ln.Addr().String())
	}

	go watchLoop(ctx, cfg, lg, hub)

	select {
	case <-ctx.Done():
	case err = <-errc:
		lg.event(colorRed, tagError, "server stopped: %v", err)
	}

	lg.event(colorYellow, tagStop, "shutting down")
	shutdown, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer stop()
	srv.Shutdown(shutdown)
	return err
}

// runBuild runs one build, timing it and reporting the outcome to both the log
// and the browser.
func runBuild(cfg Config, lg *logger, hub *hub) (ok bool) {
	lg.event(colorYellow, tagBuild, "building")

	start := time.Now()
	err := cfg.Build()
	elapsed := time.Since(start).Round(time.Millisecond)

	if err != nil {
		// A build run through Command has already streamed its own output to
		// the terminal, so the line here is the verdict rather than a repeat of
		// it. The full text goes to the browser overlay.
		lg.event(colorRed, tagBuild, "failed (%s)", elapsed)
		hub.buildError(err.Error())
		return false
	}
	lg.event(colorGreen, tagBuild, "ok (%s)", elapsed)
	return true
}

// logChanges reports what the watcher saw, one line per file so a rebuild can
// always be traced back to the edit that caused it. A large batch — switching
// branches, say — is summarised instead of scrolling the terminal away.
func logChanges(lg *logger, c changes) {
	const maxListed = 10

	if c.count() > maxListed {
		lg.event(colorYellow, tagChanged, "%d files (%d new, %d modified, %d deleted)",
			c.count(), len(c.added), len(c.modified), len(c.removed))
		return
	}
	for _, p := range c.added {
		lg.event(colorGreen, tagNew, "%s", p)
	}
	for _, p := range c.modified {
		lg.event(colorYellow, tagChanged, "%s", p)
	}
	for _, p := range c.removed {
		lg.event(colorRed, tagDeleted, "%s", p)
	}
}

// watchLoop rebuilds whenever the watcher reports a change, and tells the
// browsers what happened either way.
func watchLoop(ctx context.Context, cfg Config, lg *logger, hub *hub) {
	w := newWatcher(cfg)
	w.scan() // the state at startup is not a change

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		found := w.scan()
		if found.empty() {
			continue
		}

		// Wait for a quiet tick before building: an editor saving several files,
		// or writing one file in two steps, should be one rebuild and not two.
		// Anything that lands while waiting is part of the same edit, so it is
		// reported alongside the first batch rather than triggering a second.
		for more := found; !more.empty(); {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			more = w.scan()
			found.added = append(found.added, more.added...)
			found.modified = append(found.modified, more.modified...)
			found.removed = append(found.removed, more.removed...)
		}

		logChanges(lg, found)
		ok := runBuild(cfg, lg, hub)

		// Adopt whatever the build itself touched before looking again. A build
		// that writes inside a watched directory — templ generate producing
		// *_templ.go is the usual one — would otherwise see its own output as
		// the next change and rebuild forever.
		w.scan()

		if ok {
			hub.reload()
		}
	}
}
