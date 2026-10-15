import { describe, expect, it } from 'vitest';
import { byFilename, formatUnitCost, isBooklet, itemIdFromParam, shipsWith, sizeStockLabel, unitCostLabel, itemPath, moveImage, preview3D, previewCount, thumbnail } from './items';
import type { Item } from './types';

const poster: Item = {
	id: 681, sku: 'Pri/HPd/11x/1st', name: '11x17 Hackpad Poster (Double-Sided)', description: '',
	images: ['https://cdn.example/front.jpg', 'https://cdn.example/back.jpg'],
	visible: true, sort_order: 1, max_per_request: 1, max_per_user: null, width_in: 11, height_in: 17, variants: [], finish: 'matte', thickness_in: null, photos: [], packs: '', binding: '', pages: [], ships_with: []
};

describe('item URLs', () => {
	it('puts the id first and a readable slug after it', () => {
		expect(itemPath(poster)).toBe('/items/681-11x17-hackpad-poster-double-sided');
		expect(itemPath({ ...poster, name: '¡¡¡' })).toBe('/items/681');
	});

	it('reads the id back from the slug, so renamed items keep working', () => {
		expect(itemIdFromParam('681-11x17-hackpad-poster-double-sided')).toBe(681);
		expect(itemIdFromParam('681-an-old-name')).toBe(681);
		expect(itemIdFromParam('681')).toBe(681);
		expect(itemIdFromParam('hackpad')).toBeNull();
		expect(itemIdFromParam('0-zero')).toBeNull();
	});
});

describe('thumbnail', () => {
	it('is the first image, or empty', () => {
		expect(thumbnail(poster)).toBe('https://cdn.example/front.jpg');
		expect(thumbnail({ ...poster, images: [] })).toBe('');
	});

	it('falls back to a booklet\'s cover', () => {
		expect(thumbnail({ ...poster, images: [], pages: ['https://cdn.example/cover.jpg'] })).toBe('https://cdn.example/cover.jpg');
	});
});

describe('booklets', () => {
	const mini = { ...poster, binding: 'staple', pages: ['https://cdn.example/p1.jpg', 'https://cdn.example/p2.jpg'] };

	it('flips through only when bound and given pages', () => {
		expect(isBooklet(mini)).toBe(true);
		expect(isBooklet({ ...mini, binding: '' })).toBe(false);
		expect(isBooklet({ ...mini, pages: [mini.pages[0]] })).toBe(false);
	});

	it('orders uploaded page files by their numbers', () => {
		const files = ['mag-10.jpg', 'mag-2.jpg', 'mag-1.jpg'].map((name) => ({ name }));
		expect(byFilename(files).map((f) => f.name)).toEqual(['mag-1.jpg', 'mag-2.jpg', 'mag-10.jpg']);
	});

	it('finds what an item ships with by SKU', () => {
		const card = { ...poster, id: 9, sku: 'Pri/M25/4x6/1st' };
		const mag = { ...poster, ships_with: ['Pri/M25/4x6/1st', 'Gone/1'] };
		expect(shipsWith(mag, [poster, card])).toEqual([card]);
	});
});

describe('moveImage', () => {
	it('swaps an image with its neighbor and ignores moves off either end', () => {
		const imgs = ['a', 'b', 'c'];
		expect(moveImage(imgs, 2, -1)).toEqual(['a', 'c', 'b']);
		expect(moveImage(imgs, 0, 1)).toEqual(['b', 'a', 'c']);
		expect(moveImage(imgs, 0, -1)).toEqual(['a', 'b', 'c']);
		expect(moveImage(imgs, 2, 1)).toEqual(['a', 'b', 'c']);
		expect(imgs).toEqual(['a', 'b', 'c']);
	});
});

describe('formatUnitCost', () => {
	it('shows dollars with at least cents, keeping sub-cent precision', () => {
		expect(formatUnitCost('0.25')).toBe('$0.25');
		expect(formatUnitCost('3')).toBe('$3.00');
		expect(formatUnitCost('0.0832')).toBe('$0.0832');
		expect(formatUnitCost('1250.5')).toBe('$1,250.50');
	});

	it('is empty when the warehouse has no cost', () => {
		expect(formatUnitCost(null)).toBe('');
		expect(formatUnitCost('')).toBe('');
		expect(formatUnitCost('n/a')).toBe('');
	});
});

describe('unitCostLabel', () => {
	it('shows one cost, or a range when sizes cost different amounts', () => {
		expect(unitCostLabel({ in_stock: 5, unit_cost: '0.3682', unit_cost_max: null, variants: [] })).toBe('$0.3682 each');
		expect(unitCostLabel({ in_stock: 309, unit_cost: '13.8166', unit_cost_max: '14.2682', variants: [] })).toBe('$13.8166–$14.2682 each');
		expect(unitCostLabel({ in_stock: 0, unit_cost: null, unit_cost_max: null, variants: [] })).toBe('');
	});
});

describe('sizeStockLabel', () => {
	it('lists each size with its count, or nothing for one-size items', () => {
		const variants = [
			{ sku: 'Swa/Shirt/HC/S', label: 'S', in_stock: 92, unit_cost: null },
			{ sku: 'Swa/Shirt/HC/XL', label: 'XL', in_stock: 1122, unit_cost: null }
		];
		expect(sizeStockLabel({ in_stock: 1214, unit_cost: null, unit_cost_max: null, variants })).toBe('S 92 · XL 1,122');
		expect(sizeStockLabel({ in_stock: 5, unit_cost: null, unit_cost_max: null, variants: [] })).toBe('');
	});
});

describe('preview3D', () => {
	it('shows a sized print as a sheet with its front, back, thickness and finish, without the phone', () => {
		expect(preview3D(poster)).toEqual({
			front: 'https://cdn.example/front.jpg', back: 'https://cdn.example/back.jpg',
			widthIn: 11, heightIn: 17, thicknessIn: 0.008, finish: 'matte', phone: false
		});
	});

	it('shows a closed booklet: front cover, back cover, the whole book thick', () => {
		const mag = { ...poster, images: [], binding: 'staple', pages: ['p1', 'p2', 'p3', 'p4'], width_in: 5.5, height_in: 8.5 };
		expect(preview3D(mag)).toMatchObject({ front: 'p1', back: 'p4', thicknessIn: 0.016 });
		expect(preview3D({ ...mag, thickness_in: 0.1 })).toMatchObject({ thicknessIn: 0.1 });
	});

	it('shows stickers as stickers, even without a size', () => {
		expect(preview3D({ ...poster, packs: 'sticker', width_in: null, height_in: null })).toEqual({
			kind: 'stickers', front: 'https://cdn.example/front.jpg', widthIn: null, heightIn: null, phone: false
		});
	});

	it('is null without a size or without art', () => {
		expect(preview3D({ ...poster, width_in: null })).toBeNull();
		expect(preview3D({ ...poster, images: [] })).toBeNull();
		expect(preview3D({ ...poster, packs: 'sticker', images: [] })).toBeNull();
	});
});

describe('previewCount', () => {
	const sticker = { ...poster, packs: 'sticker' };
	it('shows a sealed bag of 50 stickers until you pick a quantity, then what you picked', () => {
		expect(previewCount(sticker, 0)).toBe(50);
		expect(previewCount(sticker, 3)).toBe(3);
		expect(previewCount(sticker, 150)).toBe(150);
	});
	it('is what you picked for everything else', () => {
		expect(previewCount(poster, 0)).toBe(0);
		expect(previewCount(poster, 2)).toBe(2);
	});
});
