import { itemPath } from './items';
import type { Item } from './types';

export type Slide = { src: string; href: string; alt: string; real: boolean; sticker: boolean; page?: boolean };

/** The homepage strip: every real-world photo, with one product image of each item mixed in
 * after each photo, so it reads as swag out in the world rather than a catalog. Each slide links to its item. */
export function gallery(items: Item[]): Slide[] {
	const photos: Slide[] = items.flatMap((i) =>
		i.photos.map((p) => ({ src: p.url, href: itemPath(i), alt: p.caption || i.name, real: true, sticker: false }))
	);
	const products: Slide[] = items
		.filter((i) => i.images.length)
		.map((i) => ({ src: i.images[0], href: itemPath(i), alt: i.name, real: false, sticker: i.packs === 'sticker' }));
	const out: Slide[] = [];
	for (let n = 0; n < Math.max(photos.length, products.length); n++) {
		if (photos[n]) out.push(photos[n]);
		if (products[n]) out.push(products[n]);
	}
	return out;
}

/** "~$15 USD" for 1500; the hero rounds the international fee to whole dollars. */
export const roughFee = (cents: number): string => `~$${Math.round(cents / 100)} USD`;

/** Every other slide from a and b, a first, until both run out. */
function interleave<T>(a: T[], b: T[]): T[] {
	const out: T[] = [];
	for (let n = 0; n < Math.max(a.length, b.length); n++) {
		if (a[n]) out.push(a[n]);
		if (b[n]) out.push(b[n]);
	}
	return out;
}

/** Repeats a row until it has at least min slides, so a short one still fills a scrolling strip. */
function fill(row: Slide[], min: number): Slide[] {
	if (!row.length) return row;
	const out = [...row];
	while (out.length < min) out.push(...row);
	return out;
}

/**
 * The homepage's two scrolling strips: real-world photos alternating with each
 * item's main image on top; magazines' inside pages alternating with items'
 * other images (backs, details) below. With nothing for the second strip, the
 * first is split between the two.
 */
export function galleryRows(items: Item[], min = 10): [Slide[], Slide[]] {
	const photos: Slide[] = items.flatMap((i) =>
		i.photos.map((p) => ({ src: p.url, href: itemPath(i), alt: p.caption || i.name, real: true, sticker: false }))
	);
	const image = (i: Item, src: string): Slide => ({ src, href: itemPath(i), alt: i.name, real: false, sticker: i.packs === 'sticker' });
	const mains = items.filter((i) => i.images.length).map((i) => image(i, i.images[0]));
	const others = items.flatMap((i) => i.images.slice(1).map((src) => image(i, src)));
	// Inside pages only: the front cover is already the item's main image.
	const pages: Slide[] = items.flatMap((i) =>
		i.pages.slice(1, 9).map((src, n) => ({ src, href: itemPath(i), alt: `${i.name}, page ${n + 2}`, real: false, sticker: false, page: true }))
	);
	let top = interleave(photos, mains);
	let bottom = interleave(pages, others);
	if (!bottom.length) [top, bottom] = [top.filter((_, n) => n % 2 === 0), top.filter((_, n) => n % 2 === 1)];
	return [fill(top, min), fill(bottom, min)];
}
