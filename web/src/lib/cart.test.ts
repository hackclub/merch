import { describe, expect, it } from 'vitest';
import { cartErrors, cartLines, emptyCart, packQuantities, parseCart, parseQuickPicks, presetQuantities, quantityProblem, parseSizes, pruneCart, pruneSizes, quantityOptions, setQuantity, setSizeQuantity, sizeLabel, sizesSummary, stickerNudge, totalQuantity } from './cart';
import type { Item } from './types';

const items: Item[] = [
	{ id: 1, sku: 'Sti/A', name: 'Stickers', description: '', images: [], visible: true, sort_order: 0, max_per_request: 3, min_per_request: 1, quick_picks: [], max_per_user: null, width_in: null, height_in: null, variants: [], finish: '', thickness_in: null, weight_oz: null, photos: [], print_files: [], packs: '', binding: '', pages: [], ships_with: [], club_only: false, allow_backorder: false, clip: '' },
	{ id: 2, sku: 'Swa/Sox', name: 'Socks', description: '', images: [], visible: true, sort_order: 1, max_per_request: 1, min_per_request: 1, quick_picks: [], max_per_user: 1, width_in: null, height_in: null, variants: [], finish: '', thickness_in: null, weight_oz: null, photos: [], print_files: [], packs: '', binding: '', pages: [], ships_with: [], club_only: false, allow_backorder: false, clip: '' },
	{
		id: 3, sku: 'Swa/Shirt/HC', name: 'T-shirt', description: '', images: [], visible: true, sort_order: 2, max_per_request: 1, min_per_request: 1, quick_picks: [], max_per_user: null, width_in: null, height_in: null, finish: '', thickness_in: null, weight_oz: null, photos: [], print_files: [], packs: '', binding: '', pages: [], ships_with: [], club_only: false, allow_backorder: false, clip: '',
		variants: [{ label: 'S', sku: 'Swa/Shirt/HC/S' }, { label: 'M', sku: 'Swa/Shirt/HC/M' }]
	}
];

const postcard: Item = { ...items[0], id: 4, sku: 'Pri/Wel/4x6/1st', name: 'Postcard', max_per_request: 150, min_per_request: 1, quick_picks: [], packs: 'postcard' };
const sticker: Item = { ...items[0], id: 5, sku: 'Sti/Sti/Kaw/1st', name: 'Kawaii sticker', max_per_request: 150, min_per_request: 1, quick_picks: [], packs: 'sticker' };
const miniMag: Item = { ...items[0], id: 6, sku: 'Pri/Bok/Mini25/1st', name: 'Mini-Magazine', max_per_request: 25, min_per_request: 1, quick_picks: [], packs: 'fives' };

