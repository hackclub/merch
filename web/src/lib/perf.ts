// How smooth the site runs on real devices, for usage analytics (AGENTS.md,
// "Usage analytics"): frame rate and dropped frames per page, how long each 3D
// view takes to load and draw, and the long animation frames that block input.
// Pure; the browser glue that feeds these is in track.ts.

type Props = Record<string, unknown>;

const SUSPEND_MS = 5000; // a gap this long between frames is a suspended device, not a freeze
const JANK_MS = 50;
const RATES = [30, 48, 50, 60, 72, 75, 90, 100, 120, 144, 165, 240];
const round = (v: number, d = 1) => Math.round(v * 10 ** d) / 10 ** d;

/** Counts values in fixed-width buckets, so a long page keeps a small, fixed memory. */
export class Histogram {
	n = 0;
	max = 0;
	private counts: Uint32Array;
	constructor(private step: number, top: number) {
		this.counts = new Uint32Array(Math.ceil(top / step) + 1);
	}
	add(v: number) {
		this.counts[Math.min(this.counts.length - 1, Math.max(0, Math.round(v / this.step)))]++;
		this.n++;
		if (v > this.max) this.max = v;
	}
	/** The q-th quantile, to the bucket (the last bucket reads as the largest value). */
	quantile(q: number): number {
		if (!this.n) return 0;
		const rank = Math.max(1, Math.ceil(q * this.n));
		let seen = 0;
		for (let i = 0; i < this.counts.length; i++) {
			seen += this.counts[i];
			if (seen >= rank) return round(i === this.counts.length - 1 ? this.max : Math.min(this.max, i * this.step), 3);
		}
		return round(this.max, 3);
	}
	/** Each bucket's value and count, skipping empty ones. */
	*buckets(): Generator<[number, number]> {
		for (let i = 0; i < this.counts.length; i++) if (this.counts[i]) yield [i === this.counts.length - 1 ? this.max : i * this.step, this.counts[i]];
	}
}

/** Time between animation frames: frame rate, the display's refresh rate, and how many frames were dropped. */
export class FrameStats {
	private hist = new Histogram(0.5, 500);
	private elapsed = 0;
	private last: number | null = null;
	tick(now: number) {
		if (this.last !== null) {
			const d = now - this.last;
			if (d >= 0 && d < SUSPEND_MS) {
				this.hist.add(d);
				this.elapsed += d;
			}
		}
		this.last = now;
	}
	/** Frames stopped on purpose (hidden tab, nothing to draw): the next one starts fresh. */
	gap() {
		this.last = null;
	}
	props(): Props {
		const n = this.hist.n;
		if (!n) return { frames: 0 };
		const p50 = this.hist.quantile(0.5);
		const hz = RATES.reduce((a, b) => (Math.abs(b - 1000 / p50) < Math.abs(a - 1000 / p50) ? b : a));
		const vsync = 1000 / hz;
		let dropped = 0;
		let jank = 0;
		for (const [v, c] of this.hist.buckets()) {
			dropped += c * Math.max(0, Math.round(v / vsync) - 1);
			if (v > JANK_MS) jank += c;
		}
		return {
			frames: n,
			fps: round(n / (this.elapsed / 1000)),
			hz,
			frame_p50: p50,
			frame_p95: this.hist.quantile(0.95),
			frame_p99: this.hist.quantile(0.99),
			frame_max: round(this.hist.max),
			jank,
			dropped_pct: round((dropped / (n + dropped)) * 100)
		};
	}
}

/** One 3D view's life: how long it took to load and draw, how long each draw took, and its frame rate while moving. */
export class ViewStats {
	draws = 0;
	private loadAt: number | null = null;
	private readyAt: number | null = null;
	private renders = new Histogram(0.1, 100);
	private frames = new FrameStats();
	constructor(private started: number) {}
	/** The viewer's code downloaded and its scene is built. */
	loaded(now: number) {
		this.loadAt ??= now;
	}
	/** Its images are in and it has drawn for real. */
	ready(now: number) {
		this.readyAt ??= now;
	}
	/** It drew a frame, taking ms of main-thread time. */
	rendered(now: number, ms: number) {
		this.draws++;
		this.renders.add(ms);
		this.frames.tick(now);
	}
	/** A frame with nothing to draw: the next draw's gap isn't a slow frame. */
	idle() {
		this.frames.gap();
	}
	props(now: number): Props {
		const out: Props = { alive_ms: Math.round(now - this.started) };
		if (this.loadAt !== null) out.load_ms = Math.round(this.loadAt - this.started);
		if (this.readyAt !== null) out.ready_ms = Math.round(this.readyAt - this.started);
		out.draws = this.draws;
		if (this.draws) {
			out.render_p50 = this.renders.quantile(0.5);
			out.render_p95 = this.renders.quantile(0.95);
			out.render_max = round(this.renders.max);
		}
		return Object.assign(out, this.frames.props());
	}
}

/** Where a script came from: the path of ours, the whole URL (minus query) of anyone else's. */
export function scriptSource(url: string, origin: string): string {
	const bare = url.split(/[?#]/)[0];
	return (bare.startsWith(origin + '/') ? bare.slice(origin.length) : bare).slice(-120);
}

type LoafScript = { duration: number; sourceURL?: string; sourceFunctionName?: string; invoker?: string };
type Loaf = { duration: number; blockingDuration?: number; scripts?: readonly LoafScript[] };

/** Long animation frames (over 50 ms, Chrome's long-animation-frame entries): how many, how long, and whose scripts. */
export class LongFrames {
	private n = 0;
	private ms = 0;
	private blocking = 0;
	private max = 0;
	private scripts = new Map<string, { src: string; fn: string; invoker: string; ms: number; n: number }>();
	constructor(private origin: string) {}
	add(e: Loaf) {
		this.n++;
		this.ms += e.duration;
		this.blocking += e.blockingDuration ?? 0;
		this.max = Math.max(this.max, e.duration);
		for (const s of e.scripts ?? []) {
			const src = scriptSource(s.sourceURL ?? '', this.origin);
			const fn = (s.sourceFunctionName ?? '').slice(0, 60);
			const invoker = (s.invoker ?? '').slice(0, 60);
			const key = `${src}|${fn}|${invoker}`;
			const got = this.scripts.get(key) ?? { src, fn, invoker, ms: 0, n: 0 };
			got.ms += s.duration;
			got.n++;
			this.scripts.set(key, got);
		}
	}
	props(): Props {
		if (!this.n) return {};
		const top = [...this.scripts.values()].sort((a, b) => b.ms - a.ms).slice(0, 3).map((s) => ({ ...s, ms: Math.round(s.ms) }));
		return { loaf: this.n, loaf_ms: Math.round(this.ms), loaf_blocking_ms: Math.round(this.blocking), loaf_max: Math.round(this.max), loaf_scripts: top };
	}
}
