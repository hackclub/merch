import type { Item, ItemStock, Line } from './types';

/** item id -> quantity */
export type Cart = Record<number, number>;

/** item id -> chosen size SKU, for items that come in sizes */
export type Sizes = Record<number, string>;

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

/** Snaps a pack quantity to the largest allowed size not above it; below the smallest pack rounds up to it. */
function snapPack(item: Item, q: number): number {
	if (q <= 0) return 0;
	const sizes = packQuantities(item.packs, item.max_per_request);
	return sizes.filter((s) => s <= Math.max(q, sizes[0] ?? 0)).at(-1) ?? 0;
}

export function setQuantity(cart: Cart, item: Item, qty: number): Cart {
	const next = { ...cart };
	const n = Math.max(Math.floor(qty) || 0, 0);
	const q = item.packs ? snapPack(item, n) : Math.min(n, item.max_per_request);
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

export type CostLine = { item: Item; quantity: number; unitCost: string | null; total: number | null };

/** What the cart costs at warehouse unit costs (dollars). A sized item is priced at its picked
 * size; a line is unpriced (null) until its cost is known, and so is the subtotal. */
export function cartCosts(cart: Cart, items: Item[], sizes: Sizes, stocks: Record<number, ItemStock>) {
	const lines: CostLine[] = items
		.filter((i) => cart[i.id])
		.map((item) => {
			const stock = stocks[item.id];
			const unitCost = (item.variants.length ? stock?.variants.find((v) => v.sku === sizes[item.id])?.unit_cost : stock?.unit_cost) ?? null;
			const n = Number(unitCost);
			return { item, quantity: cart[item.id], unitCost, total: unitCost && Number.isFinite(n) ? n * cart[item.id] : null };
		});
	const subtotal = lines.every((l) => l.total !== null) ? lines.reduce((a, l) => a + (l.total ?? 0), 0) : null;
	return { lines, subtotal };
}

/** Client-side mirror of the server's checks, for instant feedback. The server is authoritative. */
export function cartErrors(cart: Cart, maxItemsPerRequest: number, items: Item[] = [], sizes: Sizes = {}): string[] {
	const total = totalQuantity(cart);
	if (total === 0) return ['Pick at least one item.'];
	// A pack counts as one item toward the per-request total, as on the server.
	const packed = items.filter((i) => i.packs && cart[i.id]).reduce((n, i) => n + cart[i.id] - 1, 0);
	if (maxItemsPerRequest > 0 && total - packed > maxItemsPerRequest)
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
