import { describe, expect, it } from 'vitest';
import { heavyNote, tooHeavy, weightCrossing, weightLimitProps, weightMeter, type TooHeavy } from './weight';
import type { Packing } from './shipping';

const packing = (weight_oz: number, limit_oz?: number, unknown_weight = false): Packing => ({
	package: { name: 'Bubble mailer', class: 'parcel' },
	weight_oz,
	contents_oz: weight_oz - 1,
	unknown_weight,
	limit_oz
});

// What the server's 3 lb 422 says (internal/httpapi/shipping.go).
const heavy: TooHeavy = {
	error: 'international requests can weigh at most 3 lb packed; this is about 4.6 lb packed',
	weight_oz: 73.6,
	limit_oz: 48,
	tare_oz: 14.8,
	contents_oz: 58.8,
	package: '18x12x6 box',
	lines: [
		{ item_id: 4, name: 'Magazine 2025', quantity: 5, oz: 52.6 },
		{ item_id: 2, name: 'Welcome Postcard', quantity: 20, oz: 4.8 },
		{ item_id: 1, name: 'Hackpad Poster', quantity: 1, oz: 1.2 },
		{ item_id: 3, name: 'Kawaii Sticker', quantity: 4, oz: 0.2 }
	]
};

describe('weightMeter', () => {
	it('shows the packed weight against the limit the server gave', () => {
		const m = weightMeter(packing(30, 48), null)!;
		expect(m).toMatchObject({ weightOz: 30, limitOz: 48, over: false, overOz: 0, heaviest: [] });
		expect(m.pct).toBeCloseTo(62.5);
		expect(m.text).toBe('1.9 lb of the 3 lb limit abroad, packed.');
	});

	it('is nothing without a limit (the US) or a quote', () => {
		expect(weightMeter(packing(30), null)).toBeNull();
		expect(weightMeter(null, null)).toBeNull();
		expect(weightMeter(undefined, null)).toBeNull();
	});

	it('is still under at exactly the limit', () => {
		const m = weightMeter(packing(48, 48), null)!;
		expect(m.over).toBe(false);
		expect(m.pct).toBe(100);
	});

	it('says "at least" when some item has no weight', () => {
		expect(weightMeter(packing(10, 48, true), null)!.text).toBe('At least 10 oz of the 3 lb limit abroad, packed.');
	});

	it('over the limit, gives the excess and the 3 heaviest lines by name', () => {
		const m = weightMeter(null, heavy)!;
		expect(m).toMatchObject({ weightOz: 73.6, limitOz: 48, over: true, pct: 100 });
		expect(m.overOz).toBeCloseTo(25.6);
		expect(m.heaviest.map((l) => l.item_id)).toEqual([4, 2, 1]);
		expect(m.text).toBe('4.6 lb packed: 1.6 lb over the 3 lb limit abroad. Take some out to send it.');
		expect(m.heaviestText).toBe('Heaviest: 5 × Magazine 2025 (3.3 lb), 20 × Welcome Postcard (4.8 oz), 1 × Hackpad Poster (1.2 oz).');
	});

	it('prefers the refusal over a stale quote', () => {
		expect(weightMeter(packing(30, 48), heavy)!.over).toBe(true);
	});
});

describe('tooHeavy', () => {
	it('reads the 422 body, and nothing else', () => {
		expect(tooHeavy(heavy)).toEqual(heavy);
		expect(tooHeavy({ error: "these won't ship together in one package" })).toBeNull();
		expect(tooHeavy(undefined)).toBeNull();
		expect(tooHeavy('nope')).toBeNull();
	});
});

describe('heavyNote', () => {
	it('marks the heaviest lines in the list when over', () => {
		const m = weightMeter(null, heavy);
		expect(heavyNote(m, 4)).toBe('Heaviest in your box: 3.3 lb.');
		expect(heavyNote(m, 3)).toBe('');
		expect(heavyNote(weightMeter(packing(30, 48), null), 4)).toBe('');
		expect(heavyNote(null, 4)).toBe('');
	});
});

describe('weightCrossing', () => {
	it('fires once each time the request goes over, and re-arms once it is back under', () => {
		const over = weightMeter(null, heavy);
		const under = weightMeter(packing(30, 48), null);
		let wasOver = false;
		const fired: boolean[] = [];
		for (const m of [under, over, over, under, over, null, over]) {
			const c = weightCrossing(wasOver, m);
			fired.push(c.fire);
			wasOver = c.over;
		}
		expect(fired).toEqual([false, true, false, false, true, false, true]);
	});
});

describe('weightLimitProps', () => {
	it('gives the numbers and the country, never the item names', () => {
		const p = weightLimitProps(weightMeter(null, heavy)!, 'de');
		expect(p).toEqual({ over_oz: 25.6, weight_oz: 73.6, lines: 4, country: 'DE' });
	});
});
