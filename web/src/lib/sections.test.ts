import { describe, expect, it } from 'vitest';
import { sectionize, tileFact } from './sections';
import type { Item } from './types';

const base: Item = { id: 0, sku: '', name: '', description: '', images: [], visible: true, sort_order: 0, max_per_request: 3, min_per_request: 1, quick_picks: [], max_per_user: null, width_in: null, height_in: null, variants: [], finish: '', thickness_in: null, photos: [], packs: '', binding: '', pages: [], ships_with: [], club_only: false, clip: '' };
const item = (id: number, sku: string, sort_order = id): Item => ({ ...base, id, sku, name: sku, sort_order });

const mag = item(1, 'Pri/Bok/M25/1st');
const mini = item(2, 'Pri/Bok/Mini25/1st');
const shirt = item(3, 'Swa/Shirt/HC');
const poster = item(4, 'Pri/HPd/11x/1st');
const card = item(5, 'Pri/HPd/4x6/1st');
const postcard = item(6, 'Pri/Wel/4x6/1st');
const flag = item(7, 'Sti/Sti/Fla/Top');
const laptops = item(8, 'Sti/Bra/O&H/Lap');
const mystery = item(9, 'Toy/Plush/Orph');

describe('sectionize', () => {
	const all = [mag, mini, shirt, poster, card, postcard, flag, laptops, mystery];
	const got = sectionize(all);

	it('orders sections: magazines, Hackpad, Hack Club, anything else, stickers last', () => {
		expect(got.map((s) => s.section.key)).toEqual(['magazines', 'hackpad', 'hackclub', 'more', 'stickers']);
	});

	it('puts every item in exactly one section', () => {
		expect(got.flatMap((s) => s.tiles.map((t) => t.item.id)).sort()).toEqual(all.map((i) => i.id).sort());
	});

	it('leads with the featured item, shown big, then the rest in sort order', () => {
		const tiles = (key: string) => got.find((s) => s.section.key === key)!.tiles;
		expect(tiles('magazines').map((t) => [t.item.id, t.big])).toEqual([[2, true], [1, false]]);
		expect(tiles('stickers').map((t) => [t.item.id, t.big])).toEqual([[8, true], [7, false]]);
		expect(tiles('hackpad').map((t) => [t.item.id, t.big])).toEqual([[4, true], [5, false]]);
		expect(tiles('hackclub').map((t) => [t.item.id, t.big])).toEqual([[3, true], [6, false]]);
	});

	it('links program sections to the program', () => {
		expect(got.find((s) => s.section.key === 'hackpad')!.section.link?.href).toBe('https://hackpad.hackclub.com');
	});

	it('leaves out empty sections', () => {
		expect(sectionize([flag]).map((s) => s.section.key)).toEqual(['stickers']);
		expect(sectionize([])).toEqual([]);
	});

	it('only makes a featured item big when its section has company', () => {
		expect(sectionize([laptops])[0].tiles[0].big).toBe(false);
	});
});

describe('tileFact', () => {
	it('says FREE and the one fact that matters for choosing', () => {
		expect(tileFact({ ...shirt, variants: [{ label: 'S', sku: 'a' }, { label: 'M', sku: 'b' }], max_per_user: 1 })).toBe('Free · S M · limit 1');
		expect(tileFact({ ...flag, packs: 'sticker' })).toBe('Free · 3, 10 or packs of 50');
		expect(tileFact({ ...mini, packs: 'fives', binding: 'staple', pages: Array(12).fill('p') })).toBe('Free · 12 pages · in fives');
		expect(tileFact({ ...postcard, packs: 'postcard', width_in: 6, height_in: 4 })).toBe('Free · 6×4 in · in fives');
		expect(tileFact({ ...poster, width_in: 11, height_in: 17 })).toBe('Free · 11×17 in');
		expect(tileFact({ ...poster, width_in: 11, height_in: 17, min_per_request: 2 })).toBe('Free · 11×17 in · at least 2');
		expect(tileFact(mystery)).toBe('Free');
	});
});
