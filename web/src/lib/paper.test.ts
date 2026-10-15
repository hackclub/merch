import { describe, expect, it } from 'vitest';
import { coverCrop, finishInfo, paperHeight, sheetThickness, SURFACE_TILE_IN } from './paper';

describe('coverCrop', () => {
	it('keeps art that already matches the sheet', () => {
		expect(coverCrop(11 / 17, 11 / 17)).toEqual({ repeatX: 1, repeatY: 1, offsetX: 0, offsetY: 0 });
	});

	it('trims top and bottom when the sheet is wider than the art, like a print shop', () => {
		// The Hackpad art is 11x17 (0.647); the card is 4x6 (0.667).
		const c = coverCrop(11 / 17, 4 / 6);
		expect(c.repeatX).toBe(1);
		expect(c.repeatY).toBeCloseTo(0.9706, 4);
		expect(c.offsetY).toBeCloseTo((1 - c.repeatY) / 2, 6);
	});

	it('trims the sides when the sheet is narrower than the art', () => {
		const c = coverCrop(1, 0.5);
		expect(c).toEqual({ repeatX: 0.5, repeatY: 1, offsetX: 0.25, offsetY: 0 });
	});
});

describe('finishInfo', () => {
	it('describes each paper and how it reflects light', () => {
		expect(finishInfo('linen').label).toBe('Linen card stock');
		expect(finishInfo('gloss').roughness).toBeLessThan(finishInfo('matte').roughness);
		expect(finishInfo('').label).toBe('');
	});

	it('gives uncoated papers a soft sheen and glossy paper none', () => {
		expect(finishInfo('matte').sheen).toBeGreaterThan(0);
		expect(finishInfo('gloss').sheen).toBe(0);
	});
});

describe('paperHeight', () => {
	const N = 256;
	const std = (a: ArrayLike<number>) => {
		let m = 0;
		for (let i = 0; i < a.length; i++) m += a[i];
		m /= a.length;
		let v = 0;
		for (let i = 0; i < a.length; i++) v += (a[i] - m) ** 2;
		return Math.sqrt(v / a.length);
	};
	/** Averages k×k blocks: what's left when the sheet is ~N/k px per tile on screen. */
	const shrink = (h: Float32Array, k: number) => {
		const n = N / k;
		const out = new Float32Array(n * n);
		for (let y = 0; y < N; y++) for (let x = 0; x < N; x++) out[Math.floor(y / k) * n + Math.floor(x / k)] += h[y * N + x] / (k * k);
		return out;
	};

	it('is the same paper every visit', () => {
		expect(paperHeight(N, finishInfo('matte'))).toEqual(paperHeight(N, finishInfo('matte')));
	});

	it('keeps its tooth when the sheet is small on screen, instead of averaging to flat', () => {
		// A poster at full view is ~60 px per inch on screen.
		const h = paperHeight(N, finishInfo('matte'));
		const k = N / (60 * SURFACE_TILE_IN);
		expect(std(shrink(h, 2 ** Math.round(Math.log2(k))))).toBeGreaterThan(std(h) * 0.4);
	});

	it('tiles without a seam', () => {
		const h = paperHeight(N, finishInfo('matte'));
		let inside = 0;
		let seam = 0;
		for (let y = 0; y < N; y++) {
			inside += Math.abs(h[y * N + N / 2] - h[y * N + N / 2 - 1]);
			seam += Math.abs(h[y * N] - h[y * N + N - 1]);
		}
		expect(seam).toBeLessThan(inside * 2);
	});

	it('makes glossy paper smoother than matte, and matte smoother than uncoated', () => {
		const s = (f: string) => std(paperHeight(N, finishInfo(f)));
		expect(s('gloss')).toBeLessThan(s('matte'));
		expect(s('matte')).toBeLessThan(s('uncoated'));
	});
});

describe('sheetThickness', () => {
	it('uses the recorded thickness, or typical paper', () => {
		expect(sheetThickness({ thickness_in: 0.016 })).toBe(0.016);
		expect(sheetThickness({ thickness_in: null })).toBe(0.008);
	});
});
