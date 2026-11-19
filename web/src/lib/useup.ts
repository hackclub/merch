// Using up older stock first: an item can name an older one (use_up_first; the
// Heidi T-shirt names the Hack Club T-shirt), and each of its sizes opens only
// once the older item is out of that size. Mirrors swag.UseUpFirstBlocks; the
// server is authoritative. Stock is what's left once open requests ship.

import { cartLines, type Cart, type Sizes } from './cart';
import { offered } from './purpose';
import type { Item, Purpose, SkuStock } from './types';

type Stock = Record<string, SkuStock> | undefined;

const known = (stock: Stock): stock is Record<string, SkuStock> => !!stock && Object.keys(stock).length > 0;

// The older item, if it's listed and offered for the purpose.
function olderItem(item: Item, items: Item[], purpose: Purpose | ''): Item | undefined {
	if (item.use_up_first == null) return undefined;
	const old = items.find((i) => i.id === item.use_up_first);
	return old && old.visible && old.id !== item.id && offered(old, purpose) ? old : undefined;
}

/** The older item to get instead of this size (sku; undefined for items without sizes), or null when it's open. */
export function useUpBlock(item: Item, items: Item[], stock: Stock, sku: string | undefined, purpose: Purpose | ''): { old: Item; label: string } | null {
	if (!known(stock)) return null;
	const old = olderItem(item, items, purpose);
	if (!old) return null;
	let oldSKU = old.sku;
	let label = '';
	if (item.variants.length) {
		const v = item.variants.find((x) => x.sku === sku);
		if (!v) return null;
		label = v.label;
		const ov = old.variants.find((x) => x.label.toLowerCase() === v.label.toLowerCase());
		if (!ov) return null; // the older item never came in this size
		oldSKU = ov.sku;
	}
	return (stock[oldSKU]?.in_stock ?? 0) > 0 ? { old, label } : null;
}

/** The SKUs of the item's sizes that wait on the older item, in size order. */
export const lockedSizes = (item: Item, items: Item[], stock: Stock, purpose: Purpose | ''): Set<string> =>
	new Set(item.variants.filter((v) => useUpBlock(item, items, stock, v.sku, purpose)).map((v) => v.sku));

// "S", "S and M", "S, M and L" / with "or".
const list = (xs: string[], word: string) => (xs.length < 2 ? xs.join('') : `${xs.slice(0, -1).join(', ')} ${word} ${xs[xs.length - 1]}`);

/** The note on the newer item while some of it waits; '' once nothing does. */
export function useUpNote(item: Item, items: Item[], stock: Stock, purpose: Purpose | ''): string {
	if (!item.variants.length) {
		const b = useUpBlock(item, items, stock, undefined, purpose);
		return b ? `We're giving out the ${b.old.name} first. Get that until it runs out.` : '';
	}
	const locked = lockedSizes(item, items, stock, purpose);
	if (!locked.size) return '';
	const old = olderItem(item, items, purpose)!;
	const open = item.variants.filter((v) => !locked.has(v.sku)).map((v) => v.label);
	if (!open.length) return `We're giving out the ${old.name} first. Each size of this one opens once the ${old.name} runs out of it.`;
	const waiting = item.variants.filter((v) => locked.has(v.sku)).map((v) => v.label);
	return `We're giving out the ${old.name} first, so this one comes only in ${list(open, 'and')} for now. For ${list(waiting, 'or')}, get the ${old.name}.`;
}

/** The older item's note pointing its sold-out sizes at the newer one ("Out of M? Get the Heidi T-Shirt."). */
export function newerNote(item: Item, items: Item[], stock: Stock, purpose: Purpose | ''): string {
	if (!known(stock) || !item.visible || !offered(item, purpose)) return '';
	for (const n of items) {
		if (n.use_up_first !== item.id || n.id === item.id || !n.visible || !offered(n, purpose)) continue;
		const out = item.variants
			.filter((v) => (stock[v.sku]?.in_stock ?? 0) <= 0 && n.variants.some((x) => x.label.toLowerCase() === v.label.toLowerCase()))
			.map((v) => v.label);
		if (out.length) return `Out of ${list(out, 'and')}? Get the ${n.name}.`;
		if (!item.variants.length && !n.variants.length && (stock[item.sku]?.in_stock ?? 0) <= 0) return `Out of stock? Get the ${n.name}.`;
	}
	return '';
}

/** What keeps the request from going through because older stock comes first, one line per picked size. */
export function useUpErrors(cart: Cart, items: Item[], sizes: Sizes, stock: Stock, purpose: Purpose | ''): string[] {
	const out: string[] = [];
	for (const l of cartLines(cart, items, sizes)) {
		const item = items.find((i) => i.id === l.item_id)!;
		const b = useUpBlock(item, items, stock, l.sku, purpose);
		if (!b) continue;
		out.push(b.label ? `${item.name}: size ${b.label} is still available as the ${b.old.name}; get that instead.` : `${item.name}: the ${b.old.name} is still available; get that instead.`);
	}
	return out;
}
