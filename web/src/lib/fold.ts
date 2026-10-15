// A trifold brochure (binding 'trifold'): one sheet letter-folded in thirds.
// Pure math; the WebGL side lives in fold3d.ts.
//
// Looking at the outside art (images[0]) it reads flap | back | cover; the
// inside art (images[1]) is the other side of the sheet. To fold it, the flap
// tucks in over the back, then the cover folds down on top. It opens the other
// way round: position runs from 0 (folded, the cover facing you) through 1 (the
// cover open, the flap still tucked in) to 2 (open flat, the inside facing you).

export const FOLD_STEPS = 2;

const SPRING = (3 * Math.PI) / 180; // a folded cover springs open a hair
const CREASE = (10 * Math.PI) / 180; // open, each outer panel still tips in along its crease
const LIFT = (8 * Math.PI) / 180; // with the cover off it, the flap lifts a little

const clamp = (x: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, x));
const smooth = (a: number, b: number, x: number) => {
	const t = clamp((x - a) / (b - a), 0, 1);
	return t * t * (3 - 2 * t);
};
const lerp = (a: number, b: number, t: number) => a + (b - a) * t;

/**
 * How far each outer panel is folded in over the back panel at a position,
 * radians: 0 = lying flat beside it, π = folded flat on top of it.
 */
export function foldAngles(position: number): { cover: number; flap: number } {
	const p = clamp(position, 0, FOLD_STEPS);
	const cover = lerp(Math.PI - SPRING, CREASE, smooth(0, 1, p));
	// The flap stays tucked until the cover is well clear of it, then lifts on its crease.
	const flap = p <= 1 ? Math.PI - LIFT * smooth(0.6, 1, p) : lerp(Math.PI - LIFT, CREASE, smooth(1, 2, p));
	return { cover, flap };
}

export type Panel = 'flap' | 'back' | 'cover';

/** The part of the outside or inside art (u from 0 at its left to 1 at its right) printed on a panel. */
export function panelU(panel: Panel, side: 'outside' | 'inside'): [number, number] {
	const i = { flap: 0, back: 1, cover: 2 }[panel];
	const k = side === 'outside' ? i : 2 - i;
	return [k / 3, (k + 1) / 3];
}

/** A w × h sheet t thick, folded in thirds. */
export const foldedSize = (w: number, h: number, t: number) => ({ w: w / 3, h, t: t * 3 });

export const foldLabel = (step: number): string => ['Folded', 'Cover open', 'Open flat'][clamp(Math.round(step), 0, FOLD_STEPS)];

/** How long the brochure takes to open and fold itself back up, ms. */
export const FOLD_CYCLE_MS = 11000;

/**
 * Where a brochure that opens and folds itself is heading, t ms into its loop:
 * folded on the cover, the cover opens, then the flap, it stays open long
 * enough to read, then folds back up.
 */
export function foldCycle(t: number): number {
	const at = ((t % FOLD_CYCLE_MS) + FOLD_CYCLE_MS) % FOLD_CYCLE_MS;
	if (at < 2000) return 0;
	if (at < 3600) return 1;
	if (at < 8000) return 2;
	return 0;
}
