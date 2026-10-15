// "Everything you're about to get": each line of the request as a pile at true
// size (inches), as tall as its count makes it, laid out on a table next to a
// phone for scale. haul3d.ts draws it; this is the geometry, kept pure for tests.
import { isBooklet, thumbnail } from './items';
import { sheetThickness } from './paper';
import { CARD_IN, PER_BUNDLE } from './postcards';
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
	band?: boolean; // a printer's bundle, with a paper band around it
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
		.flatMap((line): Pile[] => {
			const { item, quantity } = line;
			// Postcards ship in banded bundles of 50: one pile each, then the rest loose.
			if (item.packs === 'postcard' && quantity >= PER_BUNDLE) {
				const one = pileOf({ ...line, quantity: PER_BUNDLE });
				const bundles = Array.from({ length: Math.floor(quantity / PER_BUNDLE) }, (_, n) => ({ ...one, key: `${one.key}:${n}`, band: true }));
				const rest = quantity % PER_BUNDLE;
				return rest ? [...bundles, { ...pileOf({ ...line, quantity: rest }), key: `${one.key}:rest` }] : bundles;
			}
			return [pileOf(line)];
		});
}

/** "3 × Poster (M)" for a line of the request. */
export const lineLabel = ({ item, quantity, size }: HaulLine) => `${quantity} × ${item.name}${size ? ` (${size})` : ''}`;

function pileOf({ item, quantity, size }: HaulLine): Pile {
	const label = lineLabel({ item, quantity, size });
	const key = `${item.id}:${size ?? ''}`;
	const art = thumbnail(item);
	if (item.width_in != null && item.height_in != null) {
		const unit = isBooklet(item)
			? (item.thickness_in ?? item.pages.length * PAGE_IN)
			: item.packs === 'sticker'
				? STICKER_THICKNESS_IN
				: item.packs === 'postcard'
					? (item.thickness_in ?? CARD_IN)
					: sheetThickness(item);
		return { key, label, art, w: item.width_in, d: item.height_in, height: unit * quantity };
	}
	if (item.packs === 'sticker') return { key, label, art, w: STICKER.w, d: STICKER.d, height: STICKER_THICKNESS_IN * quantity };
	const box = item.variants.length ? SHIRT : UNKNOWN;
	return { key, label, art, w: box.w, d: box.d, height: box.t * quantity };
}

export type Placed = Pile & { x: number; z: number }; // centre of the footprint on the table
export type Table = { x: number; z: number; w: number; d: number }; // centre and size of a tabletop

/** A standard 6-foot folding table, the kind at every club fair. */
export const TABLE = { w: 72, d: 30 };
/** Space between piles, between the piles and the table's edge, and between tables, in inches. */
const GAP = 0.75;
const EDGE = 3;
const BETWEEN = 12;

export type HaulLayout = { piles: Placed[]; phone: { x: number; z: number }; tables: Table[]; width: number; depth: number };

/**
 * Rows of piles, biggest footprints first, with the phone lying flat right beside
 * the biggest so the two are easy to compare; x runs left to right and z back to
 * front, centred on 0. With table, everything sits on 6-foot folding tables, as
 * many as it takes side by side, so people can see how much of a table it fills.
 */
