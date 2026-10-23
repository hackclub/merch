import { describe, expect, it } from 'vitest';
import { bookletStack, LAPTOP, layoutHaul, LOFT, piles, pickedLines, requestHaul, TABLE, tableCover, tall } from './haul';
import { PIN_GAP_IN, PIN_PILE_H } from './pin';
import { PHONE } from './scale';
import type { Item } from './types';

const base: Item = {
	id: 1, sku: 'Pri/A', name: 'Poster', description: '', images: ['poster.jpg'], visible: true, sort_order: 0, max_per_request: 10, min_per_request: 1, quick_picks: [], max_per_user: null,
	width_in: 11, height_in: 17, variants: [], finish: '', thickness_in: 0.01, weight_oz: null, photos: [], print_files: [], packs: '', binding: '', pages: [], ships_with: [], club_only: false, staff_only: false, model: '', allow_backorder: false, clip: ''
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
		// Stapled booklets don't lie flat, so the stack stands taller than its paper.
		expect(m.height).toBeCloseTo(10 * 4 * 0.004 * LOFT.staple);
		// A folded tee: about 9.5 × 12 × 1.2 in each.
		expect(s).toMatchObject({ label: '2 × T-Shirt (M)', w: 9.5, d: 12 });
		expect(s.height).toBeCloseTo(2.4);
		expect(k.height).toBeCloseTo(50 * 0.0118);
	});

	it('gives postcards card-stock thickness when none is set', () => {
		const card: Item = { ...base, id: 5, name: 'Postcard', width_in: 6, height_in: 4, thickness_in: null, weight_oz: null, packs: 'postcard' };
		expect(piles([{ item: card, quantity: 25 }])[0].height).toBeCloseTo(0.35);
	});

	it('splits postcards into banded bundles of 50, as they ship', () => {
		const card: Item = { ...base, id: 6, name: 'Postcard', width_in: 6, height_in: 4, thickness_in: 0.014, weight_oz: null, packs: 'postcard' };
		const out = piles([{ item: card, quantity: 100 }]);
		expect(out.map((p) => [+p.height.toFixed(3), p.band])).toEqual([[0.7, true], [0.7, true]]);
		expect(piles([{ item: card, quantity: 25 }]).map((p) => !!p.band)).toEqual([false]);
	});

	it('treats an unmeasured sticker as a 3 in sticker, not a box', () => {
		const [p] = piles([{ item: { ...sticker, width_in: null, height_in: null }, quantity: 10 }]);
		expect(p).toMatchObject({ w: 3, d: 3, kind: 'sticker', count: 10 });
		expect(p.height).toBeCloseTo(10 * 0.0118);
		expect(p.bag).toBeFalsy();
	});

	it('splits stickers into individual packs of 50 plus any loose remainder', () => {
		const bags = piles([{ item: sticker, quantity: 150 }]);
		expect(bags).toHaveLength(3);
		expect(bags.map((p) => [p.label, +p.height.toFixed(4), p.bag, p.count, p.kind])).toEqual([
			['50 × Sticker', +(50 * 0.0118).toFixed(4), true, 50, 'sticker'],
			['50 × Sticker', +(50 * 0.0118).toFixed(4), true, 50, 'sticker'],
			['50 × Sticker', +(50 * 0.0118).toFixed(4), true, 50, 'sticker']
		]);

		const mixed = piles([{ item: sticker, quantity: 60 }]);
		expect(mixed.map((p) => [p.label, !!p.bag, p.count])).toEqual([
			['50 × Sticker', true, 50],
			['10 × Sticker', false, 10]
		]);
	});

	it('includes companion items from ships_with when given catalog includes', () => {
		const card: Item = { ...base, id: 9, sku: 'Pri/M25/4x6/1st', name: 'Intro Card', width_in: 6, height_in: 4, thickness_in: 0.014 };
		const fullMag: Item = { ...mag, id: 8, name: 'Magazine 2025', ships_with: ['Pri/M25/4x6/1st'] };
		const out = piles([{ item: fullMag, quantity: 5 }], [card]);
		expect(out.map((p) => [p.label, p.kind, p.count])).toEqual([
			['5 × Magazine 2025', 'booklet', 5],
			['5 × Intro Card', 'paper', 5]
		]);
	});
});

describe('3D models', () => {
	const plush: Item = { ...base, id: 6, sku: 'Swa/HC/Plu/Orp', name: 'Orpheus Plushie', images: ['plush.jpg'], model: 'orpheus.glb', width_in: 8, height_in: 10, thickness_in: null };

	it('sit side by side, three to a row, at their real height', () => {
		const [p] = piles([{ item: plush, quantity: 4 }]);
		expect(p).toMatchObject({ kind: 'model', model: 'orpheus.glb', count: 4, art: 'plush.jpg', widthIn: 8, heightIn: 10, height: 10 });
		expect(p.w).toBeCloseTo(3 * 8);
		expect(p.d).toBeCloseTo(2 * 10);
	});

	it('take a plush toy\'s usual size without measurements', () => {
		const [p] = piles([{ item: { ...plush, width_in: null, height_in: null }, quantity: 1 }]);
		expect(p).toMatchObject({ kind: 'model', w: 8, d: 10, height: 10, heightIn: 10 });
	});
});

