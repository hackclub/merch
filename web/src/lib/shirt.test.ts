import { describe, expect, it } from 'vitest';
import { clothDepth, edgeDistance, fabricColor, foldedShirtTop, isProductShot, shirtCrop, shirtSize, SHIRT_PHOTO } from './shirt';

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

describe('shirtSize', () => {
	it('is a unisex tee’s chest width and body length in inches, M for sizes it doesn’t know', () => {
		expect(shirtSize('M')).toEqual({ chest: 20, length: 29 });
		expect(shirtSize('XL')).toEqual({ chest: 24, length: 31 });
		expect(shirtSize('s')).toEqual({ chest: 18, length: 28 });
		expect(shirtSize('Youth L')).toEqual(shirtSize('M'));
	});
});

// A w × h RGBA image: `paint` gives each pixel's color.
function image(w: number, h: number, paint: (x: number, y: number) => [number, number, number, number?]) {
	const px = new Uint8ClampedArray(w * h * 4);
	for (let y = 0; y < h; y++)
		for (let x = 0; x < w; x++) {
			const [r, g, b, a = 255] = paint(x, y);
			px.set([r, g, b, a], (y * w + x) * 4);
		}
	return px;
}
const inBox = (x: number, y: number) => x >= 10 && x < 30 && y >= 8 && y < 34;

describe('isProductShot', () => {
	it('is a shirt alone on a plain backdrop, or a cutout', () => {
		expect(isProductShot(image(40, 40, (x, y) => (inBox(x, y) ? [20, 22, 40] : [255, 255, 255])), 40, 40)).toBe(true);
		expect(isProductShot(image(40, 40, (x, y) => (inBox(x, y) ? [200, 30, 40] : [0, 0, 0, 0])), 40, 40)).toBe(true);
	});

	it('is not a photo of people at an event, whose edges are busy', () => {
		const busy = image(40, 40, (x, y) => [(x * 37 + y * 11) % 256, (x * 5 + y * 53) % 256, (x * y) % 256]);
		expect(isProductShot(busy, 40, 40)).toBe(false);
		// Nor a crop that runs off the shirt at the edges.
		expect(isProductShot(image(40, 40, (x) => (x < 30 ? [20, 22, 40] : [255, 255, 255])), 40, 40)).toBe(false);
	});
});

describe('edgeDistance', () => {
	it('counts pixels in from the nearest edge of the mask, 0 outside', () => {
		const w = 9, h = 9;
		const mask = new Uint8Array(w * h).map((_, i) => (i % w >= 1 && i % w <= 7 && Math.floor(i / w) >= 1 && Math.floor(i / w) <= 7 ? 1 : 0));
		const d = edgeDistance(mask, w, h);
		expect(d[0]).toBe(0);
		expect(d[4 * w + 1]).toBe(1);
		expect(d[4 * w + 4]).toBeCloseTo(4, 5);
		expect(d[2 * w + 2]).toBeCloseTo(2, 5);
	});
});

describe('clothDepth', () => {
	it('rounds over from nothing at the edge to the full depth a little way in', () => {
		expect(clothDepth(0, 3, 2)).toBe(0);
		expect(clothDepth(3, 3, 2)).toBeCloseTo(2);
		expect(clothDepth(10, 3, 2)).toBeCloseTo(2);
		expect(clothDepth(0.75, 3, 2)).toBeGreaterThan(2 * 0.6);
		expect(clothDepth(1, 3, 2)).toBeLessThan(clothDepth(2, 3, 2));
	});
});

describe('fabricColor', () => {
	it('is the color of the cloth near the edges, not the print in the middle', () => {
		const w = 40, h = 40;
		const px = image(w, h, (x, y) => (!inBox(x, y) ? [255, 255, 255] : x >= 14 && x < 26 && y >= 12 && y < 30 ? [240, 240, 250] : [20, 22, 40]));
		const mask = new Uint8Array(w * h).map((_, i) => (inBox(i % w, Math.floor(i / w)) ? 1 : 0));
		expect(fabricColor(px, mask, edgeDistance(mask, w, h), w)).toEqual([20, 22, 40]);
	});
});
