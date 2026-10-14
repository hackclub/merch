import { describe, expect, it } from 'vitest';
import { formatUnitCost, itemIdFromParam, sizeStockLabel, unitCostLabel, itemPath, moveImage, thumbnail } from './items';
import type { Item } from './types';

const poster: Item = {
	id: 681, sku: 'Pri/HPd/11x/1st', name: '11x17 Hackpad Poster (Double-Sided)', description: '',
	images: ['https://cdn.example/front.jpg', 'https://cdn.example/back.jpg'],
	visible: true, sort_order: 1, max_per_request: 1, max_per_user: null, width_in: 11, height_in: 17, variants: [], finish: 'matte', thickness_in: null, photos: [], sold_in_packs: false
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
			{ sku: 'Swa/Shirt/HC/S', label: 'S', in_stock: 92 },
			{ sku: 'Swa/Shirt/HC/XL', label: 'XL', in_stock: 1122 }
		];
		expect(sizeStockLabel({ in_stock: 1214, unit_cost: null, unit_cost_max: null, variants })).toBe('S 92 · XL 1,122');
		expect(sizeStockLabel({ in_stock: 5, unit_cost: null, unit_cost_max: null, variants: [] })).toBe('');
	});
});
