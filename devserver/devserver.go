// Package devserver is the loop you leave running while you work: it serves the
// build directory, watches your sources, rebuilds when they change, and reloads
// the browser.
//
// It knows nothing about [ssg] — you hand it a Build function, and it calls it.
// That keeps it a general watch-rebuild-reload loop rather than a second entry
// point into the walker, and it means the same server works for a project whose
// build does more than call [ssg.Build]:
//
//	func main() {
//		if *serve {
//			devserver.Run(context.Background(), devserver.Config{
//				Build: func() error { return build(true) },
//			})
//			return
//		}
//		build(false)
//	}
//
// Pass a build that sets [ssg.BuildOptions.Dev], so the output directory is
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

	// Log receives build and reload messages. Defaults to [log.Default].
	Log *log.Logger

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
		c.Log = log.Default()
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

	hub := newHub()
	defer hub.close()

	// The first build is reported but not fatal: a project with a broken build
	// still wants the server up, showing the error, so fixing it reloads.
	if err := cfg.Build(); err != nil {
		cfg.Log.Printf("build failed: %v", err)
		hub.buildError(err.Error())
	} else {
		cfg.Log.Printf("built %s/", cfg.Dir)
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("devserver: listen on %s: %w", cfg.Addr, err)
	}
	defer ln.Close()

	srv := &http.Server{Handler: handler(cfg.Dir, hub)}
	errc := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	cfg.Log.Printf("serving %s/ on http://%s", cfg.Dir, ln.Addr())
	if cfg.Ready != nil {
		cfg.Ready(ln.Addr().String())
	}

	go watchLoop(ctx, cfg, hub)

	select {
	case <-ctx.Done():
	case err = <-errc:
	}

	shutdown, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer stop()
	srv.Shutdown(shutdown)
	return err
}

// watchLoop rebuilds whenever the watcher reports a change, and tells the
// browsers what happened either way.
func watchLoop(ctx context.Context, cfg Config, hub *hub) {
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

		changed := w.scan()
		if len(changed) == 0 {
			continue
		}
		first := changed[0]

		// Wait for a quiet tick before building: an editor saving several files,
		// or writing one file in two steps, should be one rebuild and not two.
		for len(changed) > 0 {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			changed = w.scan()
		}

		cfg.Log.Printf("changed: %s, rebuilding", first)
		start := time.Now()
		if err := cfg.Build(); err != nil {
			cfg.Log.Printf("build failed: %v", err)
			hub.buildError(err.Error())
			continue
		}
		cfg.Log.Printf("rebuilt in %s, reloading", time.Since(start).Round(time.Millisecond))
		hub.reload()
	}
}
