// Usage analytics: what people do on the site goes to the server's
// usage_events table (AGENTS.md, "Usage analytics"), so we can see how the shop
// is used and improve it. Page views and time on page, clicks, form fields
// (never what's typed), cart changes, hovers over items, and errors.
// Text inside [data-private] (addresses) is never sent.
import type { Cart, Sizes } from './cart';
import { FrameStats, LongFrames, ViewStats } from './perf';

type Props = Record<string, unknown>;
type Batch = { visit: string; release: string; sent: number; events: { name: string; path: string; t: number; props: Props }[] };

const CHUNK = 40;
const VISIT_IDLE_MS = 30 * 60_000;
const INPUT_IDLE_MS = 60_000;
const HOVER_MIN_MS = 500;
const SEEN_MIN_MS = 1000;

/** Queues events and sends them in batches. */
export class Tracker {
	private queue: Batch['events'] = [];
	constructor(
		private io: { send: (b: Batch) => void; now: () => number; visit: () => string; path: () => string; release: string }
	) {}

	track(name: string, props: Props = {}, path = this.io.path()) {
		this.queue.push({ name, path, t: this.io.now(), props });
	}

	get pending() {
		return this.queue.length;
	}

	flush() {
		while (this.queue.length) {
			const events = this.queue.splice(0, CHUNK);
			this.io.send({ visit: this.io.visit(), release: this.io.release, sent: this.io.now(), events });
		}
	}
}

export type Visit = { id: string; last: number };

/** The tab's visit, renewed after 30 idle minutes; fresh means it just started. */
export function nextVisit(stored: Visit | null, now: number, newId: () => string): Visit & { fresh: boolean } {
	if (stored && now - stored.last < VISIT_IDLE_MS) return { id: stored.id, last: now, fresh: false };
	return { id: newId(), last: now, fresh: true };
}

export function cartChanges(prev: Cart, next: Cart) {
	const ids = [...new Set([...Object.keys(prev), ...Object.keys(next)].map(Number))].sort((a, b) => a - b);
	return ids.filter((id) => (prev[id] ?? 0) !== (next[id] ?? 0)).map((id) => ({ item_id: id, from: prev[id] ?? 0, to: next[id] ?? 0 }));
}

export function sizeChanges(prev: Sizes, next: Sizes) {
	const out: { item_id: number; sku: string; from: number; to: number }[] = [];
	for (const id of [...new Set([...Object.keys(prev), ...Object.keys(next)].map(Number))]) {
		const a = prev[id] ?? {};
		const b = next[id] ?? {};
		for (const sku of new Set([...Object.keys(a), ...Object.keys(b)])) {
			if ((a[sku] ?? 0) !== (b[sku] ?? 0)) out.push({ item_id: id, sku, from: a[sku] ?? 0, to: b[sku] ?? 0 });
		}
	}
	return out;
}

const squash = (s: string | null | undefined, n = 80) => (s ?? '').replace(/\s+/g, ' ').trim().slice(0, n);

const CLICKABLE = 'a, button, [role="button"], [role="option"], [role="tab"], [role="link"], summary, label, input, select, textarea, canvas, video';

/** What a click hit: the clickable thing it's in (its text, link, item) or, when there's none, a dead click. */
export function describeClick(target: Element): Props {
	const el = target.closest(CLICKABLE);
	const tag = (el ?? target).tagName.toLowerCase();
	const out: Props = { el: tag };
	if (!el) {
		out.dead = true;
	} else {
		if (el !== target) out.target = target.tagName.toLowerCase();
		const role = el.getAttribute('role');
		if (role) out.role = role;
		const priv = !!el.closest('[data-private]');
		if (!priv && (['a', 'button', 'summary', 'label'].includes(tag) || role === 'button' || role === 'tab')) {
			const text = squash(el.textContent);
			if (text) out.text = text;
		}
		const aria = el.getAttribute('aria-label');
		if (aria && !priv) out.label = squash(aria);
		const href = el.getAttribute('href');
		if (href && !priv) out.href = href.slice(0, 300);
		if (el instanceof HTMLInputElement || el instanceof HTMLSelectElement || el instanceof HTMLTextAreaElement) {
			Object.assign(out, fieldName(el));
		}
	}
	Object.assign(out, context(target));
	return out;
}