describe('enamel pins', () => {
	const pin: Item = { ...base, id: 5, name: 'Pin', images: ['pin.jpg'], width_in: 2, height_in: 1, thickness_in: 0.375, finish: 'enamel' };

	it('lie face up in rows of 5, standing on their posts', () => {
		const [p] = piles([{ item: pin, quantity: 7 }]);
		expect(p).toMatchObject({ kind: 'pin', count: 7, art: 'pin.jpg', widthIn: 2, heightIn: 1, height: PIN_PILE_H });
		expect(p.w).toBeCloseTo(5 * 2 + 4 * PIN_GAP_IN);
		expect(p.d).toBeCloseTo(2 * 1 + PIN_GAP_IN);
	});

	it('take about an inch each without measurements', () => {
		const [p] = piles([{ item: { ...pin, width_in: null, height_in: null }, quantity: 1 }]);
		expect(p).toMatchObject({ kind: 'pin', w: 1, d: 1 });
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

describe('layoutHaul on tables', () => {
	const inside = (p: { x: number; z: number; w: number; d: number }, t: { x: number; z: number; w: number; d: number }) =>
		p.x - p.w / 2 >= t.x - t.w / 2 && p.x + p.w / 2 <= t.x + t.w / 2 && p.z - p.d / 2 >= t.z - t.d / 2 && p.z + p.d / 2 <= t.z + t.d / 2;

	it('sets everything on a 6-foot folding table', () => {
		const out = layoutHaul(piles([{ item: base, quantity: 2 }, { item: mag, quantity: 50 }, { item: sticker, quantity: 100 }]), { table: true });
		expect(out.tables).toHaveLength(1);
		expect(out.tables[0]).toMatchObject({ w: TABLE.w, d: TABLE.d });
		for (const p of [...out.piles, { ...out.phone, w: 2.8, d: 5.8 }]) expect(inside(p, out.tables[0])).toBe(true);
		expect(out.width).toBe(TABLE.w);
	});

	it('centres the piles on the table, like a booth display', () => {
		const out = layoutHaul(piles([{ item: mag, quantity: 10 }]), { table: true });
		const all = [...out.piles.map((p) => ({ x: p.x, z: p.z, w: p.w, d: p.d })), { ...out.phone, w: 2.8, d: 5.8 }, { ...out.laptop!, w: LAPTOP.w, d: LAPTOP.d }];
		const left = Math.min(...all.map((p) => p.x - p.w / 2)), right = Math.max(...all.map((p) => p.x + p.w / 2));
		const back = Math.min(...all.map((p) => p.z - p.d / 2)), front = Math.max(...all.map((p) => p.z + p.d / 2));
		expect((left + right) / 2).toBeCloseTo(out.tables[0].x);
		expect((back + front) / 2).toBeCloseTo(out.tables[0].z);
	});

	it('spills onto another table when one is full', () => {
		const posters = piles(Array.from({ length: 12 }, (_, i) => ({ item: { ...base, id: 20 + i, name: `P${i}` }, quantity: 2 })));
		const out = layoutHaul(posters, { table: true });
		expect(out.tables.length).toBeGreaterThan(1);
		for (const p of out.piles) expect(out.tables.some((t) => inside(p, t))).toBe(true);
	});

	it('keeps the piles close together instead of spreading them across the table', () => {
		const out = layoutHaul(piles([{ item: base, quantity: 15 }, { item: mag, quantity: 50 }, { item: sticker, quantity: 300 }]), { table: true });
		const all = [...out.piles, { ...out.phone, w: 2.8, d: 5.8 }, { ...out.laptop!, ...LAPTOP }];
		const left = Math.min(...all.map((p) => p.x - p.w / 2)), right = Math.max(...all.map((p) => p.x + p.w / 2));
		expect(right - left).toBeLessThan(TABLE.w * 0.6);
	});

	it('sets a laptop on the table for scale, clear of the piles', () => {
		const out = layoutHaul(piles([{ item: base, quantity: 2 }, { item: mag, quantity: 50 }]), { table: true });
		const laptop = { ...out.laptop!, w: LAPTOP.w, d: LAPTOP.d };
		expect(inside(laptop, out.tables[0])).toBe(true);
		const overlaps = (a: typeof laptop, b: typeof laptop) =>
			Math.abs(a.x - b.x) < (a.w + b.w) / 2 && Math.abs(a.z - b.z) < (a.d + b.d) / 2;
		for (const p of out.piles) expect(overlaps(laptop, p)).toBe(false);
		expect(layoutHaul(piles([{ item: base, quantity: 1 }])).laptop).toBeNull();
	});

	it('has no table unless asked', () => {
		expect(layoutHaul(piles([{ item: base, quantity: 1 }])).tables).toEqual([]);
	});
});

describe('tableCover', () => {
	it('says how much of the table the piles cover', () => {
		const out = layoutHaul(piles([{ item: base, quantity: 2 }]), { table: true });
		expect(tableCover(out)).toBeCloseTo((11 * 17) / (TABLE.w * TABLE.d));
		expect(tableCover(layoutHaul(piles([{ item: base, quantity: 2 }])))).toBe(0);
	});
});

describe('bound booklets', () => {
	it('loft: stapled ones puff up more than perfect-bound ones, both more than flat paper', () => {
		expect(LOFT.staple).toBeGreaterThan(LOFT.perfect);
		expect(LOFT.perfect).toBeGreaterThan(1);
		const perfect: Item = { ...mag, binding: 'perfect', thickness_in: 0.3 };
		expect(piles([{ item: perfect, quantity: 2 }])[0].height).toBeCloseTo(0.6 * LOFT.perfect);
	});

	it('bookletStack piles each one on the last, a little askew, the same way every time', () => {
		const s = bookletStack(20, 0.05);
		expect(s).toHaveLength(20);
		expect(s[0].y).toBe(0);
		expect(s[19].y).toBeCloseTo(19 * 0.05);
		expect(bookletStack(20, 0.05)).toEqual(s);
		// Askew, but only slightly: never more than a tenth of an inch or a few degrees.
		expect(new Set(s.map((b) => b.dx)).size).toBeGreaterThan(5);
		for (const b of s) {
			expect(Math.abs(b.dx)).toBeLessThanOrEqual(0.1);
			expect(Math.abs(b.dz)).toBeLessThanOrEqual(0.1);
			expect(Math.abs(b.yaw)).toBeLessThanOrEqual(0.05);
		}
	});
});

describe('pickedLines', () => {
	it('shows the amount picked, or the smallest quick pick before anything is picked', () => {
		const mini: Item = { ...mag, quick_picks: [5, 10, 25, 50, 100], max_per_request: 100 };
		expect(pickedLines(mini, 25)).toEqual([{ item: mini, quantity: 25 }]);
		expect(pickedLines(mini, 0)).toEqual([{ item: mini, quantity: 5 }]);
	});
});

describe('tall', () => {
	it('says how tall in inches and centimetres', () => {
		expect(tall(4)).toBe('4 in tall (10 cm)');
		expect(tall(0.7)).toBe('0.7 in tall (1.8 cm)');
	});
});

describe('trifold brochures', () => {
	const brochure: Item = { ...base, id: 5, name: 'Brochure', binding: 'trifold', images: ['out.jpg', 'in.jpg'], width_in: 11, height_in: 8.5, thickness_in: 0.005 };

	it('lie folded, cover up: a third as wide, three sheets thick each, a little puffed at the creases', () => {
		const [p] = piles([{ item: brochure, quantity: 10 }]);
		expect(p.fold).toEqual({ outside: 'out.jpg', inside: 'in.jpg', w: 11 / 3, sheet: 0.005 });
		expect(p.d).toBe(8.5);
		expect(LOFT.trifold).toBeGreaterThan(1);
		expect(p.height).toBeCloseTo(10 * 0.015 * LOFT.trifold);
		expect(p.art).toBe('out.jpg');
		// The cover is the right third of the outside art.
		expect(p.artPart).toEqual({ from: 2 / 3, to: 1, aspect: 11 / 8.5 });
	});

	it('leave room on the table for the top one to open out flat', () => {
		const [p] = piles([{ item: brochure, quantity: 10 }]);
		expect(p.w).toBe(11);
		const { piles: placed, phone } = layoutHaul([p]);
		expect(Math.abs(placed[0].x - phone.x)).toBeGreaterThanOrEqual(11 / 2 + PHONE.w / 2);
	});

	it('is a flat sheet without its inside art', () => {
		const [p] = piles([{ item: { ...brochure, images: ['out.jpg'] }, quantity: 1 }]);
		expect(p.w).toBe(11);
		expect(p.artPart).toBeUndefined();
		expect(p.fold).toBeUndefined();
	});
});

describe('requestHaul', () => {
	it('pairs each line with its item and size, leaving out unsent lines and unknown items', () => {
		const lines = [
			{ item_id: 2, sku: 'Pri/Mag', name: 'Mini-Magazine', quantity: 5 },
			{ item_id: 3, sku: 'T/M', name: 'T-Shirt', variant: 'M', quantity: 1 },
			{ item_id: 1, sku: 'Pri/A', name: 'Poster', quantity: 0, requested_quantity: 2 },
			{ item_id: 99, sku: 'Gone', name: 'Gone', quantity: 1 }
		];
		expect(requestHaul(lines, [base, mag, shirt])).toEqual([
			{ item: mag, quantity: 5 },
			{ item: shirt, quantity: 1, size: 'M' }
		]);
	});
});
