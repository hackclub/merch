// True-to-scale size previews: a printed item drawn next to a phone, both at
// the same pixels-per-inch, so people can tell a 4x6 card from an 11x17 poster.

export type Size = { w: number; h: number }; // inches

/** A typical phone (iPhone 15 is 2.82 × 5.81 in): everyone knows how big one is. */
export const PHONE: Size = { w: 2.8, h: 5.8 };

/** Space between the item and the phone, in inches. */
export const GAP_IN = 2;

/** Share of the stage the scene may fill; the rest absorbs perspective, rotation and labels. */
const FILL_W = 0.8;
const FILL_H = 0.72;

export const hasSize = (it: { width_in: number | null; height_in: number | null }) =>
	it.width_in != null && it.height_in != null;

const fmt = (n: number) => String(Math.round(n * 100) / 100);

export const sizeLabel = (w: number, h: number) =>
	`${fmt(w)} × ${fmt(h)} in (${Math.round(w * 2.54)} × ${Math.round(h * 2.54)} cm)`;

/** Largest scale at which the item and the phone, side by side, fit the stage. */
export function pixelsPerInch(item: Size, stage: Size): number {
	const sceneW = item.w + GAP_IN + PHONE.w;
	const sceneH = Math.max(item.h, PHONE.h);
	return Math.min((stage.w * FILL_W) / sceneW, (stage.h * FILL_H) / sceneH);
}
