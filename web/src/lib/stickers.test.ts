import { describe, expect, it } from 'vitest';
import { maskBounds, packLabel, packLayout, PER_BAG, STICKER_THICKNESS_IN, stickerMask, stickerSize } from './stickers';

// A w×h RGBA image filled with bg, with rects painted on top.
function image(w: number, h: number, bg: number[], rects: { x: number; y: number; w: number; h: number; rgb: number[] }[]) {
	const data = new Uint8ClampedArray(w * h * 4);
	for (let i = 0; i < w * h; i++) data.set([...bg, 255], i * 4);
	for (const r of rects)
		for (let y = r.y; y < r.y + r.h; y++) for (let x = r.x; x < r.x + r.w; x++) data.set([...r.rgb, 255], (y * w + x) * 4);
	return data;
}

const BG = [240, 239, 235]; // the light grey the product photos sit on

describe('stickerMask', () => {
	it('cuts a sticker out of its product photo, white border and all', () => {
		const data = image(10, 10, BG, [
			{ x: 2, y: 2, w: 6, h: 6, rgb: [255, 255, 255] }, // white die-cut border
			{ x: 3, y: 3, w: 4, h: 4, rgb: [230, 40, 40] }
		]);
		const m = stickerMask(data, 10, 10);
		expect(m[0]).toBe(0);
		expect(m[2 * 10 + 2]).toBe(1);
		expect(m[5 * 10 + 5]).toBe(1);
		expect(m.reduce((a, b) => a + b, 0)).toBe(36);
	});

	it('treats the soft drop shadow as background', () => {
		const data = image(10, 10, BG, [
			{ x: 3, y: 4, w: 6, h: 5, rgb: [205, 204, 201] }, // shadow, offset down-right
			{ x: 2, y: 2, w: 6, h: 6, rgb: [230, 40, 40] }
		]);
		expect(stickerMask(data, 10, 10).reduce((a, b) => a + b, 0)).toBe(36);
	});

	it('keeps background-colored holes inside the sticker', () => {
		const data = image(10, 10, BG, [
			{ x: 1, y: 1, w: 8, h: 8, rgb: [20, 20, 20] },
			{ x: 4, y: 4, w: 2, h: 2, rgb: BG }
		]);
		expect(stickerMask(data, 10, 10)[4 * 10 + 4]).toBe(1);
	});

	it('uses transparency when the art has it', () => {
		const data = image(4, 4, [0, 0, 0], [{ x: 1, y: 1, w: 2, h: 2, rgb: [0, 0, 0] }]);
		for (let i = 0; i < 16; i++) data[i * 4 + 3] = 0;
		for (const i of [5, 6, 9, 10]) data[i * 4 + 3] = 255;
		expect([...stickerMask(data, 4, 4)]).toEqual([0, 0, 0, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 0, 0, 0]);
	});
});

describe('packLabel', () => {
	it('says how many stickers and bags', () => {
		expect(packLabel(0)).toBe('1 sticker');
		expect(packLabel(3)).toBe('3 stickers');
		expect(packLabel(50)).toBe('50 stickers, in a bag');
		expect(packLabel(150)).toBe('150 stickers, in 3 bags of 50');
	});
});

describe('maskBounds', () => {
	it('finds the box around the sticker', () => {
		const m = new Uint8Array(100);
		m[2 * 10 + 3] = m[6 * 10 + 7] = 1;
		expect(maskBounds(m, 10, 10)).toEqual({ x: 3, y: 2, w: 5, h: 5 });
	});
	it('is null for an empty mask', () => {
		expect(maskBounds(new Uint8Array(4), 2, 2)).toBeNull();
	});
});

describe('stickerSize', () => {
	it('uses the recorded size', () => {
		expect(stickerSize({ width_in: 2, height_in: 4 }, 1)).toEqual({ w: 2, h: 4 });
	});
	it('assumes about 3 inches across the long side', () => {
		expect(stickerSize({ width_in: null, height_in: null }, 2)).toEqual({ w: 3, h: 1.5 });
		expect(stickerSize({ width_in: null, height_in: null }, 0.5)).toEqual({ w: 1.5, h: 3 });
	});
});

describe('thickness', () => {
	it('makes a bag of 50 about 15 mm tall, like the real bundles', () => {
		expect(PER_BAG * STICKER_THICKNESS_IN * 25.4).toBeCloseTo(15, 0);
	});
});

describe('packLayout', () => {
	it('shows one sticker before any are requested', () => {
		expect(packLayout(0)).toEqual({ loose: 1, bags: [] });
	});
	it('spreads out small packs so you can count them', () => {
		expect(packLayout(3)).toEqual({ loose: 3, bags: [] });
		expect(packLayout(10)).toEqual({ loose: 10, bags: [] });
	});
	it('lays bags of 50 side by side, four across, then piles them up', () => {
		expect(packLayout(50).bags).toEqual([{ pile: 0, level: 0 }]);
		expect(packLayout(150).bags).toEqual([{ pile: 0, level: 0 }, { pile: 1, level: 0 }, { pile: 2, level: 0 }]);
		const { loose, bags } = packLayout(300);
		expect(loose).toBe(0);
		expect(bags).toHaveLength(6);
		expect(bags.filter((b) => b.pile === 0)).toHaveLength(2);
		expect(bags.at(-1)).toEqual({ pile: 1, level: 1 });
	});
	it('keeps any leftover loose next to the bags', () => {
		expect(packLayout(60)).toEqual({ loose: 10, bags: [{ pile: 0, level: 0 }] });
	});
});
