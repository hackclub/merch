// Things that aren't printed (the Orpheus plushie) are shown from a 3D model of
// the real thing: a GLB file (items.model), scaled to the item's measured height.
// model3d.ts draws it; this is the geometry, kept pure for tests. Units are inches.
import type { Item } from './types';

export type ModelOptions = {
	kind: 'model';
	url: string; // the GLB file
	front: string; // a photo, shown until the model loads or without WebGL
	widthIn: number | null; // side to side
	heightIn: number | null; // floor to top
	phone?: boolean; // stand a phone beside it for scale (default true)
};

/** A plush toy's usual size, for a model without measurements. */
export const MODEL_DEFAULT = { w: 8, h: 10 };

/** How many sit side by side in a row in the haul. */
export const MODEL_COLS = 3;

/**
 * One unit's size: measured, or a plush toy's. Depth isn't measured: it's taken as
 * the larger of width and height, since a sitting plush's tail reaches back about
 * as far as it's tall (it would poke into the next pile otherwise).
 */
export function modelSize(item: Pick<Item, 'width_in' | 'height_in'>): { w: number; h: number; d: number } {
	const h = item.height_in ?? MODEL_DEFAULT.h;
	const w = item.width_in ?? (item.height_in != null ? h * (MODEL_DEFAULT.w / MODEL_DEFAULT.h) : MODEL_DEFAULT.w);
	return { w, h, d: Math.max(w, h) };
}

/** Columns and rows for n units sitting side by side. */
export const modelGrid = (n: number) => {
	const cols = Math.max(1, Math.min(n, MODEL_COLS));
	return { cols, rows: Math.max(1, Math.ceil(n / cols)) };
};

/** "10 in tall (25 cm)"; "About 10 in tall" when it isn't measured. */
export function modelLabel(heightIn: number | null): string {
	const h = heightIn ?? MODEL_DEFAULT.h;
	const s = `${Math.round(h * 10) / 10} in tall (${Math.round(h * 2.54)} cm)`;
	return heightIn == null ? `About ${s}` : s;
}
