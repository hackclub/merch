import { describe, expect, it } from 'vitest';
import { ENAMEL_FILL, erode, faceColor, plating, isMetal, metalMap, isPin, largestPiece, pinGrid, pinLabel, pinSize, postSpots, reliefHeights, ringArea, traceOutline } from './pin';

/** A w × h mask from rows of '#' (pin) and '.' (backdrop). */
const grid = (rows: string[]) => {
	const h = rows.length, w = rows[0].length;
	const mask = new Uint8Array(w * h);
	rows.forEach((r, y) => [...r].forEach((c, x) => (mask[y * w + x] = c === '#' ? 1 : 0)));
	return { mask, w, h };
};

describe('isPin', () => {
	it('is the enamel finish', () => {
		expect(isPin({ finish: 'enamel' })).toBe(true);
		expect(isPin({ finish: 'gloss' })).toBe(false);
	});
});

describe('isMetal', () => {
	it('tells gold plating from red enamel', () => {
		expect(isMetal(232, 190, 90)).toBe(true); // gold
		expect(isMetal(180, 130, 40)).toBe(true); // gold in shadow
		expect(isMetal(250, 248, 240)).toBe(true); // a glint off the metal
		expect(isMetal(190, 40, 40)).toBe(false); // red enamel
		expect(isMetal(30, 60, 160)).toBe(false); // blue enamel
		expect(isMetal(60, 50, 30)).toBe(false); // too dark to be plating
	});
});

describe('metalMap', () => {
	it('smooths away a stray glint on the enamel', () => {
		const w = 5, h = 5;
		const rgba = new Uint8ClampedArray(w * h * 4);
		for (let i = 0; i < w * h; i++) rgba.set(i % w < 2 ? [232, 190, 90, 255] : [190, 40, 40, 255], i * 4);
		rgba.set([250, 250, 250, 255], (2 * w + 3) * 4); // glare in the red
		const metal = metalMap(rgba, new Uint8Array(w * h).fill(1), w, h);
		expect(metal[2 * w + 3]).toBe(0);
		expect(metal[2 * w + 0]).toBe(1);
		expect(metal[2 * w + 4]).toBe(0);
	});
});

describe('erode', () => {
	it('shaves a pixel off the edge', () => {
		const { mask, w, h } = grid(['.....', '.###.', '.###.', '.###.', '.....']);
		expect([...erode(mask, w, h)].join('')).toBe(['00000', '00000', '00100', '00000', '00000'].join(''));
	});
});

describe('faceColor', () => {
	const gold: [number, number, number] = [230, 190, 90];
	it('deepens enamel and enriches gold', () => {
		expect(faceColor(200, 50, 40, false, gold)).toEqual([164, 41, 33]);
		const [r, g, b] = faceColor(240, 200, 120, true, gold);
		expect(r - b).toBeGreaterThan(240 - 120); // more saturated
		expect(r).toBeLessThanOrEqual(255);
	});
	it('paints a white glint on the plating gold, not silver', () => {
		const [r, , b] = faceColor(250, 250, 248, true, gold);
		expect(r - b).toBeGreaterThan(100);
	});
	it('averages the plating without its glints', () => {
		const rgba = new Uint8ClampedArray(60 * 4);
		for (let i = 0; i < 60; i++) rgba.set(i < 50 ? [230, 190, 90, 255] : [255, 255, 255, 255], i * 4);
		expect(plating(rgba, new Uint8Array(60).fill(1))).toEqual([230, 190, 90]);
		expect(plating(rgba, new Uint8Array(60))).toBeNull();
	});
});

describe('pinSize', () => {
	it('keeps the photo aspect inside the measured box', () => {
		expect(pinSize(2, 1, 2.07)).toEqual({ w: 2, h: 2 / 2.07 });
		expect(pinSize(2, 1, 1.5)).toEqual({ w: 1.5, h: 1 });
		expect(pinSize(1.25, null, 1)).toEqual({ w: 1.25, h: 1.25 });
		expect(pinSize(null, null, 2)).toEqual({ w: 1, h: 0.5 });
	});
});

