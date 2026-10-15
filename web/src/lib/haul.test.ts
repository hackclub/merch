import { describe, expect, it } from 'vitest';
import { layoutHaul, piles } from './haul';
import type { Item } from './types';

const base: Item = {
	id: 1, sku: 'Pri/A', name: 'Poster', description: '', images: ['poster.jpg'], visible: true, sort_order: 0, max_per_request: 10, min_per_request: 1, max_per_user: null,
	width_in: 11, height_in: 17, variants: [], finish: '', thickness_in: 0.01, photos: [], packs: '', binding: '', pages: [], ships_with: [], club_only: false, clip: ''
};
const mag: Item = { ...base, id: 2, name: 'Mini-Magazine', images: [], pages: ['p1.jpg', 'p2.jpg', 'p3.jpg', 'p4.jpg'], binding: 'staple', width_in: 6, height_in: 6, thickness_in: null };
const shirt: Item = { ...base, id: 3, name: 'T-Shirt', images: ['tee.jpg'], width_in: null, height_in: null, variants: [{ label: 'M', sku: 'T/M' }] };
const sticker: Item = { ...base, id: 4, name: 'Sticker', images: ['s.png'], width_in: 4, height_in: 1.5, packs: 'sticker' };

describe('piles', () => {
	it('stacks each line at true size, as tall as its count', () => {
		const [p] = piles([{ item: base, quantity: 3 }]);
		expect(p).toMatchObject({ label: '3 × Poster', art: 'poster.jpg', w: 11, d: 17 });
		expect(p.height).toBeCloseTo(0.03);
	});

	it('uses page counts for booklets, the cover for art, and labels sizes', () => {
		const [m, s, k] = piles([{ item: mag, quantity: 10 }, { item: shirt, quantity: 2, size: 'M' }, { item: sticker, quantity: 50 }]);
		expect(m.art).toBe('p1.jpg');
		expect(m.height).toBeCloseTo(10 * 4 * 0.004);
		// A folded tee: about 9.5 × 12 × 1.2 in each.
		expect(s).toMatchObject({ label: '2 × T-Shirt (M)', w: 9.5, d: 12 });
		expect(s.height).toBeCloseTo(2.4);
		expect(k.height).toBeCloseTo(50 * 0.0118);
	});

	it('treats an unmeasured sticker as a 3 in sticker, not a box', () => {
		const [p] = piles([{ item: { ...sticker, width_in: null, height_in: null }, quantity: 10 }]);
		expect(p).toMatchObject({ w: 3, d: 3 });
		expect(p.height).toBeCloseTo(10 * 0.0118);
	});
});

describe('layoutHaul', () => {
	it('sets piles out in rows, the phone beside the biggest, centred on the table', () => {
		const out = layoutHaul(piles([{ item: base, quantity: 1 }, { item: mag, quantity: 5 }]));
		expect(out.piles.map((p) => p.label)).toEqual(['1 × Poster', '5 × Mini-Magazine']);
		const [poster, magazine] = out.piles;
		// Left to right with gaps: the biggest thing, the phone right beside it for comparison, then the rest.
		expect(out.phone.x - 1.4).toBeGreaterThan(poster.x + poster.w / 2);
		expect(magazine.x - magazine.w / 2).toBeGreaterThan(out.phone.x + 1.4);
		// Centred: the left edge mirrors the right edge.
		expect(poster.x - poster.w / 2).toBeCloseTo(-out.width / 2);
		expect(magazine.x + magazine.w / 2).toBeCloseTo(out.width / 2);
	});

	it('wraps into more rows when the line gets long', () => {
		const many = piles(Array.from({ length: 8 }, (_, i) => ({ item: { ...base, id: 10 + i, name: `P${i}` }, quantity: 1 })));
		const out = layoutHaul(many);
		expect(new Set(out.piles.map((p) => p.z)).size).toBeGreaterThan(1);
		expect(out.width).toBeLessThan(8 * 11);
	});
});