describe('packs', () => {
	it('lists 5 to 25 by fives, then multiples of 50, up to the max', () => {
		expect(packQuantities('postcard', 150)).toEqual([5, 10, 15, 20, 25, 50, 100, 150]);
		expect(packQuantities('postcard', 60)).toEqual([5, 10, 15, 20, 25, 50]);
		expect(packQuantities('postcard', 4)).toEqual([]);
		expect(quantityOptions(postcard)).toEqual([0, 5, 10, 15, 20, 25, 50, 100, 150]);
	});

	it('lists fives as every multiple of 5 up to the max', () => {
		expect(packQuantities('fives', 35)).toEqual([5, 10, 15, 20, 25, 30, 35]);
		expect(packQuantities('fives', 4)).toEqual([]);
		expect(quantityOptions(miniMag)).toEqual([0, 5, 10, 15, 20, 25]);
		let c = setQuantity(emptyCart(), miniMag, 1);
		expect(c[6]).toBe(5);
		c = setQuantity(c, miniMag, 12);
		expect(c[6]).toBe(10);
	});

	it('lists stickers as 3, 10, then multiples of 50', () => {
		expect(packQuantities('sticker', 200)).toEqual([3, 10, 50, 100, 150, 200]);
		expect(packQuantities('sticker', 49)).toEqual([3, 10]);
		expect(quantityOptions(sticker)).toEqual([0, 3, 10, 50, 100, 150]);
		let c = setQuantity(emptyCart(), sticker, 1);
		expect(c[5]).toBe(3);
		c = setQuantity(c, sticker, 25);
		expect(c[5]).toBe(10);
		expect(quantityOptions(items[0])).toBeUndefined();
	});

	it('snaps pack quantities to an allowed size', () => {
		let c = setQuantity(emptyCart(), postcard, 1);
		expect(c[4]).toBe(5);
		c = setQuantity(c, postcard, 30);
		expect(c[4]).toBe(25);
		c = setQuantity(c, postcard, 75);
		expect(c[4]).toBe(50);
		c = setQuantity(c, postcard, 1000);
		expect(c[4]).toBe(150);
		c = setQuantity(c, postcard, 0);
		expect(c[4]).toBeUndefined();
		expect(setQuantity(emptyCart(), { ...postcard, max_per_request: 4 }, 5)[4]).toBeUndefined();
	});

});

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

	it('reports empty carts, and has no cap on how many different items', () => {
		expect(cartErrors(emptyCart())).toEqual(['Pick at least one item.']);
		let c = setQuantity(emptyCart(), items[0], 3);
		c = setQuantity(c, items[1], 1);
		c = setQuantity(c, postcard, 50);
		expect(cartErrors(c, [...items, postcard])).toEqual([]);
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

	const shirt: Item = { ...items[2], max_per_request: 3 };

	it('holds a quantity per size, and the item total in the cart', () => {
		let { cart, sizes } = setSizeQuantity(emptyCart(), {}, shirt, 'Swa/Shirt/HC/S', 1);
		({ cart, sizes } = setSizeQuantity(cart, sizes, shirt, 'Swa/Shirt/HC/M', 2));
		expect(sizes).toEqual({ 3: { 'Swa/Shirt/HC/S': 1, 'Swa/Shirt/HC/M': 2 } });
		expect(cart).toEqual({ 3: 3 });
		// The item's max counts every size together.
		({ cart, sizes } = setSizeQuantity(cart, sizes, shirt, 'Swa/Shirt/HC/S', 5));
		expect(sizes[3]).toEqual({ 'Swa/Shirt/HC/S': 1, 'Swa/Shirt/HC/M': 2 });
		({ cart, sizes } = setSizeQuantity(cart, sizes, shirt, 'Swa/Shirt/HC/M', 0));
		expect(sizes).toEqual({ 3: { 'Swa/Shirt/HC/S': 1 } });
		expect(cart).toEqual({ 3: 1 });
		({ cart, sizes } = setSizeQuantity(cart, sizes, shirt, 'Swa/Shirt/HC/S', 0));
		expect([cart, sizes]).toEqual([{}, {}]);
	});

	it('sends one line per size, in size order', () => {
		const c = setQuantity(emptyCart(), items[0], 1);
		const { cart, sizes } = setSizeQuantity(setSizeQuantity(c, {}, shirt, 'Swa/Shirt/HC/M', 2).cart, { 3: { 'Swa/Shirt/HC/M': 2 } }, shirt, 'Swa/Shirt/HC/S', 1);
		expect(cartLines(cart, [items[0], shirt], sizes)).toEqual([
			{ item_id: 1, quantity: 1 },
			{ item_id: 3, quantity: 1, sku: 'Swa/Shirt/HC/S' },
			{ item_id: 3, quantity: 2, sku: 'Swa/Shirt/HC/M' }
		]);
	});

	it('asks for a size before a sized item can be requested', () => {
		const c = setQuantity(emptyCart(), items[2], 1);
		expect(cartErrors(c, items, {})).toEqual(['Pick a size for T-shirt.']);
		expect(cartErrors(c, items, { 3: { 'Swa/Shirt/HC/XXL': 1 } })).toEqual(['Pick a size for T-shirt.']);
		expect(cartErrors(c, items, { 3: { 'Swa/Shirt/HC/S': 1 } })).toEqual([]);
	});

	it('restores and prunes saved sizes, reading the old one-size format too', () => {
		expect(parseSizes('{"3":{"Swa/Shirt/HC/S":1,"Swa/Shirt/HC/M":2,"bad":-1},"x":{"a":1},"4":5}')).toEqual({ 3: { 'Swa/Shirt/HC/S': 1, 'Swa/Shirt/HC/M': 2 } });
		expect(parseSizes('{"3":"Swa/Shirt/HC/S"}')).toEqual({ 3: { 'Swa/Shirt/HC/S': 1 } });
		expect(parseSizes('nope')).toEqual({});
		// sizes no longer offered (or for items without sizes) are dropped
		expect(pruneSizes({ 1: { 'Sti/A': 1 }, 3: { 'Swa/Shirt/HC/XL': 1 }, 9: { x: 1 } }, items)).toEqual({});
		expect(pruneSizes({ 3: { 'Swa/Shirt/HC/M': 1, 'Swa/Shirt/HC/XL': 1 } }, items)).toEqual({ 3: { 'Swa/Shirt/HC/M': 1 } });
	});

	it("takes a sized item's cart quantity from its sizes", () => {
		expect(pruneCart({ 1: 1, 3: 1 }, [items[0], shirt], { 3: { 'Swa/Shirt/HC/S': 1, 'Swa/Shirt/HC/M': 1 } })).toEqual({ 1: 1, 3: 2 });
		expect(pruneCart({ 3: 1 }, [shirt], {})).toEqual({});
	});

	it('summarizes the sizes in the bag', () => {
		expect(sizesSummary(shirt, { 3: { 'Swa/Shirt/HC/M': 2, 'Swa/Shirt/HC/S': 1 } })).toBe('1 S, 2 M');
		expect(sizesSummary(shirt, {})).toBe('');
	});

	it('labels a size by its SKU', () => {
		expect(sizeLabel(items[2], 'Swa/Shirt/HC/M')).toBe('M');
		expect(sizeLabel(items[2], undefined)).toBe('');
	});
});

