// "Everything you're about to get": each line of the request as a pile at true
// size (inches), as tall as its count makes it, laid out on a table next to a
// phone for scale. haul3d.ts draws it; this is the geometry, kept pure for tests.
import { presetQuantities } from './cart';
import { foldedSize, panelU } from './fold';
import { isBooklet, isTrifold, shipsWith, thumbnail } from './items';
import { sheetThickness } from './paper';
import { CARD_IN, PER_BUNDLE } from './postcards';
import { PHONE } from './scale';
import { PER_BAG, STICKER_THICKNESS_IN } from './stickers';
import type { Item, RequestLine } from './types';

export type HaulLine = { item: Item; quantity: number; size?: string };

export type PileKind = 'paper' | 'booklet' | 'sticker' | 'shirt' | 'box';

export type Pile = {
	key: string;
	label: string; // "3 × Poster"
	art: string; // image on top of the pile ('' = plain)
	artPart?: { from: number; to: number; aspect: number }; // only this slice of art (u from…to) shows on top; aspect is the whole art's
	fold?: { outside: string; inside: string; w: number; sheet: number }; // a trifold: the top one opens out flat and folds back up (w: folded; sheet: one sheet's thickness)
	w: number; // footprint, left to right
	d: number; // footprint, front to back
	height: number; // the whole stack
	kind?: PileKind;
	count?: number; // items in this pile
	finish?: string; // paper finish ('matte' | 'gloss' | 'linen' | 'uncoated' | '')
	binding?: string; // booklet binding ('staple' | 'perfect')
	band?: boolean; // a printer's bundle, with a paper band around it
	bag?: boolean; // a clear zip bag of 50 stickers
	widthIn?: number | null;
	heightIn?: number | null;
};

// A folded T-shirt, roughly.
export const SHIRT = { w: 9.5, d: 12, t: 1.2 };
// A die-cut sticker with no measurements: about 3 in across.
const STICKER = { w: 3, d: 3 };
// Something we have no measurements for: a small box.
const UNKNOWN = { w: 6, d: 6, t: 1 };
// Booklets without a measured thickness: about 0.1 mm a page.
const PAGE_IN = 0.004;
/**
 * Bound booklets don't lie flat: a stapled one bulges at the fold and its cover
 * springs open, and a perfect-bound one a little less, so a stack of them stands
 * taller than its paper. Each booklet takes up its thickness times this.
 */
export const LOFT = { staple: 1.25, perfect: 1.06, trifold: 1.3 } as const;
const loft = (binding: string) => LOFT[binding as keyof typeof LOFT] ?? 1;

/** "4 in tall (10 cm)", "0.7 in tall (1.8 cm)". */
export function tall(inches: number): string {
	const n = inches >= 1 ? inches.toFixed(1).replace(/\.0$/, '') : inches.toFixed(2).replace(/0$/, '');
	const cm = inches * 2.54;
	return `${n} in tall (${cm >= 10 ? Math.round(cm) : cm.toFixed(1)} cm)`;
}

/** Where each booklet in a hand-piled stack sits: its bottom (y), a slight shift (dx, dz, inches) and turn (yaw, radians). */
export type StackedBooklet = { y: number; dx: number; dz: number; yaw: number };

/**
 * A stack of count booklets, each step taller than the last: piled by hand, so
 * each one sits a little off square. The same count always gives the same pile.
 */
export function bookletStack(count: number, step: number): StackedBooklet[] {
	// A fixed pseudo-random wobble in -1…1 for booklet i (no Math.random, so the pile doesn't jump on re-render).
	const wobble = (i: number, salt: number) => {
		const x = Math.sin(i * 12.9898 + salt * 78.233) * 43758.5453;
		return (x - Math.floor(x)) * 2 - 1;
	};
	return Array.from({ length: count }, (_, i) => ({
		y: i * step, dx: wobble(i, 1) * 0.08, dz: wobble(i, 2) * 0.08, yaw: wobble(i, 3) * 0.04
	}));
}

/** One item's line for a quantity preview: the amount picked, or the smallest quick pick before anything is. */
export const pickedLines = (item: Item, inCart: number): HaulLine[] => [
	{ item, quantity: inCart > 0 ? inCart : (presetQuantities(item)[0] ?? 1) }
];

/** Expands each line with whatever its item always ships with (e.g. the magazine's intro card). */
export function withIncludes(lines: HaulLine[], includes: Item[] = []): HaulLine[] {
	const active = lines.filter((l) => l.quantity > 0);
	if (!includes.length) return active;
	const pool = [...active.map((l) => l.item), ...includes];
	const out: HaulLine[] = [];
	const add = (line: HaulLine) => {
		const existing = !line.size && out.find((l) => l.item.id === line.item.id && !l.size);
		if (existing) existing.quantity += line.quantity;
		else out.push({ ...line });
	};
	for (const l of active) {
		add(l);
		for (const extra of shipsWith(l.item, pool)) add({ item: extra, quantity: l.quantity });
	}
	return out;
}

