import type { Item, Line } from './types';

/** item id -> quantity */
export type Cart = Record<number, number>;

/** item id -> chosen size SKU, for items that come in sizes */
export type Sizes = Record<number, string>;

export const emptyCart = (): Cart => ({});

export function setQuantity(cart: Cart, item: Item, qty: number): Cart {
	const next = { ...cart };
	const q = Math.min(Math.max(Math.floor(qty) || 0, 0), item.max_per_request);
	if (q === 0) delete next[item.id];
	else next[item.id] = q;
	return next;
}

export const totalQuantity = (cart: Cart): number => Object.values(cart).reduce((a, b) => a + b, 0);

export function cartLines(cart: Cart, items: Item[], sizes: Sizes = {}): Line[] {
	return items
		.filter((i) => cart[i.id])
		.map((i) => (i.variants.length ? { item_id: i.id, quantity: cart[i.id], sku: sizes[i.id] } : { item_id: i.id, quantity: cart[i.id] }));
}

const offers = (item: Item, sku: string | undefined) => item.variants.some((v) => v.sku === sku);

/** The size label ("M") of a sized item's SKU, or "" if none is picked. */
export const sizeLabel = (item: Item, sku: string | undefined): string => item.variants.find((v) => v.sku === sku)?.label ?? '';

/** Client-side mirror of the server's checks, for instant feedback. The server is authoritative. */
export function cartErrors(cart: Cart, maxItemsPerRequest: number, items: Item[] = [], sizes: Sizes = {}): string[] {
	const total = totalQuantity(cart);
	if (total === 0) return ['Pick at least one item.'];
	if (maxItemsPerRequest > 0 && total > maxItemsPerRequest)
		return [`You can request at most ${maxItemsPerRequest} items at a time.`];
	return items.filter((i) => cart[i.id] && i.variants.length && !offers(i, sizes[i.id])).map((i) => `Pick a size for ${i.name}.`);
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

/** Drops items that are no longer listed and re-clamps quantities to current limits. */
export function pruneCart(cart: Cart, items: Item[]): Cart {
	let next = emptyCart();
	for (const item of items) if (cart[item.id]) next = setQuantity(next, item, cart[item.id]);
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
		if (Number.isInteger(id) && id > 0 && typeof v === 'string') sizes[id] = v;
	}
	return sizes;
}

/** Keeps only sizes the listed items still offer. */
export function pruneSizes(sizes: Sizes, items: Item[]): Sizes {
	const next: Sizes = {};
	for (const item of items) if (offers(item, sizes[item.id])) next[item.id] = sizes[item.id];
	return next;
}
