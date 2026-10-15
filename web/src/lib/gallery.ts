import { itemPath } from './items';
import type { Item } from './types';

export type Slide = { src: string; href: string; alt: string; real: boolean; sticker: boolean };

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
