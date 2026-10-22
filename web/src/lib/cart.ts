import type { Item, Line, Purpose } from './types';

/** item id -> quantity */
export type Cart = Record<number, number>;

/** item id -> size SKU -> quantity, for items that come in sizes. The cart holds each one's total. */
export type Sizes = Record<number, Record<string, number>>;

export const emptyCart = (): Cart => ({});

/** Small pack sizes per kind of pack; past the last, multiples of 50 ("fives" is every multiple of 5). Mirrors swag.Packs. */
export const PACKS: Record<string, number[]> = { fives: [], postcard: [5, 10, 15, 20, 25], sticker: [3, 10] };

/** Quantities a pack kind comes in, up to max: multiples of 5 for "fives", or its small sizes then multiples of 50. */
export function packQuantities(packs: string, max: number): number[] {
	const step = packs === 'fives' ? 5 : 50;
	const out = (PACKS[packs] ?? []).filter((q) => q <= max);
	for (let q = step; q <= max; q += step) out.push(q);
	return out;
}

/** The quantities to offer for an item (0 = none), or undefined when any count up to the max is fine. */
export const quantityOptions = (item: Item): number[] | undefined =>
	item.packs ? [0, ...packQuantities(item.packs, item.max_per_request)] : undefined;

/** The quick-pick amounts shown after "Add": the item's own list when it has one, set amounts for postcards, the first few pack sizes for other packs, or round counts up to the max. */
export function presetQuantities(item: Item): number[] {
	// Set on the admin Items tab (e.g. 1, 3, 5, 10 for the full-size magazine).
	if (item.quick_picks.length) return item.quick_picks.filter((q) => q >= item.min_per_request && q <= item.max_per_request);
	// Postcards: the amounts people actually ask for (all of them valid pack sizes).
	if (item.packs === 'postcard') return [5, 10, 25, 50, 100].filter((q) => q <= item.max_per_request);
	if (item.packs) return packQuantities(item.packs, item.max_per_request).slice(0, 6);
	const min = item.min_per_request;
	const round = [1, 2, 3, 5, 10, 15, 20, 25, 50, 100].filter((q) => q > min && q <= item.max_per_request);
	return [min, ...round].filter((q) => q <= item.max_per_request).slice(0, 6);
}

/** Snaps a pack quantity to the largest allowed size not above it; below the smallest pack rounds up to it. */
function snapPack(item: Item, q: number): number {
	if (q <= 0) return 0;
	const sizes = packQuantities(item.packs, item.max_per_request);
	return sizes.filter((s) => s <= Math.max(q, sizes[0] ?? 0)).at(-1) ?? 0;
}

export function setQuantity(cart: Cart, item: Item, qty: number): Cart {
	const next = { ...cart };
	let n = Math.max(Math.floor(qty) || 0, 0);
	// Below the item's minimum: adding goes up to it, stepping down from it takes the item out.
	if (n > 0 && n < item.min_per_request) n = (cart[item.id] ?? 0) > n ? 0 : item.min_per_request;
	const q = item.packs ? snapPack(item, n) : Math.min(n, item.max_per_request);
	if (q === 0) delete next[item.id];
	else next[item.id] = q;
	return next;
}

export const totalQuantity = (cart: Cart): number => Object.values(cart).reduce((a, b) => a + b, 0);

/** Sets how many of one size of a sized item to send. The item's per-request max counts every size together. */
export function setSizeQuantity(cart: Cart, sizes: Sizes, item: Item, sku: string, qty: number): { cart: Cart; sizes: Sizes } {
	const mine = { ...sizes[item.id] };
	delete mine[sku];
	const others = Object.values(mine).reduce((a, b) => a + b, 0);
	const q = Math.min(Math.max(Math.floor(qty) || 0, 0), Math.max(item.max_per_request - others, 0));
	if (q > 0) mine[sku] = q;
	const nextSizes = { ...sizes, [item.id]: mine };
	const nextCart = { ...cart, [item.id]: others + q };
	if (!Object.keys(mine).length) delete nextSizes[item.id];
	if (!nextCart[item.id]) delete nextCart[item.id];
	return { cart: nextCart, sizes: nextSizes };
}

/** A sized item's sizes in the bag, in the item's size order. */
const picked = (item: Item, sizes: Sizes) => item.variants.filter((v) => sizes[item.id]?.[v.sku] > 0).map((v) => ({ ...v, quantity: sizes[item.id][v.sku] }));

/** "1 S, 2 M" for what's in the bag of a sized item; "" if none. */
export const sizesSummary = (item: Item, sizes: Sizes): string => picked(item, sizes).map((v) => `${v.quantity} ${v.label}`).join(', ');

export function cartLines(cart: Cart, items: Item[], sizes: Sizes = {}): Line[] {
	return items
		.filter((i) => cart[i.id])
		.flatMap((i) => (i.variants.length ? picked(i, sizes).map((v) => ({ item_id: i.id, quantity: v.quantity, sku: v.sku })) : [{ item_id: i.id, quantity: cart[i.id] }]));
}

const offers = (item: Item, sku: string | undefined) => item.variants.some((v) => v.sku === sku);

/** The size label ("M") of a sized item's SKU, or "" if none is picked. */
export const sizeLabel = (item: Item, sku: string | undefined): string => item.variants.find((v) => v.sku === sku)?.label ?? '';