function context(el: Element): Props {
	const out: Props = {};
	const item = el.closest('[data-item]')?.getAttribute('data-item');
	if (item && /^\d+$/.test(item)) out.item_id = Number(item);
	const track = el.closest('[data-track]')?.getAttribute('data-track');
	if (track) out.area = track.slice(0, 40);
	return out;
}

type Field = HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement;

function fieldName(el: Field): Props {
	const out: Props = { type: el.type };
	let name = el.name || el.id || el.getAttribute('autocomplete') || '';
	if (!name || name === 'on') {
		const label = el.closest('label');
		name = label ? squash(label.textContent, 60) : '';
	}
	if (name) out.field = name.slice(0, 60);
	return out;
}

/** A form field someone changed: which one, and what they picked, but only how much they typed. */
export function describeField(el: Element): Props | null {
	if (!(el instanceof HTMLInputElement || el instanceof HTMLSelectElement || el instanceof HTMLTextAreaElement)) return null;
	const { type, field } = fieldName(el);
	const out: Props = { field, type };
	if (el instanceof HTMLInputElement && el.type === 'checkbox') out.checked = el.checked;
	else if (el instanceof HTMLInputElement && el.type === 'radio') out.value = el.value.slice(0, 40);
	else if (el instanceof HTMLSelectElement) out.value = el.value.slice(0, 40);
	else if (el.type === 'number' || el.type === 'range') out.value = Number(el.value);
	else out.chars = el.value.length;
	if (el.closest('[data-private]')) delete out.value;
	if (!field) delete out.field;
	return Object.assign(out, context(el));
}

/** Time on a page: how long it was visible, and how much of that someone was using it (input within the last minute). */
export class PageClock {
	activeMs = 0;
	visibleMs = 0;
	private lastTick: number;
	private lastInput: number;
	constructor(now: number) {
		this.lastTick = this.lastInput = now;
	}
	input(now: number) {
		this.lastInput = now;
	}
	tick(now: number, visible: boolean) {
		if (visible) {
			this.visibleMs += now - this.lastTick;
			this.activeMs += Math.max(0, Math.min(now, this.lastInput + INPUT_IDLE_MS) - this.lastTick);
		}
		this.lastTick = now;
	}
}

/** Which items were on screen (at least half of them) for a second, in the order they were first seen. */
export class SeenItems {
	private since = new Map<number, number>();
	private done = new Set<number>();
	update(id: number, visible: boolean, now: number) {
		if (this.done.has(id)) return;
		const since = this.since.get(id);
		if (visible && since === undefined) this.since.set(id, now);
		if (!visible && since !== undefined) {
			if (now - since >= SEEN_MIN_MS) this.done.add(id);
			this.since.delete(id);
		}
	}
	seen(now: number): number[] {
		for (const [id, since] of this.since) if (now - since >= SEEN_MIN_MS) this.done.add(id);
		return [...this.done];
	}
}

/** A field the browser refused to submit: which one and why (valueMissing, typeMismatch…), never its value. */
export function describeInvalid(el: Element): Props | null {
	if (!(el instanceof HTMLInputElement || el instanceof HTMLSelectElement || el instanceof HTMLTextAreaElement)) return null;
	const { type, field } = fieldName(el);
	const out: Props = { field, type };
	if (!field) delete out.field;
	const v = el.validity;
	const reason = (['valueMissing', 'typeMismatch', 'patternMismatch', 'tooShort', 'tooLong', 'rangeUnderflow', 'rangeOverflow', 'stepMismatch', 'badInput', 'customError'] as const).find((k) => v[k]);
	if (reason) out.reason = reason;
	return Object.assign(out, context(el));
}

/** A Core Web Vital (from the web-vitals library) as event props: CLS keeps 4 decimals, the rest whole milliseconds. */
export function vitalProps(m: { name: string; value: number; rating: string; navigationType: string }): Props {
	return { metric: m.name, value: m.name === 'CLS' ? Math.round(m.value * 10_000) / 10_000 : Math.round(m.value), rating: m.rating, nav: m.navigationType };
}

type Nav = { hardwareConcurrency?: number; deviceMemory?: number; connection?: { effectiveType?: string; saveData?: boolean } };

/** What the device can do, for telling slow phones from slow code: CPU cores, memory (GB), network, reduced motion. */
export function deviceProps(nav: Nav, reducedMotion: boolean): Props {
	const out: Props = { reduced_motion: reducedMotion };
	if (nav.hardwareConcurrency) out.cores = nav.hardwareConcurrency;
	if (nav.deviceMemory) out.memory_gb = nav.deviceMemory;
	if (nav.connection?.effectiveType) out.net = nav.connection.effectiveType;
	if (nav.connection?.saveData) out.save_data = true;
	return out;
}

