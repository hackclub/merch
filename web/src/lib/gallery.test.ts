import { describe, expect, it } from 'vitest';
import { gallery, galleryRows, roughFee } from './gallery';
import type { Item } from './types';

const item = (id: number, name: string, images: string[], photos: Item['photos'] = []): Item => ({
	id, sku: `S/${id}`, name, description: '', images, visible: true, sort_order: id, max_per_request: 1, min_per_request: 1, quick_picks: [], max_per_user: null,
	width_in: null, height_in: null, variants: [], finish: '', thickness_in: null, photos, packs: '', binding: '', pages: [],
	ships_with: [], club_only: false, clip: ''
});

describe('gallery', () => {
	it('alternates real-world photos with product images, each linking to its item', () => {
		const items = [
			item(1, 'Poster', ['p.jpg'], [{ url: 'wild1.jpg', caption: 'On a wall', credit: '' }, { url: 'wild2.jpg', caption: '', credit: '' }]),
			item(2, 'Sticker', ['s.jpg']),
			item(3, 'Card', ['c.jpg'])
		];
		expect(gallery(items).map((s) => [s.src, s.real])).toEqual([
			['wild1.jpg', true], ['p.jpg', false], ['wild2.jpg', true], ['s.jpg', false], ['c.jpg', false]
		]);
		expect(gallery(items)[0]).toMatchObject({ href: '/items/1-poster', alt: 'On a wall' });
		expect(gallery(items)[2].alt).toBe('Poster');
	});

	it('flags sticker photos, which sit on the tile grey already', () => {
		const sticker = { ...item(1, 'Sticker', ['s.jpg']), packs: 'sticker' };
		expect(gallery([sticker])[0].sticker).toBe(true);
	});

	it('skips items without images', () => {
		expect(gallery([item(1, 'Box', [])])).toEqual([]);
	});
});

describe('roughFee', () => {
	it('rounds to whole dollars', () => {
		expect(roughFee(1500)).toBe('~$15 USD');
		expect(roughFee(1499)).toBe('~$15 USD');
	});
});

describe('galleryRows', () => {
	const poster = item(1, 'Poster', ['front.jpg', 'back.jpg'], [{ url: 'wild.jpg', caption: 'On a wall', credit: '' }]);
	const mag = { ...item(2, 'Magazine', ['cover.jpg']), binding: 'staple', pages: ['p1.jpg', 'p2.jpg', 'p3.jpg'] };
	const srcs = (row: { src: string }[]) => [...new Set(row.map((s) => s.src))];

	it('puts photos and products in one row, inside pages and other sides in the other', () => {
		const [top, bottom] = galleryRows([poster, mag], 1);
		expect(srcs(top)).toEqual(['wild.jpg', 'front.jpg', 'cover.jpg']);
		expect(srcs(bottom)).toEqual(['p2.jpg', 'back.jpg', 'p3.jpg']);
		expect(bottom[0]).toMatchObject({ page: true, href: '/items/2-magazine' });
	});

	it('repeats a short row so it fills the strip', () => {
		const [top] = galleryRows([poster], 8);
		expect(top.length).toBeGreaterThanOrEqual(8);
		expect(srcs(top)).toEqual(['wild.jpg', 'front.jpg']);
	});

	it('splits one row in two when there is nothing for the second', () => {
		const items = [1, 2, 3, 4].map((i) => item(i, `I${i}`, [`${i}.jpg`]));
		const [top, bottom] = galleryRows(items, 1);
		expect(srcs(top)).toEqual(['1.jpg', '3.jpg']);
		expect(srcs(bottom)).toEqual(['2.jpg', '4.jpg']);
	});
});
