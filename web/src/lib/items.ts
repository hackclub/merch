import type { Item, ItemStock } from './types';

const slugify = (s: string) =>
	s
		.toLowerCase()
		.replace(/[^a-z0-9]+/g, '-')
		.replace(/^-|-$/g, '');

/** Product page URL: /items/<id>-<slug>. Only the id matters; the slug is for humans. */
export function itemPath(item: Item): string {
	const slug = slugify(item.name);
	return `/items/${item.id}${slug ? '-' + slug : ''}`;
}

export function itemIdFromParam(param: string): number | null {
	const id = Number(/^(\d+)(?:-|$)/.exec(param)?.[1]);
	return id > 0 ? id : null;
}

export const thumbnail = (item: Item): string => item.images[0] ?? '';

/** Returns a copy of images with images[i] moved one place left (-1) or right (+1). */
export function moveImage(images: string[], i: number, delta: -1 | 1): string[] {
	const j = i + delta;
	if (j < 0 || j >= images.length) return [...images];
	const next = [...images];
	[next[i], next[j]] = [next[j], next[i]];
	return next;
}

/** Warehouse unit cost ("0.0832" dollars) as "$0.0832"; at least cents, never rounded away. */
export function formatUnitCost(cost: string | null): string {
	const n = Number(cost);
	if (!cost || !Number.isFinite(n)) return '';
	const decimals = Math.max(2, cost.split('.')[1]?.length ?? 0);
	return new Intl.NumberFormat('en-US', {
		style: 'currency', currency: 'USD', minimumFractionDigits: decimals, maximumFractionDigits: decimals
	}).format(n);
}

/** "$0.37 each", or "$13.82–$14.27 each" when an item's sizes cost different amounts. */
export function unitCostLabel(stock: ItemStock): string {
	const lo = formatUnitCost(stock.unit_cost);
	if (!lo) return '';
	const hi = formatUnitCost(stock.unit_cost_max);
	return `${hi ? `${lo}–${hi}` : lo} each`;
}
