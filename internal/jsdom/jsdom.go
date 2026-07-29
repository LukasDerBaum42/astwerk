//go:build js && wasm

// Package jsdom provides a minimal fake DOM for tests running under
// GOOS=js GOARCH=wasm.
//
// node has no DOM, so wrapper tests need one to talk to. This stub is
// deliberately small and dumb: it records what was done to it so a test can
// assert that a Go method reached for the right JS property with the right
// arguments. It is not an attempt to emulate a browser, and nothing here should
// grow to cover behaviour that only a real DOM can settle.
//
// Selector support is limited to what the tests need: "#id", ".class",
// "[attr]" and a bare tag name.
package jsdom

import "syscall/js"

// Install defines document, window-ish globals and a fake history on
// globalThis. Calling it more than once is harmless.
func Install() {
	js.Global().Call("eval", source)
}

// Reset gives a test a clean document.body.
func Reset() {
	js.Global().Call("__resetDOM")
}

// NewElement makes a detached stub element.
func NewElement(tag string) js.Value {
	return js.Global().Call("__newElement", tag)
}

// Extras that the stub adds beyond the real DOM, for assertions:
//
//	el.listenerCount(type)  number of handlers bound for an event
//	el._tree                comma-joined text of the subtree, for list ordering
//	el._rect                the object getBoundingClientRect returns
//	el.dispatch(type, prop) fire an event with extra properties
//	el._ctxCalls            recorded 2D canvas calls, as "fillRect(1,2,3,4)"
const source = `
(function () {
  if (globalThis.__jsdomInstalled) return;
  globalThis.__jsdomInstalled = true;

  class Cls {
    constructor(el) { this.el = el; }
    add(...ns) { ns.forEach(n => this.el._classes.add(n)); }
    remove(...ns) { ns.forEach(n => this.el._classes.delete(n)); }
    toggle(n) {
      if (this.el._classes.has(n)) { this.el._classes.delete(n); return false; }
      this.el._classes.add(n); return true;
    }
    contains(n) { return this.el._classes.has(n); }
  }

  class Style {
    constructor() { this._p = {}; }
    setProperty(k, v) { this._p[k] = String(v); }
    getPropertyValue(k) { return k in this._p ? this._p[k] : ""; }
  }

  function makeCtx(el) {
    const calls = el._ctxCalls;
    const rec = name => (...args) => { calls.push(name + "(" + args.join(",") + ")"); };
    const ctx = { fillStyle: "", strokeStyle: "", lineWidth: 0, font: "" };
    ["fillRect", "strokeRect", "clearRect", "beginPath", "closePath", "moveTo",
     "lineTo", "arc", "fill", "stroke", "fillText", "save", "restore", "scale"
    ].forEach(n => { ctx[n] = rec(n); });
    ctx.measureText = t => ({ width: t.length * 7 });
    return ctx;
  }

  class El {
    constructor(tag) {
      this.tagName = String(tag).toUpperCase();
      this._classes = new Set(); this._attrs = {}; this._children = [];
      this._listeners = {}; this._ctxCalls = [];
      this.style = new Style();
      this.textContent = ""; this.innerHTML = ""; this.parentElement = null;
      this.value = ""; this.checked = false;
      this.width = 0; this.height = 0;
      this._rect = { left: 0, top: 0, width: 0, height: 0 };
    }
    get classList() { return new Cls(this); }
    get children() { return this._children; }
    appendChild(c) {
      if (c.parentElement) c.remove();
      c.parentElement = this; this._children.push(c); return c;
    }
    insertBefore(c, ref) {
      if (c.parentElement) c.remove();
      const i = this._children.indexOf(ref);
      c.parentElement = this;
      if (i < 0) this._children.push(c); else this._children.splice(i, 0, c);
      return c;
    }
    remove() {
      const p = this.parentElement; if (!p) return;
      const i = p._children.indexOf(this); if (i >= 0) p._children.splice(i, 1);
      this.parentElement = null;
    }
    cloneNode(deep) {
      const c = new El(this.tagName);
      c.textContent = this.textContent; c.innerHTML = this.innerHTML;
      c._attrs = Object.assign({}, this._attrs);
      c._classes = new Set(this._classes);
      if (deep) this._children.forEach(k => c.appendChild(k.cloneNode(true)));
      return c;
    }
    getAttribute(n) { return n in this._attrs ? this._attrs[n] : null; }
    setAttribute(n, v) { this._attrs[n] = String(v); }
    removeAttribute(n) { delete this._attrs[n]; }
    addEventListener(t, f) { (this._listeners[t] = this._listeners[t] || []).push(f); }
    removeEventListener(t, f) {
      const a = this._listeners[t]; if (!a) return;
      const i = a.indexOf(f); if (i >= 0) a.splice(i, 1);
    }
    listenerCount(t) { return (this._listeners[t] || []).length; }
    dispatch(t, props) {
      const self = this;
      const ev = Object.assign({
        type: t, target: self, defaultPrevented: false, propagationStopped: false,
        preventDefault() { ev.defaultPrevented = true; },
        stopPropagation() { ev.propagationStopped = true; },
      }, props || {});
      (this._listeners[t] || []).slice().forEach(f => f(ev));
      return ev;
    }
    querySelector(sel) { return match(this._children, sel); }
    querySelectorAll(sel) { return matchAll(this._children, sel); }
    getBoundingClientRect() { return this._rect; }
    getContext(kind) { if (!this._ctx) this._ctx = makeCtx(this); return this._ctx; }
    closest(sel) {
      let n = this;
      while (n) { if (hits(n, sel)) return n; n = n.parentElement; }
      return null;
    }
    get _tree() { return this._children.map(c => c.textContent || c._tree).join(","); }
  }

  function hits(el, sel) {
    if (sel[0] === "#") return el._attrs.id === sel.slice(1);
    if (sel[0] === ".") return el._classes.has(sel.slice(1));
    if (sel[0] === "[") return sel.slice(1, -1) in el._attrs;
    return el.tagName === sel.toUpperCase();
  }
  function walk(nodes, fn) { for (const n of nodes) { fn(n); walk(n._children, fn); } }
  function match(nodes, sel) {
    let f = null; walk(nodes, n => { if (!f && hits(n, sel)) f = n; }); return f;
  }
  function matchAll(nodes, sel) {
    const o = []; walk(nodes, n => { if (hits(n, sel)) o.push(n); }); return o;
  }

  const doc = new El("document");
  doc.body = new El("body");
  doc._children.push(doc.body);
  doc.createElement = t => new El(t);
  doc.querySelector = s => match(doc._children, s);
  doc.querySelectorAll = s => matchAll(doc._children, s);

  globalThis.document = doc;
  globalThis.getComputedStyle = el => el.style;
  globalThis.devicePixelRatio = 2;

  globalThis.__newElement = t => new El(t);
  globalThis.__resetDOM = () => {
    doc.body = new El("body");
    doc._children.length = 0; doc._children.push(doc.body);
  };

  // History and location, so the router has something to drive.
  globalThis.__path = "/";
  globalThis.location = {
    get pathname() { return globalThis.__path; },
    origin: "https://site.test",
  };
  globalThis.history = {
    pushState(_s, _t, p) { globalThis.__path = p; },
    replaceState(_s, _t, p) { globalThis.__path = p; },
  };
  if (!globalThis.addEventListener) globalThis.addEventListener = () => {};
  if (!globalThis.removeEventListener) globalThis.removeEventListener = () => {};

  // Animation frames, driven off timers so tests can await them.
  globalThis.requestAnimationFrame = fn =>
    setTimeout(() => fn(globalThis.__now = (globalThis.__now || 0) + 16), 0);
  globalThis.cancelAnimationFrame = id => clearTimeout(id);
})();
`