/** Facts about a page from its path: the item on product pages. */
export function pageProps(path: string): Props {
	const m = path.match(/^\/items\/(\d+)/);
	return m ? { item_id: Number(m[1]) } : {};
}

// ---- in the browser ----

const VISIT_KEY = 'merch:visit';
const randomId = () => (crypto.randomUUID?.() ?? `${Date.now()}-${Math.random()}`).replace(/[^A-Za-z0-9_-]/g, '');

let tracker: Tracker | null = null;
let visit: Visit | null = null;
// A visit that started before tracking did (the first API call can come first) is announced once it starts.
let announce = false;
type Page = {
	id: string; path: string; clock: PageClock; scroll: number; clicks: number; seen: SeenItems;
	frames: FrameStats; loaf: LongFrames; views: number; viewsMax: number; lost: number;
};
let page: Page | null = null;
const newPage = (path: string): Page => ({
	id: randomId(), path, clock: new PageClock(Date.now()), scroll: 0, clicks: 0, seen: new SeenItems(),
	frames: new FrameStats(), loaf: new LongFrames(location.origin), views: 0, viewsMax: 0, lost: 0
});
// 3D views on screen now, each reporting with page_perf too in case the tab never comes back.
const liveViews = new Set<{ report: (final: boolean) => void }>();
let seenObserver: IntersectionObserver | null = null;
let hover: { id: string; since: number } | null = null;

// Ends the current hover over an item, logging it if it lasted.
function endHover(now: number) {
	if (hover && now - hover.since >= HOVER_MIN_MS) track('item_hover', { item_id: Number(hover.id), ms: now - hover.since });
	hover = null;
}

function currentVisit(): string {
	let stored: Visit | null = null;
	try {
		stored = JSON.parse(sessionStorage.getItem(VISIT_KEY) ?? 'null');
	} catch {
		// unreadable: start a new visit
	}
	const v = nextVisit(stored ?? visit, Date.now(), randomId);
	visit = { id: v.id, last: v.last };
	try {
		sessionStorage.setItem(VISIT_KEY, JSON.stringify(visit));
	} catch {
		// private mode: the in-memory visit still works
	}
	if (v.fresh) announce = true;
	if (announce && tracker) {
		announce = false;
		tracker.track('visit_start', visitProps());
	}
	return v.id;
}

/** The visit id the API sends with every call, so server events join up with the page's. */
export const visitId = () => (typeof sessionStorage === 'undefined' ? '' : visit?.id ?? currentVisit());

function visitProps(): Props {
	const q = new URLSearchParams(location.search);
	const utm = Object.fromEntries([...q].filter(([k]) => k.startsWith('utm_') || k === 'ref').map(([k, v]) => [k, v.slice(0, 100)]));
	return {
		referrer: document.referrer.slice(0, 300),
		landing: location.pathname,
		...utm,
		screen: `${screen.width}x${screen.height}`,
		viewport: `${innerWidth}x${innerHeight}`,
		dpr: devicePixelRatio,
		lang: navigator.language,
		tz: Intl.DateTimeFormat().resolvedOptions().timeZone,
		touch: matchMedia('(pointer: coarse)').matches,
		standalone: matchMedia('(display-mode: standalone)').matches,
		automated: !!navigator.webdriver,
		...deviceProps(navigator as Nav, matchMedia('(prefers-reduced-motion: reduce)').matches)
	};
}

/** Records an event (no-op until tracking starts, and on the server). */
export function track(name: string, props: Props = {}) {
	if (!tracker) return;
	currentVisit();
	tracker.track(name, props);
}

const privatePage = () => location.pathname.startsWith('/admin');

function send(b: Batch) {
	fetch('/api/events', {
		method: 'POST',
		credentials: 'same-origin',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(b),
		keepalive: true
	}).catch(() => {});
}

function closePage(final: boolean) {
	if (!page || !tracker) return;
	page.clock.tick(Date.now(), document.visibilityState === 'visible');
	tracker.track(
		'page_time',
		{ pv: page.id, active_ms: page.clock.activeMs, visible_ms: page.clock.visibleMs, scroll_pct: page.scroll, clicks: page.clicks, seen: page.seen.seen(Date.now()), final, ...pageProps(page.path) },
		page.path
	);
	const heap = (performance as Performance & { memory?: { usedJSHeapSize: number } }).memory?.usedJSHeapSize;
	tracker.track(
		'page_perf',
		{
			pv: page.id, ...page.frames.props(), ...page.loaf.props(), views: page.views, views_max: page.viewsMax, lost: page.lost,
			...(heap ? { heap_mb: Math.round(heap / 2 ** 20) } : {}), final, ...pageProps(page.path)
		},
		page.path
	);
	for (const v of liveViews) v.report(false);
}

