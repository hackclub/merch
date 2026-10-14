import type { Item, Line } from './types';

/** item id -> quantity */
export type Cart = Record<number, number>;

export const emptyCart = (): Cart => ({});

export function setQuantity(cart: Cart, item: Item, qty: number): Cart {
	const next = { ...cart };
	const q = Math.min(Math.max(Math.floor(qty) || 0, 0), item.max_per_request);
	if (q === 0) delete next[item.id];
	else next[item.id] = q;
	return next;
}

export const totalQuantity = (cart: Cart): number => Object.values(cart).reduce((a, b) => a + b, 0);

export function cartLines(cart: Cart, items: Item[]): Line[] {
	return items.filter((i) => cart[i.id]).map((i) => ({ item_id: i.id, quantity: cart[i.id] }));
}

/** Client-side mirror of the server's checks, for instant feedback. The server is authoritative. */
export function cartErrors(cart: Cart, maxItemsPerRequest: number): string[] {
	const total = totalQuantity(cart);
	if (total === 0) return ['Pick at least one item.'];
	if (maxItemsPerRequest > 0 && total > maxItemsPerRequest)
		return [`You can request at most ${maxItemsPerRequest} items at a time.`];
	return [];
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
