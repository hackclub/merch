import { describe, expect, it } from 'vitest';
import { gallery, roughFee } from './gallery';
import type { Item } from './types';

const item = (id: number, name: string, images: string[], photos: Item['photos'] = []): Item => ({
	id, sku: `S/${id}`, name, description: '', images, visible: true, sort_order: id, max_per_request: 1, max_per_user: null,
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