let gpuName: string | undefined;
// The GPU's name, read off a context the viewer already made (asking first would make one with the wrong settings).
function gpu(canvas: HTMLCanvasElement): string {
	if (canvas.dataset.gpu) return (gpuName = squash(canvas.dataset.gpu, 120));
	if (gpuName !== undefined) return gpuName;
	try {
		const gl = (canvas.getContext('webgl2') ?? canvas.getContext('webgl')) as WebGLRenderingContext | null;
		if (!gl) return '';
		const ext = gl.getExtension('WEBGL_debug_renderer_info');
		gpuName = squash(String(gl.getParameter(ext ? ext.UNMASKED_RENDERER_WEBGL : gl.RENDERER)), 120);
	} catch {
		gpuName = '';
	}
	return gpuName;
}

export type Meter = {
	/** The viewer is built (its code downloaded, its scene made). */
	loaded(): void;
	/** Its images are in and it has drawn for real. */
	ready(): void;
	/** Draws a frame, timing it. */
	draw(render: () => void): void;
	/** A frame with nothing to draw. */
	idle(): void;
	/** It couldn't be built (no WebGL, a failed download). */
	fail(): void;
	/** It's gone: reports it for the last time. */
	end(): void;
};

/** Measures one 3D view (view: product, tile or haul; kind: what it shows) and reports it as view3d when it ends. */
export function meterView(canvas: HTMLCanvasElement, view: string, kind: string): Meter {
	const id = randomId();
	const stats = new ViewStats(performance.now());
	const path = location.pathname;
	const item = context(canvas).item_id ?? pageProps(path).item_id;
	let lost = false;
	let failed = false;
	let done = false;
	let gpuLabel = '';
	if (page) {
		page.views++;
		page.viewsMax = Math.max(page.viewsMax, liveViews.size + 1);
	}
	const onLost = () => {
		lost = true;
		if (page) page.lost++;
	};
	canvas.addEventListener('webglcontextlost', onLost);
	const live = {
		report(final: boolean) {
			tracker?.track(
				'view3d',
				{
					vid: id, view, kind, ...(item ? { item_id: item } : {}), ...stats.props(performance.now()),
					px: `${canvas.width}x${canvas.height}`, dpr: devicePixelRatio, gl: canvas.dataset.gl ?? 'own', gpu: gpuLabel, lost, failed, final
				},
				path
			);
		}
	};
	liveViews.add(live);
	return {
		loaded() {
			stats.loaded(performance.now());
			gpuLabel = gpu(canvas);
		},
		ready: () => stats.ready(performance.now()),
		draw(render) {
			const t = performance.now();
			render();
			stats.rendered(t, performance.now() - t);
		},
		idle: () => stats.idle(),
		fail: () => (failed = true),
		end() {
			if (done) return;
			done = true;
			canvas.removeEventListener('webglcontextlost', onLost);
			liveViews.delete(live);
			live.report(true);
		}
	};
}

