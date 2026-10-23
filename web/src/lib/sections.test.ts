import { describe, expect, it } from 'vitest';
import { sectionize, tileFact } from './sections';
import type { Item } from './types';

const base: Item = { id: 0, sku: '', name: '', description: '', images: [], visible: true, sort_order: 0, max_per_request: 3, min_per_request: 1, quick_picks: [], max_per_user: null, width_in: null, height_in: null, variants: [], finish: '', thickness_in: null, weight_oz: null, photos: [], print_files: [], packs: '', binding: '', pages: [], ships_with: [], club_only: false, staff_only: false, allow_backorder: false, clip: '' };
const item = (id: number, sku: string, sort_order = id): Item => ({ ...base, id, sku, name: sku, sort_order });

const mag = { ...item(1, 'Pri/Bok/M25/1st'), binding: 'perfect' };
const mini = item(2, 'Pri/Bok/Mini25/1st');
const shirt = item(3, 'Swa/Shirt/HC');
const poster = { ...item(4, 'Pri/HPd/11x/1st'), width_in: 11, height_in: 17 };
const card = { ...item(5, 'Pri/HPd/4x6/1st'), packs: 'postcard' };
const welcome = { ...item(6, 'Pri/Wel/4x6/1st', 9), packs: 'postcard' };
const flag = { ...item(7, 'Sti/Sti/Fla/Top'), packs: 'sticker' };
const laptops = { ...item(8, 'Sti/Bra/O&H/Lap'), packs: 'sticker' };
const mystery = item(9, 'Toy/Plush/Orph');
const haven = { ...item(10, 'Pri/Hav/4x6/1st', 1), packs: 'postcard' };
const brochure = { ...item(11, 'Pri/Hac/Tea/T26'), binding: 'trifold' };
const flyer = { ...item(12, 'Pri/Spr/8.5/Kri', 0), width_in: 8.5, height_in: 11 };
const collage = { ...item(13, 'Pri/Let/Abo/Tee'), packs: 'postcard' };
const havenPoster = { ...item(14, 'Pri/Hav/11x/1st', 0), width_in: 11, height_in: 17 };

describe('sectionize', () => {
	const all = [mag, mini, shirt, poster, card, welcome, flag, laptops, mystery, haven, brochure, flyer, collage, havenPoster];
	const got = sectionize(all);
	const ids = (key: string) => got.find((s) => s.section.key === key)!.tiles.map((t) => t.item.id);

	it('starts with the Mini-Magazine, the two postcards and the Haven poster, then the extras, stickers last', () => {
		expect(got.map((s) => s.section.key)).toEqual(['start', 'cards', 'posters', 'booklets', 'wear', 'more', 'stickers']);
		expect(got.map((s) => !!s.section.extra)).toEqual([false, true, true, true, true, true, true]);
	});

	it('lists the start items in the order we want people to pick them, whatever their sort order', () => {
		expect(ids('start')).toEqual([2, 6, 10, 14]);
	});

	it('groups the extras by what they are', () => {
		// Cards hand out better than posters, so they come first, with flyers after them.
		expect(ids('cards')).toEqual([5, 13, 12]);
		expect(ids('posters')).toEqual([4]);
		expect(ids('booklets')).toEqual([1, 11]);
		expect(ids('wear')).toEqual([3]);
		expect(ids('more')).toEqual([9]);
	});

	it('puts every item in exactly one section', () => {
		expect(got.flatMap((s) => s.tiles.map((t) => t.item.id)).sort()).toEqual(all.map((i) => i.id).sort());
	});

	it('leads stickers with the featured one, shown big, then the rest in sort order', () => {
		expect(got.find((s) => s.section.key === 'stickers')!.tiles.map((t) => [t.item.id, t.big])).toEqual([[8, true], [7, false]]);
	});

	it('leaves out empty sections', () => {
		expect(sectionize([flag]).map((s) => s.section.key)).toEqual(['stickers']);
		expect(sectionize([])).toEqual([]);
	});

	it('only makes a featured item big when its section has company', () => {
		expect(sectionize([laptops])[0].tiles[0].big).toBe(false);
	});

	it('lists stickers as the featured row, then a grid of the rest', () => {
		const stickers = got.find((s) => s.section.key === 'stickers')!;
		expect(stickers.rows.map((t) => t.item.id)).toEqual([8]);
		expect(stickers.grid.map((t) => t.item.id)).toEqual([7]);
		// Other sections are all rows.
		const start = got.find((s) => s.section.key === 'start')!;
		expect([start.rows.length, start.grid.length]).toEqual([4, 0]);
		// Without the featured sticker, the rest still go in the grid.
		expect(sectionize([flag])[0].grid.map((t) => t.item.id)).toEqual([7]);
	});
});

describe('tileFact', () => {
	it('says FREE and the one fact that matters for choosing', () => {
		expect(tileFact({ ...shirt, variants: [{ label: 'S', sku: 'a' }, { label: 'M', sku: 'b' }], max_per_user: 1 })).toBe('S M · limit 1');
		expect(tileFact({ ...flag, packs: 'sticker' })).toBe('3, 10 or packs of 50');
		expect(tileFact({ ...mini, packs: 'fives', binding: 'staple', pages: Array(12).fill('p') })).toBe('12 pages · in fives');
		expect(tileFact({ ...welcome, packs: 'postcard', width_in: 6, height_in: 4 })).toBe('6×4 in · in fives');
		expect(tileFact({ ...poster, width_in: 11, height_in: 17 })).toBe('11×17 in');
		expect(tileFact({ ...poster, width_in: 11, height_in: 17, min_per_request: 2 })).toBe('11×17 in · at least 2');
		expect(tileFact(mystery)).toBe('');
	});
});
