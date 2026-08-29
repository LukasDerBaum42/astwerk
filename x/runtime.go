package x

import "strings"

// minifyJS compacts generated JavaScript by stripping leading indentation.
// Line breaks are preserved: the runtime fragments contain // comments, so
// joining the lines would let a comment swallow the code that followed it on
// the joined line (the production bug where the signal's set method was
// commented out). User-authored JavaScript (effect bodies) is never passed
// through it: automatic-semicolon-insertion makes that unsafe.
func minifyJS(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimLeft(line, " \t")
	}
	return strings.Join(lines, "\n")
}

// The browser runtime is assembled per page from small fragments, so a page
// carries only the runtime pieces it actually uses. Everything here is emitted
// verbatim inside a <script> element, so no fragment may contain "</script>".
//
// Subscribers are resolved at build time wherever they can be: every binding
// and computed value is generated with its dependencies known, so the runtime
// just registers them. Only an effect's body is opaque JavaScript, whose
// dependencies cannot be known until it runs — that is the one place the
// runtime tracks reads.

// runtimeCore is the shell every reactive page needs: node lookup and signals.
const runtimeCore = `(() => {
	const el = (id) => document.querySelector('[data-x="' + id + '"]');
	let track = () => {};
	const signal = (init) => {
		let v = init;
		const subs = new Set();
		const s = {
			get: () => { track(s); return v; },
			// Iterate a snapshot of subscribers: an effect re-subscribes when it
			// re-runs, and iterating the live set would call it again — forever.
			// Subscribers receive the new value.
			set: (nv) => { if (nv !== v) { v = nv; [...subs].forEach((f) => f(v)); } },
		};
		s.onChange = (fn) => { subs.add(fn); return () => subs.delete(fn); };
		s.sub = (fn) => { const off = s.onChange(fn); fn(v); return off; };
		return s;
	};`

// runtimeTrack turns runtime dependency tracking on; effect needs it, since an
// effect body is opaque JavaScript and its dependencies cannot be known until
// it runs. Everything else — bindings and computed values — has its
// subscribers resolved at build time and never uses this.
const runtimeTrack = `	let observer = null;
	track = (s) => { if (observer) observer.add(s); };
`

const runtimeEffect = `	const effect = (fn) => {
		const offs = new Set();
		const run = () => {
			offs.forEach((off) => off());
			offs.clear();
			const deps = new Set();
			const prev = observer;
			observer = deps;
			try { fn(); } finally { observer = prev; }
			deps.forEach((s) => offs.add(s.onChange(run)));
		};
		run();
		return () => { offs.forEach((off) => off()); offs.clear(); };
	};
`

const runtimeComputed = `	const computed = (deps, fn) => {
		const s = signal(fn());
		deps.forEach((d) => d.onChange(() => s.set(fn())));
		return s;
	};
`

const runtimeText = `	const text = (id, s) => s.sub((v) => { const n = el(id); if (n) n.textContent = v; });
`

const runtimeOn = `	const on = (id, ev, fn) => { const n = el(id); if (n) n.addEventListener(ev, fn); };
`

const runtimeAttr = `	const attr = (id, name, s) => s.sub((v) => { const n = el(id); if (n) { if (v === "") n.removeAttribute(name); else n.setAttribute(name, v); } });
`

const runtimeBoolAttr = `	const boolAttr = (id, name, s) => s.sub((v) => { const n = el(id); if (n) n.toggleAttribute(name, v); });
`

const runtimeCls = `	const cls = (id, name, s) => s.sub((v) => { const n = el(id); if (n) n.classList.toggle(name, v); });
`

const runtimeShow = `	const show = (id, s) => s.sub((v) => { const n = el(id); if (n) n.style.display = v ? "" : "none"; });
`

const runtimeStyle = `	const style = (id, prop, s) => s.sub((v) => { const n = el(id); if (n) n.style.setProperty(prop, v); });
`

const runtimeValue = `	const value = (id, s) => { const n = el(id); if (!n) return; s.sub((v) => { n.value = v; }); n.addEventListener("input", () => s.set(n.value)); };
`

const runtimeNumber = `	const number = (id, s) => { const n = el(id); if (!n) return; s.sub((v) => { n.value = v; }); n.addEventListener("input", () => { const f = parseFloat(n.value); if (!Number.isNaN(f)) s.set(f); }); };
`

const runtimeChecked = `	const checked = (id, s) => { const n = el(id); if (!n) return; s.sub((v) => { n.checked = v; }); n.addEventListener("change", () => s.set(n.checked)); };
`

const runtimeWhen = `	const when = (id, html, s) => {
		const n = el(id);
		if (!n) return;
		let on = n.innerHTML !== "";
		s.sub((v) => {
			if (v && !on) { n.innerHTML = html; on = true; }
			else if (!v && on) { n.innerHTML = ""; on = false; }
		});
	};
`

const runtimeList = `	const list = (id, s, key, template) => {
			const n = el(id);
			if (!n) return;
			const live = new Map();
			for (const c of n.children) {
				if (c.dataset.key) live.set(c.dataset.key, c);
			}
			s.sub((items) => {
				const seen = new Set();
				const ordered = [];
				for (const item of items) {
					const k = String(item[key]);
					if (seen.has(k)) continue;
					seen.add(k);
					let c = live.get(k);
					if (!c && template) {
						const wrap = document.createElement("div");
						wrap.innerHTML = template(item);
						c = wrap.firstElementChild;
						if (c) { c.dataset.key = k; live.set(k, c); }
					}
					if (c) ordered.push(c);
				}
				for (const [k, c] of live) {
					if (!seen.has(k)) { c.remove(); live.delete(k); }
				}
				for (const c of ordered) n.appendChild(c);
			});
		};
`

// runtimeRegister is the bridge's x-side half: named signals are emitted as
// lexical consts, not globalThis properties, so a wasm module cannot reach them
// by name. The registry records the cells that carry an explicit name — the
// only ones whose identity is stable API — and lets the wasm side look one up.
// It ships only when a page declares a named signal.
const runtimeRegister = `	const states = {};
	const register = (name, s) => { states[name] = s; };
	const state = (name) => states[name];
`

// runtimeFeatures are the optional runtime pieces, in dependency order: the
// key a program feature uses to pull them in, the name they are exported
// under ("" for internals like track), and their source.
var runtimeFeatures = []struct {
	key    string
	export string
	code   string
}{
	{key: "track", code: runtimeTrack},
	{key: "effect", export: "effect", code: runtimeEffect},
	{key: "computed", export: "computed", code: runtimeComputed},
	{key: "text", export: "text", code: runtimeText},
	{key: "on", export: "on", code: runtimeOn},
	{key: "attr", export: "attr", code: runtimeAttr},
	{key: "boolAttr", export: "boolAttr", code: runtimeBoolAttr},
	{key: "cls", export: "cls", code: runtimeCls},
	{key: "show", export: "show", code: runtimeShow},
	{key: "style", export: "style", code: runtimeStyle},
	{key: "value", export: "value", code: runtimeValue},
	{key: "number", export: "number", code: runtimeNumber},
	{key: "checked", export: "checked", code: runtimeChecked},
	{key: "when", export: "when", code: runtimeWhen},
	{key: "list", export: "list", code: runtimeList},
	{key: "register", export: "state", code: runtimeRegister},
}
