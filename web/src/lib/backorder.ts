// Backorders: the warehouse is short of an item but has more on order (its
// stock goes below zero once requests wait on it). Mirrors swag.Backordered.
// Only items an admin lets be backordered (allow_backorder) wait for a restock;
// requests for anything else stop at what's left (swag.Validate's stock check).
// Stock here is what's left once open requests ship.

import { cartLines, type Cart, type Sizes } from './cart';
import type { Item, SkuStock } from './types';

const short = (st: SkuStock | undefined, quantity: number) => !!st && st.in_stock < quantity && st.inbound > 0;

// 15000 -> "15k", 1500 -> "1.5k", 500 -> "500".
export const countText = (n: number) => new Intl.NumberFormat('en-US', { notation: 'compact' }).format(n).toLowerCase();

// The item list's note for a backordered item: of what's picked, or of the item
// itself before it's picked. '' when it's in stock or nothing is coming.
export function backorderNote(item: Item, stock: Record<string, SkuStock> | undefined, cart: Cart, sizes: Sizes): string {
	if (!stock || !item.allow_backorder) return '';
	const lines = cart[item.id] ? cartLines(cart, [item], sizes) : [{ item_id: item.id, quantity: 1, sku: undefined }];
	for (const l of lines) {
		const st = stock[l.sku ?? item.sku];
		if (!short(st, l.quantity)) continue;
		const size = item.variants.find((v) => v.sku === l.sku)?.label;
		return `${countText(st.inbound)}${size ? ` size ${size}` : ''} are inbound to Hack Club HQ. Your package will ship on arrival.`;
	}
	return '';
}

// A SKU the warehouse doesn't list is out of stock (it omits what it's out of).
const left = (stock: Record<string, SkuStock>, sku: string) => Math.max(stock[sku]?.in_stock ?? 0, 0);

// The item list's note for an item that can't be backordered and the warehouse
// can't cover: of what's picked, or of the item itself before it's picked
// (only when none is left). '' otherwise, and when the warehouse is down.
export function shortNote(item: Item, stock: Record<string, SkuStock> | undefined, cart: Cart, sizes: Sizes): string {
	if (!stock || item.allow_backorder) return '';
	if (!cart[item.id]) return soldOut(item, stock) ? 'Out of stock.' : '';
	for (const l of cartLines(cart, [item], sizes)) {
		const n = left(stock, l.sku ?? item.sku);
		if (n >= l.quantity) continue;
		const size = item.variants.find((v) => v.sku === l.sku)?.label;
		if (n === 0) return size ? `Size ${size} is out of stock.` : 'Out of stock.';
		return size ? `Only ${n} left in size ${size}.` : `Only ${n} left.`;
	}
	return '';
}

// Whether none of an item (in any size) can be requested.
export function soldOut(item: Item, stock: Record<string, SkuStock> | undefined): boolean {
	if (!stock || item.allow_backorder) return false;
	const skus = item.variants.length ? item.variants.map((v) => v.sku) : [item.sku];
	return skus.every((sku) => left(stock, sku) === 0);
}

// What keeps the request from going through for lack of stock, one line per item.
export function stockErrors(cart: Cart, items: Item[], sizes: Sizes, stock: Record<string, SkuStock> | undefined): string[] {
	return items
		.filter((i) => cart[i.id])
		.map((i) => [i.name, shortNote(i, stock, cart, sizes)])
		.filter(([, note]) => note)
		.map(([name, note]) => `${name}: ${note[0].toLowerCase()}${note.slice(1)}`);
}