/** Starts tracking (once). Call from the root layout. */
export function startTracking() {
	if (tracker || typeof window === 'undefined') return;
	tracker = new Tracker({ send, now: Date.now, visit: () => visit?.id ?? currentVisit(), path: () => location.pathname, release: __MERCH_RELEASE__ });
	currentVisit();

	const input = () => page?.clock.input(Date.now());
	for (const ev of ['pointerdown', 'keydown', 'wheel', 'touchstart', 'pointermove']) addEventListener(ev, input, { passive: true, capture: true });

	addEventListener('scroll', () => {
		input();
		if (!page) return;
		const h = document.documentElement.scrollHeight;
		const pct = h > 0 ? Math.min(100, Math.round(((scrollY + innerHeight) / h) * 100)) : 100;
		page.scroll = Math.max(page.scroll, pct);
	}, { passive: true });

	addEventListener('click', (e) => {
		if (page) page.clicks++;
		if (privatePage() || !(e.target instanceof Element)) return;
		track('click', describeClick(e.target));
	}, { capture: true });

	addEventListener('change', (e) => {
		if (privatePage() || !(e.target instanceof Element)) return;
		const f = describeField(e.target);
		if (f) track('field', f);
	}, { capture: true });

	// How long the pointer rests on each item (rows, tiles, product pages).
	addEventListener('pointerover', (e) => {
		if (e.pointerType !== 'mouse' || !(e.target instanceof Element)) return;
		const id = e.target.closest('[data-item]')?.getAttribute('data-item') ?? '';
		if (id === (hover?.id ?? '')) return;
		const now = Date.now();
		endHover(now);
		if (id) hover = { id, since: now };
	}, { passive: true });

	// The browser blocked a submit: which fields it refused and why.
	addEventListener('invalid', (e) => {
		if (privatePage() || !(e.target instanceof Element)) return;
		const f = describeInvalid(e.target);
		if (f) track('field_invalid', f);
	}, { capture: true });

	// Which items people actually saw (half on screen for a second), reported with page_time.
	if ('IntersectionObserver' in window) {
		seenObserver = new IntersectionObserver((entries) => {
			const now = Date.now();
			for (const en of entries) {
				const id = Number(en.target.getAttribute('data-item'));
				if (id) page?.seen.update(id, en.isIntersecting, now);
			}
		}, { threshold: 0.5 });
		const watch = (n: Node) => {
			if (!(n instanceof Element)) return;
			if (n.matches('[data-item]')) seenObserver?.observe(n);
			n.querySelectorAll('[data-item]').forEach((e) => seenObserver?.observe(e));
		};
		watch(document.body);
		new MutationObserver((ms) => ms.forEach((m) => m.addedNodes.forEach(watch))).observe(document.body, { childList: true, subtree: true });
	}

	// How smoothly pages run: every animation frame's time, and the long ones' scripts (Chrome).
	const frame = (t: number) => {
		if (document.visibilityState === 'visible') page?.frames.tick(t);
		requestAnimationFrame(frame);
	};
	requestAnimationFrame(frame);
	if (typeof PerformanceObserver !== 'undefined' && PerformanceObserver.supportedEntryTypes?.includes('long-animation-frame')) {
		new PerformanceObserver((list) => list.getEntries().forEach((e) => page?.loaf.add(e as unknown as Parameters<LongFrames['add']>[0]))).observe({ type: 'long-animation-frame', buffered: true });
	}

	// How fast the site feels on real devices (Core Web Vitals), once per page load.
	import('web-vitals').then(({ onCLS, onFCP, onINP, onLCP, onTTFB }) => {
		for (const on of [onCLS, onFCP, onINP, onLCP, onTTFB]) on((m) => track('web_vital', vitalProps(m)));
	}).catch(() => {});

	addEventListener('error', (e) => track('js_error', { message: squash(e.message, 200), source: (e.filename ?? '').split('?')[0].slice(-120), line: e.lineno, col: e.colno }));
	addEventListener('unhandledrejection', (e) => track('js_error', { message: squash(String(e.reason?.message ?? e.reason), 200), unhandled: true }));
	addEventListener('appinstalled', () => track('pwa_installed'));

	setInterval(() => page?.clock.tick(Date.now(), document.visibilityState === 'visible'), 1000);
	setInterval(() => tracker?.flush(), 10_000);
	document.addEventListener('visibilitychange', () => {
		page?.frames.gap();
		if (document.visibilityState === 'hidden') {
			closePage(false);
			tracker?.flush();
			setTimeout(() => tracker?.flush(), 0); // web vitals report on this same event, after us
		} else {
			page?.clock.tick(Date.now(), false);
			page?.clock.input(Date.now());
			track('page_return');
		}
	});
	addEventListener('pagehide', () => {
		closePage(true);
		tracker?.flush();
	});
}

/** Records a page view, closing the last page's timer. Call after every navigation. */
export function trackPage(path: string) {
	startTracking();
	if (!tracker || page?.path === path) return;
	const from = page?.path ?? '';
	endHover(Date.now());
	closePage(true);
	page = newPage(path);
	page.views = page.viewsMax = liveViews.size; // 3D views already up (they can mount before the first page view)
	// Items already on screen count for the new page too: re-observing reports where they are now.
	document.querySelectorAll('[data-item]').forEach((e) => {
		seenObserver?.unobserve(e);
		seenObserver?.observe(e);
	});
	track('page_view', { pv: page.id, from, ...pageProps(path) });
}
