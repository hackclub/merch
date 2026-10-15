import { describe, expect, it } from 'vitest';
import { foldedShirtTop, shirtCrop, SHIRT_PHOTO } from './shirt';

const W = 9.5, D = 12, T = 1.2;

describe('foldedShirtTop', () => {
	it('is full height in the middle and comes down to the table at the folds', () => {
		const mid = foldedShirtTop(0, 2, W, D, T);
		expect(mid.y).toBeGreaterThan(T * 0.9);
		expect(mid.y).toBeLessThanOrEqual(T * 1.05);
		expect(foldedShirtTop(W / 2, 2, W, D, T).y).toBeCloseTo(0);
		expect(foldedShirtTop(0, D / 2, W, D, T).y).toBeCloseTo(0);
		// The fold rounds over: a little in from the edge is already most of the way up.
		expect(foldedShirtTop(W / 2 - 0.5, 2, W, D, T).y).toBeGreaterThan(T * 0.6);
	});

	it('is outside the shirt past the sloped shoulders at the back corners', () => {
		expect(foldedShirtTop(W / 2 - 0.2, -D / 2 + 0.2, W, D, T).inside).toBe(false);
		expect(foldedShirtTop(0, -D / 2 + 0.2, W, D, T).inside).toBe(true);
		expect(foldedShirtTop(W / 2 - 0.2, 0, W, D, T).inside).toBe(true);
	});

	it('has a raised rib collar around a sunken, shaded neck opening near the back', () => {
		const neck = foldedShirtTop(0, -D / 2 + 1.4, W, D, T);
		const chest = foldedShirtTop(0, 0, W, D, T);
		expect(neck.y).toBeLessThan(chest.y * 0.75);
		expect(neck.shade).toBeLessThan(0.8);
		expect(chest.shade).toBe(1);
		// The front of the collar sits a little above the chest beside it.
		let rib = 0;
		for (let z = -D / 2 + 1.5; z < -D / 2 + 3.5; z += 0.02) rib = Math.max(rib, foldedShirtTop(0, z, W, D, T).y);
		expect(rib).toBeGreaterThan(chest.y);
	});
});

describe('shirtCrop', () => {
	// The shirt, sleeves and all, in a 100×100 mask of its photo.
	const box = { x: 13, y: 7, w: 74, h: 86 };

	it('takes the chest of the shirt, from just above the collar down, at the fold’s aspect', () => {
		const c = shirtCrop(box, 100, 100, 1, W / D);
		expect(c.u0 + c.w / 2).toBeCloseTo(0.5, 2);
		expect(c.w).toBeCloseTo(0.36, 1);
		expect(c.v0).toBeGreaterThan(0.04);
		expect(c.v0).toBeLessThan(0.075);
		expect(c.h).toBeCloseTo(c.w * (D / W), 5);
	});

	it('falls back to where a centred product shot has its chest when the shirt isn’t found', () => {
		expect(shirtCrop(null, 10, 10, 1, W / D)).toEqual({ ...SHIRT_PHOTO, h: SHIRT_PHOTO.w / (W / D) });
	});
});