/** Client-side mirror of the server's checks, for instant feedback. The server is authoritative. */
export function cartErrors(cart: Cart, items: Item[] = [], sizes: Sizes = {}): string[] {
	if (totalQuantity(cart) === 0) return ['Pick at least one item.'];
	return items.filter((i) => cart[i.id] && i.variants.length && !picked(i, sizes).length).map((i) => `Pick a size for ${i.name}.`);
}

/** Reads a cart saved to localStorage; anything malformed is dropped. */
export function parseCart(raw: string | null): Cart {
	let data: unknown;
	try {
		data = JSON.parse(raw ?? '{}');
	} catch {
		return {};
	}
	if (!data || typeof data !== 'object' || Array.isArray(data)) return {};
	const cart: Cart = {};
	for (const [k, v] of Object.entries(data)) {
		const id = Number(k);
		if (Number.isInteger(id) && id > 0 && Number.isInteger(v) && (v as number) > 0) cart[id] = v as number;
	}
	return cart;
}

/** Drops items that are no longer listed and re-clamps quantities to current limits. A sized item's quantity is the sum of its sizes. */
export function pruneCart(cart: Cart, items: Item[], sizes: Sizes = {}): Cart {
	let next = emptyCart();
	for (const item of items) {
		const q = item.variants.length ? picked(item, sizes).reduce((a, v) => a + v.quantity, 0) : cart[item.id];
		if (q) next = setQuantity(next, item, q);
	}
	return next;
}

/** Reads sizes saved to localStorage; anything malformed is dropped. */
export function parseSizes(raw: string | null): Sizes {
	let data: unknown;
	try {
		data = JSON.parse(raw ?? '{}');
	} catch {
		return {};
	}
	if (!data || typeof data !== 'object' || Array.isArray(data)) return {};
	const sizes: Sizes = {};
	for (const [k, v] of Object.entries(data)) {
		const id = Number(k);
		if (!Number.isInteger(id) || id <= 0) continue;
		// Before several sizes, this held the one picked SKU.
		if (typeof v === 'string') sizes[id] = { [v]: 1 };
		else if (v && typeof v === 'object' && !Array.isArray(v)) {
			const counts = Object.fromEntries(Object.entries(v).filter(([, n]) => Number.isInteger(n) && (n as number) > 0)) as Record<string, number>;
			if (Object.keys(counts).length) sizes[id] = counts;
		}
	}
	return sizes;
}

/** Keeps only sizes the listed items still offer. */
export function pruneSizes(sizes: Sizes, items: Item[]): Sizes {
	const next: Sizes = {};
	for (const item of items) {
		const kept = Object.fromEntries(Object.entries(sizes[item.id] ?? {}).filter(([sku, n]) => offers(item, sku) && n > 0));
		if (Object.keys(kept).length) next[item.id] = kept;
	}
	return next;
}

/** Amounts typed on the admin Items tab ("1, 3, 5, 10"): positive whole numbers, ascending, no repeats. */
export const parseQuickPicks = (text: string): number[] =>
	[...new Set(text.split(/[\s,]+/).map(Number).filter((n) => Number.isInteger(n) && n > 0))].sort((a, b) => a - b);

/** Whether q is a pack size for the kind of pack. Mirrors swag.PackQuantity. */
function packQuantity(packs: string, q: number): boolean {
	if (packs === 'fives') return q > 0 && q % 5 === 0;
	return (PACKS[packs] ?? []).includes(q) || (q > 0 && q % 50 === 0);
}

const PACK_RULES: Record<string, string> = {
	fives: 'These come in multiples of 5.',
	postcard: 'Postcards come in 5, 10, 15, 20 or 25, then multiples of 50.',
	sticker: 'Stickers come in 3 or 10, then packs of 50.'
};

/** What's wrong with a typed amount of an item, or '' if it can be requested (the server checks again). */
export function quantityProblem(item: Item, q: number): string {
	if (q > item.max_per_request) return `At most ${item.max_per_request} per request.`;
	if (q < item.min_per_request) return `At least ${item.min_per_request} per request.`;
	if (item.packs && !packQuantity(item.packs, q)) return PACK_RULES[item.packs] ?? 'That amount isn’t a pack size.';
	return '';
}

/** Loose sticker designs (fewer than a pack of 50) a club or event request can have before we suggest packs. */
export const LOOSE_STICKER_DESIGNS = 3;

/**
 * A nudge for club and event requests that ask for a few each of many sticker designs: we'd rather
 * send fewer designs in packs of 50, which the warehouse keeps bagged. It never blocks a request.
 * Reviewers regrouped these by hand (event requests asked for a median 13.5 designs; 4.5 shipped).
 */
export function stickerNudge(cart: Cart, items: Item[], purpose: Purpose | ''): { designs: number; loose: number; text: string } | null {
	if (purpose !== 'club' && purpose !== 'event') return null;
	const picked = items.filter((i) => i.packs === 'sticker' && (cart[i.id] ?? 0) > 0);
	const loose = picked.filter((i) => cart[i.id] < 50).length;
	if (loose < LOOSE_STICKER_DESIGNS) return null;
	return {
		designs: picked.length,
		loose,
		text:
			`You picked ${picked.length} sticker designs, ${loose === picked.length ? 'a few of each' : `${loose} of them a few each`}. ` +
			"We'd rather send you fewer designs in packs of 50: they're bagged in 50s at the warehouse, so whole packs are much easier for our team to ship (and for you to hand out). " +
			'You can still request it like this; we may regroup it before it ships.'
	};
}