export function piles(lines: HaulLine[], includes: Item[] = []): Pile[] {
	return withIncludes(lines, includes).flatMap((line): Pile[] => {
		const { item, quantity } = line;
		// Postcards ship in banded bundles of 50: one pile each, then the rest loose.
		if (item.packs === 'postcard' && quantity >= PER_BUNDLE) {
			const one = pileOf({ ...line, quantity: PER_BUNDLE });
			const bundles = Array.from({ length: Math.floor(quantity / PER_BUNDLE) }, (_, n) => ({ ...one, key: `${one.key}:${n}`, band: true }));
			const rest = quantity % PER_BUNDLE;
			return rest ? [...bundles, { ...pileOf({ ...line, quantity: rest }), key: `${one.key}:rest` }] : bundles;
		}
		// Stickers ship in sealed clear zip bags of 50: one pack each, then any loose remainder.
		if (item.packs === 'sticker' && quantity >= PER_BAG) {
			const one = pileOf({ ...line, quantity: PER_BAG });
			const bags = Array.from({ length: Math.floor(quantity / PER_BAG) }, (_, n) => ({ ...one, key: `${one.key}:${n}`, bag: true }));
			const rest = quantity % PER_BAG;
			return rest ? [...bags, { ...pileOf({ ...line, quantity: rest }), key: `${one.key}:rest` }] : bags;
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
	if (item.packs === 'sticker') {
		const w = item.width_in ?? STICKER.w;
		const d = item.height_in ?? STICKER.d;
		return {
			key, label, art, w, d, height: STICKER_THICKNESS_IN * quantity,
			kind: 'sticker', count: quantity, widthIn: item.width_in, heightIn: item.height_in
		};
	}
	if (item.width_in != null && item.height_in != null && isTrifold(item)) {
		// Trifolds ship folded, cover up, springing open a little at the creases. The
		// footprint is the open sheet's, so the top one has room to open out flat.
		const f = foldedSize(item.width_in, item.height_in, sheetThickness(item));
		const [from, to] = panelU('cover', 'outside');
		return {
			key, label, art, artPart: { from, to, aspect: item.width_in / item.height_in },
			fold: { outside: item.images[0], inside: item.images[1], w: f.w, sheet: sheetThickness(item) },
			w: item.width_in, d: f.h, height: f.t * loft('trifold') * quantity, kind: 'paper', count: quantity, finish: item.finish
		};
	}
	if (item.width_in != null && item.height_in != null) {
		const booklet = isBooklet(item);
		const unit = booklet
			? (item.thickness_in ?? item.pages.length * PAGE_IN) * loft(item.binding)
			: item.packs === 'postcard'
				? (item.thickness_in ?? CARD_IN)
				: sheetThickness(item);
		return {
			key, label, art, w: item.width_in, d: item.height_in, height: unit * quantity,
			kind: booklet ? 'booklet' : 'paper', count: quantity, finish: item.finish,
			binding: booklet ? item.binding : undefined
		};
	}
	const shirt = item.variants.length > 0;
	const box = shirt ? SHIRT : UNKNOWN;
	return { key, label, art, w: box.w, d: box.d, height: box.t * quantity, kind: shirt ? 'shirt' : 'box', count: quantity };
}

export type Placed = Pile & { x: number; z: number }; // centre of the footprint on the table
export type Table = { x: number; z: number; w: number; d: number }; // centre and size of a tabletop

/** A standard 6-foot folding table, the kind at every club fair (h: floor to tabletop). */
export const TABLE = { w: 72, d: 30, h: 29 };
/** A 13-inch laptop, open: the base is 12 × 8.4 in, and the lid leans back 2 in past it. */
export const LAPTOP = { w: 12, d: 10.4, base: 8.4 };
/** Space between piles, between the piles and the table's edge, and between tables, in inches. */
const GAP = 0.75;
const EDGE = 3;
const BETWEEN = 12;

export type HaulLayout = {
	piles: Placed[];
	phone: { x: number; z: number };
	laptop: { x: number; z: number } | null; // centre of LAPTOP's footprint; only on tables
	tables: Table[];
	width: number;
	depth: number;
};

/**
 * Rows of piles, biggest footprints first, with the phone lying flat right beside
 * the biggest so the two are easy to compare; x runs left to right and z back to
 * front, centred on 0. With table, everything sits close together on 6-foot
 * folding tables, as many as it takes side by side, next to an open laptop, so
 * people can see how much of a table it fills.
 */
export function layoutHaul(all: Pile[], opts: { table?: boolean } = {}): HaulLayout {
	const phone: Pile = { key: 'phone', label: 'phone', art: '', w: PHONE.w, d: PHONE.h, height: 0 };
	const items = [...all].sort((a, b) => b.d - a.d);
	const laptop: Pile = { key: 'laptop', label: 'laptop', art: '', w: LAPTOP.w, d: LAPTOP.d, height: 0 };
	const order = [...(opts.table ? [laptop] : []), ...items.slice(0, 1), phone, ...items.slice(1)];
	const area = order.reduce((a, p) => a + (p.w + GAP) * (p.d + GAP), 0);
	// A compact cluster, not rows spread edge to edge.
	const cluster = Math.max(18, Math.sqrt(area) * 1.3);
	const depthMax = opts.table ? TABLE.d - 2 * EDGE : Infinity;

	const pack = (rowMax: number) => {
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
		return { placed, onTable, table, z, rowDepth };
	};
	// On a table, the narrowest tight cluster that fits its depth; rows across as many tables as it takes if none does.
	const full = TABLE.w - 2 * EDGE;
	let packed: ReturnType<typeof pack> | undefined;
	if (opts.table) {
		for (let w = Math.min(cluster, full); !packed && w < full + 1; w++) {
			const s = skyline(order, Math.min(w, full));
			if (s.depth <= depthMax)
				packed = { placed: s.placed.map((p) => ({ ...p, x: p.x + EDGE, z: p.z + EDGE })), onTable: order.map(() => 0), table: 0, z: 0, rowDepth: 0 };
		}
	}
	packed ??= pack(opts.table ? full : cluster);
	const { placed, onTable, table, z, rowDepth } = packed;

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
		tables = Array.from({ length: table + 1 }, (_, n) => ({ x: n * (TABLE.w + BETWEEN) + TABLE.w / 2 - width / 2, z: 0, w: TABLE.w, d: TABLE.d }));
	} else {
		width = Math.max(...placed.map((p) => p.x + p.w / 2));
		depth = z + rowDepth;
	}
	for (const p of placed) {
		p.x -= width / 2;
		p.z -= depth / 2;
	}
	const ph = placed.find((p) => p.key === 'phone')!;
	const lap = placed.find((p) => p.key === 'laptop');
	return {
		piles: placed.filter((p) => p !== ph && p !== lap), phone: { x: ph.x, z: ph.z },
		laptop: lap ? { x: lap.x, z: lap.z } : null, tables, width, depth
	};
}

/**
 * Packs piles in order into a strip width wide, each as far back as it will go
 * (a skyline: the deepest point reached so far at every x), so short piles tuck
 * in beside tall ones instead of leaving gaps. Returns footprint centres.
 */
function skyline(order: Pile[], width: number): { placed: Placed[]; depth: number } {
	let segs = [{ x: 0, w: width, z: 0 }];
	const placed: Placed[] = [];
	for (const p of order) {
		let best = { x: 0, z: Infinity };
		for (const s of segs) {
			if (s.x > 0 && s.x + p.w > width + 1e-9) break;
			const z = Math.max(...segs.filter((o) => o.x < s.x + p.w - 1e-9 && o.x + o.w > s.x + 1e-9).map((o) => o.z));
			if (z < best.z - 1e-9) best = { x: s.x, z };
		}
		placed.push({ ...p, x: best.x + p.w / 2, z: best.z + p.d / 2 });
		// Raise the skyline under the pile (and the gap to its right).
		const from = best.x, to = Math.min(width, best.x + p.w + GAP), top = best.z + p.d + GAP;
		const next: typeof segs = [];
		for (const o of segs) {
			if (o.x + o.w <= from + 1e-9 || o.x >= to - 1e-9) next.push(o);
			else {
				if (o.x < from) next.push({ x: o.x, w: from - o.x, z: o.z });
				if (o.x + o.w > to) next.push({ x: to, w: o.x + o.w - to, z: o.z });
			}
		}
		next.push({ x: from, w: to - from, z: top });
		segs = next.sort((a, b) => a.x - b.x);
	}
	return { placed, depth: Math.max(0, ...placed.map((p) => p.z + p.d / 2)) };
}

/** How much of the tables the piles cover, 0 to 1 (0 with no tables). */
export function tableCover(layout: HaulLayout): number {
	if (!layout.tables.length) return 0;
	const tables = layout.tables.reduce((a, t) => a + t.w * t.d, 0);
	return layout.piles.reduce((a, p) => a + p.w * p.d, 0) / tables;
}

/** A request's lines as haul lines (what shipped, or what's asked for until then), skipping items we don't have. */
export function requestHaul(lines: RequestLine[], items: Item[]): HaulLine[] {
	const byId = new Map(items.map((i) => [i.id, i]));
	return lines.flatMap((l) => {
		const item = byId.get(l.item_id);
		return item && l.quantity > 0 ? [{ item, quantity: l.quantity, ...(l.variant ? { size: l.variant } : {}) }] : [];
	});
}
