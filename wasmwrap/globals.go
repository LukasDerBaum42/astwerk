//go:build js && wasm

package wasmwrap

import "syscall/js"

// Storage wraps a key-value store with a getItem/setItem interface — the
// browser's localStorage or sessionStorage.
type Storage struct{ v js.Value }

// LocalStorage returns window.localStorage.
func LocalStorage() Storage { return Storage{v: js.Global().Get("localStorage")} }

// Get returns the value for key, or "" if absent.
func (s Storage) Get(key string) string {
	v := s.v.Call("getItem", key)
	if v.IsNull() || v.IsUndefined() {
		return ""
	}
	return v.String()
}

// Set stores value under key and returns the store.
func (s Storage) Set(key, value string) Storage {
	s.v.Call("setItem", key, value)
	return s
}

// Remove deletes key and returns the store.
func (s Storage) Remove(key string) Storage {
	s.v.Call("removeItem", key)
	return s
}

// Exists reports whether key is present.
func (s Storage) Exists(key string) bool {
	v := s.v.Call("getItem", key)
	return !v.IsNull() && !v.IsUndefined()
}

// LocationInfo wraps window.location.
type LocationInfo struct{ v js.Value }

// Location returns window.location.
func Location() LocationInfo { return LocationInfo{v: js.Global().Get("location")} }

// Path returns the pathname.
func (l LocationInfo) Path() string { return l.v.Get("pathname").String() }

// Query returns the query string, leading "?" included.
func (l LocationInfo) Query() string { return l.v.Get("search").String() }

// Hash returns the fragment, leading "#" included.
func (l LocationInfo) Hash() string { return l.v.Get("hash").String() }

// SetHash sets the fragment, leading "#" included.
func (l LocationInfo) SetHash(h string) { l.v.Set("hash", h) }

// WindowInfo wraps window.
type WindowInfo struct{ v js.Value }

// Window returns window.
func Window() WindowInfo { return WindowInfo{v: js.Global().Get("window")} }

// InnerWidth returns the viewport width in CSS pixels.
func (w WindowInfo) InnerWidth() int { return w.v.Get("innerWidth").Int() }

// InnerHeight returns the viewport height in CSS pixels.
func (w WindowInfo) InnerHeight() int { return w.v.Get("innerHeight").Int() }
