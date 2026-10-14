import { describe, expect, it } from 'vitest';
import { itemIdFromParam, itemPath, moveImage, thumbnail } from './items';
import type { Item } from './types';

const poster: Item = {
	id: 681, sku: 'Pri/HPd/11x/1st', name: '11x17 Hackpad Poster (Double-Sided)', description: '',
	images: ['https://cdn.example/front.jpg', 'https://cdn.example/back.jpg'],
	visible: true, sort_order: 1, max_per_request: 1, max_per_user: null
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
