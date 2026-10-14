// Page bookkeeping for the 3D flip-through of booklets (magazines). Pure math;
// the WebGL side lives in book3d.ts.
//
// A booklet of n pages is ceil(n / 2) leaves. Page 2i is the front of leaf i
// and page 2i + 1 its back, so page 0 is the front cover and page n - 1 the
// back cover. The book's position runs from 0 (closed on the front cover)
// to `leaves` (closed on the back cover); a whole number k is spread k, with
// k leaves turned onto the left.

export const leafCount = (pages: number) => Math.ceil(pages / 2);

/** The pages lying open at spread k (null = nothing there). */
export function spreadPages(k: number, pages: number): { left: number | null; right: number | null } {
	const left = 2 * k - 1;
	const right = 2 * k;
	return { left: k > 0 && left < pages ? left : null, right: right < pages ? right : null };
}

export function spreadLabel(k: number, pages: number): string {
	if (k <= 0) return 'Front cover';
	if (k >= leafCount(pages)) return 'Back cover';
	const { left, right } = spreadPages(k, pages);
	const shown = [left, right].filter((p) => p != null).map((p) => p! + 1);
	return shown.length === 2 ? `Pages ${shown[0]}–${shown[1]} of ${pages}` : `Page ${shown[0]} of ${pages}`;
}

export type BookState = {
	leaf: number; // the leaf that's turning (or about to)
	turn: number; // 0 = lying on the right, 1 = turned onto the left
	left: number; // leaves stacked on the left, under it
	right: number; // leaves stacked on the right, under it
};

export function bookState(position: number, leaves: number): BookState {
	const p = Math.min(Math.max(position, 0), leaves);
	const leaf = Math.min(Math.floor(p), leaves - 1);
	return { leaf, turn: p - leaf, left: leaf, right: leaves - leaf - 1 };
}

/**
 * How far a turning leaf has swung at a point s along it (0 = spine, 1 = outer
 * edge), in radians from lying flat on the right. The outer edge leads, the
 * way a page curls when you lift it by the corner.
 */
export function leafAngle(turn: number, s: number): number {
	const a = Math.PI * turn + 1.1 * Math.sin(Math.PI * turn) * s;
	return Math.min(Math.max(a, 0), Math.PI);
}

/** Page images worth having loaded at a position: both covers and the pages around it. */
export function pagesNear(position: number, pages: number): number[] {
	const { leaf } = bookState(position, leafCount(pages));
	const near = new Set([0, pages - 1]);
	for (let i = 2 * leaf - 3; i <= 2 * leaf + 4; i++) if (i >= 0 && i < pages) near.add(i);
	return [...near].sort((a, b) => a - b);
}

/** The spread to come to rest on after a drag that ended at position, moving in dir (+1 forward, -1 back). */
export function settle(position: number, dir: number): number {
	const PAST = 0.2;
	if (dir > 0) return Math.floor(position) + (position - Math.floor(position) >= PAST ? 1 : 0);
	if (dir < 0) return Math.ceil(position) - (Math.ceil(position) - position >= PAST ? 1 : 0);
	return Math.round(position);
}