describe('minimums', () => {
	const poster: Item = { ...items[0], min_per_request: 2, max_per_request: 20 };

	it('adds an item at its minimum, and stepping below it takes the item out', () => {
		let c = setQuantity(emptyCart(), poster, 1);
		expect(c).toEqual({ 1: 2 });
		c = setQuantity(c, poster, 3);
		expect(c).toEqual({ 1: 3 });
		c = setQuantity(setQuantity(c, poster, 2), poster, 1);
		expect(c).toEqual({});
	});
});

describe('presetQuantities', () => {
	it('offers the first few pack sizes for items that come in packs', () => {
		expect(presetQuantities(postcard)).toEqual([5, 10, 25, 50, 100]);
		expect(presetQuantities({ ...postcard, max_per_request: 30 })).toEqual([5, 10, 25]);
		expect(presetQuantities(sticker)).toEqual([3, 10, 50, 100, 150]);
		expect(presetQuantities({ ...miniMag, max_per_request: 100 })).toEqual([5, 10, 15, 20, 25, 30]);
	});

	it('offers round counts up to the max for everything else', () => {
		expect(presetQuantities(items[0])).toEqual([1, 2, 3]);
		expect(presetQuantities({ ...items[0], max_per_request: 40 })).toEqual([1, 2, 3, 5, 10, 15]);
		// Mirrors the old Hackpad poster form: minimum 2, maximum 20.
		expect(presetQuantities({ ...items[0], min_per_request: 2, max_per_request: 20 })).toEqual([2, 3, 5, 10, 15, 20]);
		expect(presetQuantities({ ...items[0], min_per_request: 4, max_per_request: 12 })).toEqual([4, 5, 10]);
		// Set on the admin Items tab: the full-size magazine offers 1, 3, 5, 10 (Custom goes up to its max of 25).
		expect(presetQuantities({ ...items[0], max_per_request: 25, quick_picks: [1, 3, 5, 10] })).toEqual([1, 3, 5, 10]);
		expect(presetQuantities({ ...items[0], max_per_request: 4, quick_picks: [1, 3, 5, 10] })).toEqual([1, 3]);
		expect(presetQuantities(items[1])).toEqual([1]);
	});
});

