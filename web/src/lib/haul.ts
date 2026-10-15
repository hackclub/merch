// "Everything you're about to get": each line of the request as a pile at true
// size (inches), as tall as its count makes it, laid out on a table next to a
// phone for scale. haul3d.ts draws it; this is the geometry, kept pure for tests.
import { isBooklet, thumbnail } from './items';
import { sheetThickness } from './paper';
import { PHONE } from './scale';
import { STICKER_THICKNESS_IN } from './stickers';
import type { Item } from './types';

export type HaulLine = { item: Item; quantity: number; size?: string };

export type Pile = {
	key: string;
	label: string; // "3 × Poster"
	art: string; // image on top of the pile ('' = plain)
	w: number; // footprint, left to right
	d: number; // footprint, front to back
	height: number; // the whole stack
};

// A folded T-shirt, roughly.
const SHIRT = { w: 9.5, d: 12, t: 1.2 };
// A die-cut sticker with no measurements: about 3 in across.
const STICKER = { w: 3, d: 3 };
// Something we have no measurements for: a small box.
const UNKNOWN = { w: 6, d: 6, t: 1 };
// Booklets without a measured thickness: about 0.1 mm a page.
const PAGE_IN = 0.004;

export function piles(lines: HaulLine[]): Pile[] {
	return lines
		.filter((l) => l.quantity > 0)
		.map(({ item, quantity, size }) => {
			const label = `${quantity} × ${item.name}${size ? ` (${size})` : ''}`;
			const key = `${item.id}:${size ?? ''}`;
			const art = thumbnail(item);
			if (item.width_in != null && item.height_in != null) {
				const unit = isBooklet(item)
					? (item.thickness_in ?? item.pages.length * PAGE_IN)
					: item.packs === 'sticker'
						? STICKER_THICKNESS_IN
						: sheetThickness(item);
				return { key, label, art, w: item.width_in, d: item.height_in, height: unit * quantity };
			}
			if (item.packs === 'sticker') return { key, label, art, w: STICKER.w, d: STICKER.d, height: STICKER_THICKNESS_IN * quantity };
			const box = item.variants.length ? SHIRT : UNKNOWN;
			return { key, label, art, w: box.w, d: box.d, height: box.t * quantity };
		});
}

export type Placed = Pile & { x: number; z: number }; // centre of the footprint on the table

/** Space between piles, in inches. */
const GAP = 1.5;

/**
 * Rows of piles, biggest footprints first, with the phone lying flat right beside
 * the biggest so the two are easy to compare; x runs left to right and z back to
 * front, centred on 0.
 */
export function layoutHaul(all: Pile[]): { piles: Placed[]; phone: { x: number; z: number }; width: number; depth: number } {
	const phone: Pile = { key: 'phone', label: 'phone', art: '', w: PHONE.w, d: PHONE.h, height: 0 };
	const items = [...all].sort((a, b) => b.d - a.d);
	const area = [...items, phone].reduce((a, p) => a + (p.w + GAP) * (p.d + GAP), 0);
	const rowMax = Math.max(24, Math.sqrt(area) * 1.6);

	const placed: Placed[] = [];
	let x = 0;
	let z = 0;
	let rowDepth = 0;
	const order = [...items.slice(0, 1), phone, ...items.slice(1)];
	for (const p of order) {
		if (x > 0 && x + p.w > rowMax) {
			z += rowDepth + GAP;
			x = 0;
			rowDepth = 0;
		}
		placed.push({ ...p, x: x + p.w / 2, z: z + p.d / 2 });
		x += p.w + GAP;
		rowDepth = Math.max(rowDepth, p.d);
	}
	const width = Math.max(...placed.map((p) => p.x + p.w / 2));
	const depth = z + rowDepth;
	for (const p of placed) {
		p.x -= width / 2;
		p.z -= depth / 2;
	}
	const ph = placed.find((p) => p.key === 'phone')!;
	return { piles: placed.filter((p) => p !== ph), phone: { x: ph.x, z: ph.z }, width, depth };
}