describe('largestPiece', () => {
	it('drops specks and fills holes', () => {
		const { mask, w, h } = grid(['#.....', '..###.', '..#.#.', '..###.', '......']);
		expect([...largestPiece(mask, w, h)].join('')).toBe(
			['000000', '001110', '001110', '001110', '000000'].join('')
		);
	});
});

describe('traceOutline', () => {
	it('traces a rectangle by its corners, clockwise', () => {
		const { mask, w, h } = grid(['.....', '.###.', '.###.', '.....']);
		const ring = traceOutline(mask, w, h);
		expect(ring).toEqual([[1, 1], [4, 1], [4, 3], [1, 3]]);
		expect(ringArea(ring)).toBe(6);
	});
	it('follows a notch', () => {
		const { mask, w, h } = grid(['###', '#..', '###']);
		const ring = traceOutline(mask, w, h, 0);
		expect(ringArea(ring)).toBe(7);
		expect(ring).toContainEqual([1, 1]);
		expect(ring).toContainEqual([1, 2]);
	});
	it('smooths pixel steps into a curve', () => {
		const rows = Array.from({ length: 40 }, (_, y) => '#'.repeat(10 + Math.floor(y / 4)).padEnd(30, '.'));
		const { mask, w, h } = grid(rows);
		const ring = traceOutline(mask, w, h, 0.3, 3);
		// No point strays more than half a pixel off the true slope, 4 rows a column.
		const edge = ring.filter(([x, y]) => y > 4 && y < 36 && x > 11);
		expect(edge.length).toBeGreaterThan(0);
		for (const [x, y] of edge) expect(Math.abs(x - (10 + y / 4))).toBeLessThan(0.9);
	});
	it('simplifies a staircase edge to a line', () => {
		const rows = Array.from({ length: 40 }, (_, y) => '#'.repeat(10 + Math.floor(y / 4)).padEnd(30, '.'));
		const { mask, w, h } = grid(rows);
		expect(traceOutline(mask, w, h, 1).length).toBeLessThan(8);
	});
});

describe('reliefHeights', () => {
	it('raises the metal and the rim above the enamel', () => {
		const { mask, w, h } = grid(['......', '.####.', '.####.', '.####.', '.####.', '......']);
		const metal = new Uint8Array(w * h);
		metal[2 * w + 2] = 1;
		const z = reliefHeights(metal, mask, w, h, 40, 0); // 40 px/in: a 1 px rim
		expect(z[0]).toBe(0);
		expect(z[1 * w + 1]).toBe(1); // rim
		expect(z[2 * w + 2]).toBe(1); // metal line
		expect(z[3 * w + 3]).toBeCloseTo(ENAMEL_FILL);
	});
});

describe('postSpots', () => {
	it('puts two posts on a wide pin, each on the pin', () => {
		// A banner sloping down to the right.
		const rows = Array.from({ length: 10 }, (_, y) => [...Array(40)].map((_, x) => (y >= Math.floor(x / 10) && y < Math.floor(x / 10) + 5 ? '#' : '.')).join(''));
		const { mask, w, h } = grid(rows);
		const spots = postSpots(mask, w, h, { w: 2, h: 0.5 });
		expect(spots).toHaveLength(2);
		for (const [fx, fy] of spots) expect(mask[Math.floor(fy * h) * w + Math.floor(fx * w)]).toBe(1);
		expect(spots[1][1]).toBeGreaterThan(spots[0][1]);
	});
	it('puts one post on a small pin', () => {
		const { mask, w, h } = grid(['###', '###', '###']);
		expect(postSpots(mask, w, h, { w: 1, h: 1 })).toEqual([[0.5, 0.5]]);
	});
});

describe('pinGrid and pinLabel', () => {
	it('lays pins in rows of 5', () => {
		expect(pinGrid(1)).toEqual({ cols: 1, rows: 1 });
		expect(pinGrid(25)).toEqual({ cols: 5, rows: 5 });
		expect(pinGrid(7)).toEqual({ cols: 5, rows: 2 });
	});
	it('counts pins', () => {
		expect(pinLabel(1)).toBe('1 pin');
		expect(pinLabel(10)).toBe('10 pins');
	});
});
