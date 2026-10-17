// Postcard requests in the 3D preview: pure layout; the WebGL side lives in
// postcard3d.ts. Postcards come from the printer banded in bundles of 50 and go
// out in 5s up to 25, then whole bundles (see PACKS in cart.ts).

export const PER_BUNDLE = 50;
/** A postcard without a measured thickness: 14 pt card stock, in inches. */
export const CARD_IN = 0.014;
/** Bundles lie side by side this many across, then start a new row. */
const ACROSS = 3;
/** Space between stacks, in inches. */
const GAP = 1;

export type Stack = { cards: number; band: boolean; x: number; z: number }; // centre of the stack's footprint

/**
 * The stacks a request arrives as: a banded bundle per 50, then the rest loose
 * (at least one card, so there's always something to see), laid out in rows of
 * three w × d cards, centred on 0.
 */
export function postcardStacks(q: number, w: number, d: number): { stacks: Stack[]; width: number; depth: number } {
	const counts = [...Array(Math.floor(Math.max(q, 0) / PER_BUNDLE)).fill(PER_BUNDLE), q > 0 ? q % PER_BUNDLE : 1].filter((n) => n > 0);
	const cols = Math.min(ACROSS, counts.length);
	const rows = Math.ceil(counts.length / ACROSS);
	const width = cols * w + (cols - 1) * GAP;
	const depth = rows * d + (rows - 1) * GAP;
	const stacks = counts.map((cards, i) => ({
		cards,
		band: cards === PER_BUNDLE,
		x: (i % ACROSS) * (w + GAP) + w / 2 - width / 2,
		z: Math.floor(i / ACROSS) * (d + GAP) + d / 2 - depth / 2
	}));
	return { stacks, width, depth };
}

/** "15 postcards", "150 postcards · 3 bundles of 50". */
export function postcardLabel(q: number): string {
	const n = Math.max(q, 1);
	const bundles = Math.floor(n / PER_BUNDLE);
	const cards = `${n} postcard${n === 1 ? '' : 's'}`;
	if (!bundles) return cards;
	return `${cards} · ${bundles === 1 ? 'a bundle' : `${bundles} bundles`} of ${PER_BUNDLE}`;
}

/**
 * The top card turning over (p = 0 front up, 1 back up): its turn about its
 * long axis, and how far it rises so its edge clears the stack under it.
 */
export function cardFlip(p: number, depth: number): { rotX: number; lift: number } {
	const k = Math.min(1, Math.max(0, p));
	if (k === 0) return { rotX: 0, lift: 0 };
	const rotX = k * Math.PI;
	return { rotX, lift: (depth / 2) * Math.sin(rotX) };
}