describe('parseQuickPicks', () => {
	it('reads amounts typed on the admin Items tab', () => {
		expect(parseQuickPicks('1, 3,5 10')).toEqual([1, 3, 5, 10]);
		expect(parseQuickPicks('10, 3, 3, x, 0')).toEqual([3, 10]);
		expect(parseQuickPicks('')).toEqual([]);
	});
});

describe('quantityProblem', () => {
	it('accepts any amount the pack rules allow, typed by hand', () => {
		for (const q of [5, 10, 15, 25, 50, 150]) expect(quantityProblem(postcard, q)).toBe('');
		for (const q of [3, 10, 50, 100]) expect(quantityProblem(sticker, q)).toBe('');
		for (const q of [5, 20, 25]) expect(quantityProblem(miniMag, q)).toBe('');
		expect(quantityProblem(items[0], 2)).toBe('');
	});

	it('says what the rule is when an amount breaks it', () => {
		expect(quantityProblem(postcard, 7)).toBe('Postcards come in 5, 10, 15, 20 or 25, then multiples of 50.');
		expect(quantityProblem(postcard, 30)).toBe('Postcards come in 5, 10, 15, 20 or 25, then multiples of 50.');
		expect(quantityProblem(sticker, 20)).toBe('Stickers come in 3 or 10, then packs of 50.');
		expect(quantityProblem(miniMag, 12)).toBe('These come in multiples of 5.');
		expect(quantityProblem(postcard, 200)).toBe('At most 150 per request.');
		expect(quantityProblem(items[0], 4)).toBe('At most 3 per request.');
		expect(quantityProblem({ ...items[0], min_per_request: 2 }, 1)).toBe('At least 2 per request.');
	});
});

describe('stickerNudge', () => {
	const designs = (n: number, q: number) =>
		Array.from({ length: n }, (_, i) => ({ ...sticker, id: 100 + i, name: `Sticker ${i}` }))
			.reduce<{ items: Item[]; cart: Record<number, number> }>((a, s) => ({ items: [...a.items, s], cart: { ...a.cart, [s.id]: q } }), { items: [], cart: {} });

	it('nudges club and event requests with a few each of many designs toward packs of 50', () => {
		const { items, cart } = designs(14, 10);
		const n = stickerNudge(cart, items, 'event');
		expect(n).toMatchObject({ designs: 14, loose: 14 });
		expect(n?.text).toMatch(/^You picked 14 sticker designs, 14 of them a few each\. /);
		expect(n?.text).toContain('packs of 50');
		expect(stickerNudge(cart, items, 'club')).not.toBeNull();
	});

	it('leaves alone a few designs, whole packs, and personal requests', () => {
		expect(stickerNudge(designs(2, 10).cart, designs(2, 10).items, 'event')).toBeNull();
		expect(stickerNudge(designs(6, 50).cart, designs(6, 50).items, 'event')).toBeNull();
		expect(stickerNudge(designs(14, 10).cart, designs(14, 10).items, 'personal')).toBeNull();
		// Only stickers count: postcards in fives are fine.
		expect(stickerNudge({ [postcard.id]: 10, [sticker.id]: 10 }, [postcard, sticker], 'event')).toBeNull();
	});

	it('counts only loose designs: whole packs alongside a couple of loose ones are fine', () => {
		const { items, cart } = designs(5, 50);
		cart[items[0].id] = 10;
		cart[items[1].id] = 3;
		expect(stickerNudge(cart, items, 'event')).toBeNull();
		cart[items[2].id] = 10;
		expect(stickerNudge(cart, items, 'event')).toMatchObject({ designs: 5, loose: 3 });
		delete cart[items[4].id];
		expect(stickerNudge(cart, items, 'event')?.text).toBe(
			"You picked 4 sticker designs, 3 of them a few each. We'd rather send you fewer designs in packs of 50 because they're bagged in 50s at the warehouse. " +
				'This makes it easier for us to ship (and for you to hand out). You can still request it like this, but we may send you higher quantities of fewer designs when we ship.'
		);
	});
});
