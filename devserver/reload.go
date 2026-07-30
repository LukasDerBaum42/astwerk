package devserver

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ReloadPath is the endpoint the injected script connects to. It is namespaced
// so it can't collide with a page a site actually has.
const ReloadPath = "/__astwerk/reload"

// event is one message pushed to the browsers: an SSE event name and its data.
type event struct{ name, data string }

// hub is the set of connected browsers. There is usually one; there are
// sometimes three, on two machines, and each has to get every event.
type hub struct {
	log *logger

	mu     sync.Mutex
	subs   map[chan event]struct{}
	closed bool

	// last is the most recent build error, replayed to a browser that connects
	// after the failure — otherwise a reload while the build is broken shows a
	// stale page with no overlay and no explanation.
	last *event
}

func newHub(lg *logger) *hub {
	if lg == nil {
		lg = newLogger(nil, true)
	}
	return &hub{log: lg, subs: map[chan event]struct{}{}}
}

// clients is how many browsers are currently listening.
func (h *hub) clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// subscribe returns a channel of events and a function to release it.
func (h *hub) subscribe() (<-chan event, func()) {
	// Buffered so a slow browser can't block the build loop; if the buffer
	// fills, that connection misses events and the next one catches it up.
	ch := make(chan event, 8)

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	h.subs[ch] = struct{}{}
	replay := h.last
	h.mu.Unlock()

	if replay != nil {
		ch <- *replay
	}

	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}
}

func (h *hub) reload() {
	h.mu.Lock()
	h.last = nil
	h.mu.Unlock()

	n := h.publish(event{name: "reload", data: "ok"})
	if n == 0 {
		// Worth saying: the usual cause is no browser tab open yet, and it
		// otherwise looks like live reload is broken.
		h.log.event(colorYellow, tagReload, "no browser connected")
		return
	}
	h.log.event(colorCyan, tagReload, "sent to %s", plural(n, "client"))
}

func (h *hub) buildError(msg string) {
	e := event{name: "builderror", data: msg}
	h.mu.Lock()
	h.last = &e
	h.mu.Unlock()

	if n := h.publish(e); n > 0 {
		h.log.event(colorCyan, tagReload, "error overlay sent to %s", plural(n, "client"))
	}
}

// publish fans an event out and reports how many browsers it reached.
func (h *hub) publish(e event) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	sent := 0
	for ch := range h.subs {
		select {
		case ch <- e:
			sent++
		default: // dropped: this browser is not keeping up
		}
	}
	return sent
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func (h *hub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for ch := range h.subs {
		delete(h.subs, ch)
		close(ch)
	}
}

// serveEvents streams the hub to one browser over SSE.
func (h *hub) serveEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Subscribing before the headers go out is what makes the stream reliable:
	// once a client has read anything at all, it is registered, so an event
	// published immediately afterwards cannot slip past it.
	events, release := h.subscribe()
	h.log.event(colorCyan, tagClient, "connected (%s)", plural(h.clients(), "total"))
	defer func() {
		release()
		h.log.event(colorCyan, tagClient, "disconnected (%s)", plural(h.clients(), "total"))
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 500\n\n")
	flusher.Flush()

	// A proxy or a suspended laptop drops an idle connection without either end
	// noticing. A periodic comment keeps it alive, and when it does fail, makes
	// the browser reconnect promptly instead of sitting on a dead stream.
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case e, ok := <-events:
			if !ok {
				return
			}
			// Data may be a multi-line compiler error, and SSE frames one line
			// at a time.
			fmt.Fprintf(w, "event: %s\n", e.name)
			for line := range strings.SplitSeq(e.data, "\n") {
				fmt.Fprintf(w, "data: %s\n", line)
			}
			fmt.Fprint(w, "\n")
			flusher.Flush()
		}
	}
}
