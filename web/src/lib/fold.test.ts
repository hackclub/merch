import { describe, expect, it } from 'vitest';
import { FOLD_CYCLE_MS, FOLD_STEPS, foldAngles, foldCycle, foldLabel, foldedSize, panelU } from './fold';

describe('foldAngles', () => {
	it('is folded shut at 0: the flap tucked flat inside, the cover on top, sprung open a hair', () => {
		const a = foldAngles(0);
		expect(a.flap).toBeCloseTo(Math.PI);
		expect(a.cover).toBeLessThan(Math.PI);
		expect(a.cover).toBeGreaterThan(Math.PI - 0.1);
	});

	it('opens the cover first, then the flap', () => {
		const one = foldAngles(1);
		expect(one.cover).toBeLessThan(0.5); // the cover lies open
		expect(one.flap).toBeGreaterThan(Math.PI - 0.3); // the flap still folded, though it lifts a little
		expect(one.flap).toBeLessThan(Math.PI);
		const two = foldAngles(FOLD_STEPS);
		expect(two.flap).toBeLessThan(0.5);
		expect(two.cover).toBeCloseTo(one.cover);
	});

	it('keeps the creases: open flat, both outer panels still tip in a little', () => {
		const a = foldAngles(FOLD_STEPS);
		expect(a.cover).toBeGreaterThan(0);
		expect(a.flap).toBeGreaterThan(0);
	});

	it("doesn't lift the flap until the cover is well clear of it", () => {
		for (let p = 0; p <= 0.6; p += 0.05) expect(foldAngles(p).flap).toBeCloseTo(Math.PI);
		expect(foldAngles(0.6).cover).toBeLessThan(Math.PI / 2);
	});

	it('moves smoothly and only one way as it opens, and clamps outside 0…2', () => {
		let last = foldAngles(0);
		for (let p = 0.02; p <= 2.0001; p += 0.02) {
			const a = foldAngles(p);
			expect(a.cover).toBeLessThanOrEqual(last.cover + 1e-9);
			expect(a.flap).toBeLessThanOrEqual(last.flap + 1e-9);
			expect(Math.abs(a.cover - last.cover)).toBeLessThan(0.2);
			expect(Math.abs(a.flap - last.flap)).toBeLessThan(0.2);
			last = a;
		}
		expect(foldAngles(-1)).toEqual(foldAngles(0));
		expect(foldAngles(5)).toEqual(foldAngles(2));
	});
});

describe('panelU', () => {
	// The outside art reads flap | back | cover; the inside art is the other side of the sheet.
	it('maps each panel to its third of the outside and inside art', () => {
		expect(panelU('flap', 'outside')).toEqual([0, 1 / 3]);
		expect(panelU('back', 'outside')).toEqual([1 / 3, 2 / 3]);
		expect(panelU('cover', 'outside')).toEqual([2 / 3, 1]);
		// Behind the cover (the right third outside) is the left third inside.
		expect(panelU('cover', 'inside')).toEqual([0, 1 / 3]);
		expect(panelU('back', 'inside')).toEqual([1 / 3, 2 / 3]);
		expect(panelU('flap', 'inside')).toEqual([2 / 3, 1]);
	});
});

describe('foldedSize', () => {
	it('is a third as wide and three sheets thick', () => {
		expect(foldedSize(11, 8.5, 0.005)).toEqual({ w: 11 / 3, h: 8.5, t: 0.015 });
	});
});

describe('foldLabel', () => {
	it('names each step', () => {
		expect(foldLabel(0)).toBe('Folded');
		expect(foldLabel(1)).toBe('Cover open');
		expect(foldLabel(2)).toBe('Open flat');
	});
});

describe('foldCycle', () => {
	it('starts folded, opens the cover, then the flap, holds it open, then folds back up, on a loop', () => {
		expect(foldCycle(0)).toBe(0);
		expect(foldCycle(2500)).toBe(1);
		expect(foldCycle(5000)).toBe(2);
		expect(foldCycle(9500)).toBe(0);
		for (const t of [0, 2500, 5000, 9500]) expect(foldCycle(t + FOLD_CYCLE_MS)).toBe(foldCycle(t));
	});

	it('spends most of its time open, so people can read the inside', () => {
		let open = 0;
		for (let t = 0; t < FOLD_CYCLE_MS; t += 10) if (foldCycle(t) === 2) open++;
		expect(open / (FOLD_CYCLE_MS / 10)).toBeGreaterThan(0.35);
	});
});
