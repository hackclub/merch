import { describe, expect, it } from 'vitest';
import { FrameStats, Histogram, LongFrames, scriptSource, ViewStats } from './perf';

describe('Histogram', () => {
	it('reads quantiles off its buckets, never past the largest value', () => {
		const h = new Histogram(1, 250);
		for (let i = 0; i < 90; i++) h.add(16.7);
		for (let i = 0; i < 10; i++) h.add(48);
		expect(h.n).toBe(100);
		expect(h.quantile(0.5)).toBe(17);
		expect(h.quantile(0.95)).toBe(48);
		expect(h.max).toBe(48);
	});

	it('puts values past the top in the last bucket but keeps the true max', () => {
		const h = new Histogram(1, 100);
		h.add(5);
		h.add(900);
		expect(h.quantile(1)).toBe(900);
		expect(h.max).toBe(900);
		h.add(1950.0999999999995);
		expect(h.quantile(1)).toBe(1950.1); // no float noise in the event
	});

	it('is empty-safe', () => {
		expect(new Histogram(1, 10).quantile(0.5)).toBe(0);
	});
});

describe('FrameStats', () => {
	it('measures frame rate and how many frames were dropped against the display rate', () => {
		const f = new FrameStats();
		let t = 0;
		f.tick(t);
		for (let i = 0; i < 95; i++) f.tick((t += 16.7));
		for (let i = 0; i < 5; i++) f.tick((t += 50)); // each misses two vsyncs
		const p = f.props();
		expect(p.frames).toBe(100);
		expect(p.hz).toBe(60);
		expect(p.fps).toBeCloseTo(100 / (t / 1000), 0);
		expect(p.frame_p50).toBe(16.5);
		expect(p.frame_p99).toBe(50);
		expect(p.jank).toBe(0); // 50 ms isn't over 50
		expect(p.dropped_pct).toBeCloseTo((10 / 110) * 100, 0);
	});

	it('works out a 120 Hz display', () => {
		const f = new FrameStats();
		for (let i = 0; i <= 50; i++) f.tick(i * 8.33);
		expect(f.props().hz).toBe(120);
		expect(f.props().dropped_pct).toBe(0);
	});

	it('skips the gap after a pause (hidden tab, idle 3D view)', () => {
		const f = new FrameStats();
		f.tick(0);
		f.tick(16);
		f.gap();
		f.tick(10_000);
		f.tick(10_016);
		expect(f.props().frames).toBe(2);
		expect(f.props().frame_max).toBe(16);
	});

	it('counts long freezes while visible, but not a suspended device', () => {
		const f = new FrameStats();
		f.tick(0);
		f.tick(400);
		f.tick(60_000);
		const p = f.props();
		expect(p.frames).toBe(1);
		expect(p.jank).toBe(1);
		expect(p.frame_max).toBe(400);
	});

	it('reports nothing without frames', () => {
		expect(new FrameStats().props()).toEqual({ frames: 0 });
	});
});

describe('ViewStats', () => {
	it('times loading and drawing, and frame rate only between back-to-back draws', () => {
		const v = new ViewStats(1000);
		v.loaded(1400);
		v.ready(1900);
		v.rendered(1900, 4.2);
		v.rendered(1916.7, 3.1);
		v.rendered(1933.4, 2.9);
		v.idle(); // sitting still: the next draw's gap isn't a slow frame
		v.rendered(9000, 3);
		const p = v.props(9000);
		expect(p.load_ms).toBe(400);
		expect(p.ready_ms).toBe(900);
		expect(p.draws).toBe(4);
		expect(p.render_p50).toBe(3);
		expect(p.render_max).toBe(4.2);
		expect(p.frames).toBe(2);
		expect(p.alive_ms).toBe(8000);
	});

	it('leaves out what never happened', () => {
		const p = new ViewStats(0).props(50);
		expect(p).toEqual({ alive_ms: 50, draws: 0, frames: 0 });
	});
});

describe('LongFrames', () => {
	it('sums long animation frames and names the scripts that took longest', () => {
		const lf = new LongFrames('https://merch.hackclub.com');
		lf.add({ duration: 120, blockingDuration: 70, scripts: [
			{ duration: 80, sourceURL: 'https://merch.hackclub.com/_app/immutable/chunks/paper3d.abc.js?x=1', sourceFunctionName: 'render', invoker: 'FrameRequestCallback' },
			{ duration: 10, sourceURL: '', sourceFunctionName: '', invoker: 'IMG#hero.onload' }
		] });
		lf.add({ duration: 300, blockingDuration: 250, scripts: [
			{ duration: 200, sourceURL: 'https://merch.hackclub.com/_app/immutable/chunks/paper3d.abc.js', sourceFunctionName: 'render', invoker: 'FrameRequestCallback' }
		] });
		expect(lf.props()).toEqual({
			loaf: 2,
			loaf_ms: 420,
			loaf_blocking_ms: 320,
			loaf_max: 300,
			loaf_scripts: [
				{ src: '/_app/immutable/chunks/paper3d.abc.js', fn: 'render', invoker: 'FrameRequestCallback', ms: 280, n: 2 },
				{ src: '', fn: '', invoker: 'IMG#hero.onload', ms: 10, n: 1 }
			]
		});
	});

	it('reports nothing without long frames', () => {
		expect(new LongFrames('https://x').props()).toEqual({});
	});
});

describe('scriptSource', () => {
	it('keeps the path of our scripts and the host of others, never the query', () => {
		expect(scriptSource('https://merch.hackclub.com/_app/a.js?sig=1#x', 'https://merch.hackclub.com')).toBe('/_app/a.js');
		expect(scriptSource('chrome-extension://abc/content.js', 'https://merch.hackclub.com')).toBe('chrome-extension://abc/content.js');
		expect(scriptSource('https://js.stripe.com/v3/fingerprinted/x.js?q', 'https://merch.hackclub.com')).toBe('https://js.stripe.com/v3/fingerprinted/x.js');
	});
});
