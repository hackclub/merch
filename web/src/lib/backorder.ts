// Backorders: the warehouse is short of an item but has more on order (its
// stock goes below zero once requests wait on it). Mirrors swag.Backordered.

import { cartLines, type Cart, type Sizes } from './cart';
import type { Item, SkuStock } from './types';

const short = (st: SkuStock | undefined, quantity: number) => !!st && st.in_stock < quantity && st.inbound > 0;

// 15000 -> "15k", 1500 -> "1.5k", 500 -> "500".
export const countText = (n: number) => new Intl.NumberFormat('en-US', { notation: 'compact' }).format(n).toLowerCase();

// The item list's note for a backordered item: of what's picked, or of the item
// itself before it's picked. '' when it's in stock or nothing is coming.
export function backorderNote(item: Item, stock: Record<string, SkuStock> | undefined, cart: Cart, sizes: Sizes): string {
	if (!stock) return '';
	const lines = cart[item.id] ? cartLines(cart, [item], sizes) : [{ item_id: item.id, quantity: 1, sku: undefined }];
	for (const l of lines) {
		const st = stock[l.sku ?? item.sku];
		if (!short(st, l.quantity)) continue;
		const size = item.variants.find((v) => v.sku === l.sku)?.label;
		return `${countText(st.inbound)}${size ? ` size ${size}` : ''} are inbound to Hack Club HQ. Your package will ship when they arrive.`;
	}
	return '';
}