export function layoutHaul(all: Pile[], opts: { table?: boolean } = {}): HaulLayout {
	const phone: Pile = { key: 'phone', label: 'phone', art: '', w: PHONE.w, d: PHONE.h, height: 0 };
	const items = [...all].sort((a, b) => b.d - a.d);
	const order = [...items.slice(0, 1), phone, ...items.slice(1)];
	const area = order.reduce((a, p) => a + (p.w + GAP) * (p.d + GAP), 0);
	const rowMax = opts.table ? TABLE.w - 2 * EDGE : Math.max(18, Math.sqrt(area) * 1.3);
	const depthMax = opts.table ? TABLE.d - 2 * EDGE : Infinity;

	const placed: Placed[] = [];
	const onTable: number[] = [];
	let table = 0;
	let x = 0;
	let z = 0;
	let rowDepth = 0;
	for (const p of order) {
		if (x > 0 && x + p.w > rowMax) {
			z += rowDepth + GAP;
			x = 0;
			rowDepth = 0;
		}
		if (z > 0 && z + p.d > depthMax) {
			table++;
			x = z = rowDepth = 0;
		}
		const offset = opts.table ? table * (TABLE.w + BETWEEN) + EDGE : 0;
		placed.push({ ...p, x: offset + x + p.w / 2, z: (opts.table ? EDGE : 0) + z + p.d / 2 });
		onTable.push(table);
		x += p.w + GAP;
		rowDepth = Math.max(rowDepth, p.d);
	}

	// placed[i] went on table onTable[i]; centre each table's piles on it, like a booth display.
	if (opts.table) {
		for (let n = 0; n <= table; n++) {
			const mine = placed.filter((_, i) => onTable[i] === n);
			const left = Math.min(...mine.map((p) => p.x - p.w / 2)), right = Math.max(...mine.map((p) => p.x + p.w / 2));
			const back = Math.min(...mine.map((p) => p.z - p.d / 2)), front = Math.max(...mine.map((p) => p.z + p.d / 2));
			const dx = n * (TABLE.w + BETWEEN) + TABLE.w / 2 - (left + right) / 2;
			const dz = TABLE.d / 2 - (back + front) / 2;
			for (const p of mine) (p.x += dx), (p.z += dz);
		}
	}

	let width: number;
	let depth: number;
	let tables: Table[] = [];
	if (opts.table) {
		width = (table + 1) * TABLE.w + table * BETWEEN;
		depth = TABLE.d;
		tables = Array.from({ length: table + 1 }, (_, n) => ({ x: n * (TABLE.w + BETWEEN) + TABLE.w / 2 - width / 2, z: 0, ...TABLE }));
	} else {
		width = Math.max(...placed.map((p) => p.x + p.w / 2));
		depth = z + rowDepth;
	}
	for (const p of placed) {
		p.x -= width / 2;
		p.z -= depth / 2;
	}
	const ph = placed.find((p) => p.key === 'phone')!;
	return { piles: placed.filter((p) => p !== ph), phone: { x: ph.x, z: ph.z }, tables, width, depth };
}

// Things everyone has held, by volume in cubic inches, smallest first.
const LIKE = [
	{ name: 'a deck of cards', in3: 3.5 * 2.5 * 0.75 },
	{ name: 'a paperback book', in3: 7 * 4.5 * 1.2 },
	{ name: 'a shoebox', in3: 13 * 8 * 5 },
	{ name: 'a box of printer paper', in3: 17.5 * 11.5 * 10.5 }
];

/**
 * Everything in the request stacked together, compared to something familiar:
 * "about the size of a shoebox", or "about 3 boxes of printer paper". Helps
 * people feel how much they'll get (they tend to ask for too little).
 */
export function haulSize(all: Pile[]): string {
	const v = all.reduce((a, p) => a + p.w * p.d * p.height, 0);
	const box = LIKE[LIKE.length - 1];
	if (v > box.in3 * 2) return `about ${Math.round(v / box.in3)} boxes of printer paper`;
	const nearest = LIKE.reduce((best, t) => (Math.abs(Math.log(v / t.in3)) < Math.abs(Math.log(v / best.in3)) ? t : best));
	return `about the size of ${nearest.name}`;
}

/** How much of the tables the piles cover, 0 to 1 (0 with no tables). */
export function tableCover(layout: HaulLayout): number {
	if (!layout.tables.length) return 0;
	const tables = layout.tables.reduce((a, t) => a + t.w * t.d, 0);
	return layout.piles.reduce((a, p) => a + p.w * p.d, 0) / tables;
}
