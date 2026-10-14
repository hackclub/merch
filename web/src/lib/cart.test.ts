import { describe, expect, it } from 'vitest';
import { cartErrors, cartLines, emptyCart, parseCart, parseSizes, pruneCart, pruneSizes, setQuantity, sizeLabel, totalQuantity } from './cart';
import type { Item } from './types';

const items: Item[] = [
	{ id: 1, sku: 'Sti/A', name: 'Stickers', description: '', images: [], visible: true, sort_order: 0, max_per_request: 3, max_per_user: null, width_in: null, height_in: null, variants: [] },
	{ id: 2, sku: 'Swa/Sox', name: 'Socks', description: '', images: [], visible: true, sort_order: 1, max_per_request: 1, max_per_user: 1, width_in: null, height_in: null, variants: [] },
	{
		id: 3, sku: 'Swa/Shirt/HC', name: 'T-shirt', description: '', images: [], visible: true, sort_order: 2, max_per_request: 1, max_per_user: null, width_in: null, height_in: null,
		variants: [{ label: 'S', sku: 'Swa/Shirt/HC/S' }, { label: 'M', sku: 'Swa/Shirt/HC/M' }]
	}
];

describe('cart', () => {
	it('clamps quantity to the per-request max and drops zero lines', () => {
		let c = emptyCart();
		c = setQuantity(c, items[0], 10);
		expect(c[1]).toBe(3);
		c = setQuantity(c, items[0], 0);
		expect(c[1]).toBeUndefined();
		c = setQuantity(c, items[1], -4);
		expect(c[2]).toBeUndefined();
	});

	it('builds API lines in catalog order', () => {
		let c = emptyCart();
		c = setQuantity(c, items[1], 1);
		c = setQuantity(c, items[0], 2);
		expect(cartLines(c, items)).toEqual([
			{ item_id: 1, quantity: 2 },
			{ item_id: 2, quantity: 1 }
		]);
		expect(totalQuantity(c)).toBe(3);
	});

	it('reports empty and over-limit carts', () => {
		expect(cartErrors(emptyCart(), 5)).toEqual(['Pick at least one item.']);
		let c = setQuantity(emptyCart(), items[0], 3);
		c = setQuantity(c, items[1], 1);
		expect(cartErrors(c, 3)).toEqual(['You can request at most 3 items at a time.']);
		expect(cartErrors(c, 0)).toEqual([]);
	});

	it('restores a saved cart, ignoring junk', () => {
		expect(parseCart('{"1":2,"2":1}')).toEqual({ 1: 2, 2: 1 });
		expect(parseCart('{"1":2,"x":3,"2":-1,"3":"many","4":1.5}')).toEqual({ 1: 2 });
		expect(parseCart('not json')).toEqual({});
		expect(parseCart('[1,2]')).toEqual({});
		expect(parseCart(null)).toEqual({});
	});

	it('prunes a restored cart to what is listed now', () => {
		// item 9 was unlisted since; item 1's max dropped below the saved quantity
		const listed = [{ ...items[0], max_per_request: 2 }, items[1]];
		expect(pruneCart({ 1: 3, 2: 1, 9: 4 }, listed)).toEqual({ 1: 2, 2: 1 });
	});

	it('sends the chosen size of sized items', () => {
		const c = setQuantity(setQuantity(emptyCart(), items[0], 1), items[2], 1);
		expect(cartLines(c, items, { 3: 'Swa/Shirt/HC/M' })).toEqual([
			{ item_id: 1, quantity: 1 },
			{ item_id: 3, quantity: 1, sku: 'Swa/Shirt/HC/M' }
		]);
	});

	it('asks for a size before a sized item can be requested', () => {
		const c = setQuantity(emptyCart(), items[2], 1);
		expect(cartErrors(c, 5, items, {})).toEqual(['Pick a size for T-shirt.']);
		expect(cartErrors(c, 5, items, { 3: 'Swa/Shirt/HC/XXL' })).toEqual(['Pick a size for T-shirt.']);
		expect(cartErrors(c, 5, items, { 3: 'Swa/Shirt/HC/S' })).toEqual([]);
	});

	it('restores and prunes saved sizes', () => {
		expect(parseSizes('{"3":"Swa/Shirt/HC/S","x":"y","4":5}')).toEqual({ 3: 'Swa/Shirt/HC/S' });
		expect(parseSizes('nope')).toEqual({});
		// sizes no longer offered (or for items without sizes) are dropped
		expect(pruneSizes({ 1: 'Sti/A', 3: 'Swa/Shirt/HC/XL', 9: 'x' }, items)).toEqual({});
		expect(pruneSizes({ 3: 'Swa/Shirt/HC/M' }, items)).toEqual({ 3: 'Swa/Shirt/HC/M' });
	});

	it('labels a size by its SKU', () => {
		expect(sizeLabel(items[2], 'Swa/Shirt/HC/M')).toBe('M');
		expect(sizeLabel(items[2], undefined)).toBe('');
	});
});
