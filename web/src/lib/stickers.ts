// Sticker packs in the 3D preview: pure data and math; the WebGL side lives in
// sticker3d.ts. Stickers reach the warehouse bundled in clear zip bags of 50
// and go out in 3s and 10s (loose) or whole bags (see PACKS in cart.ts).

/**
 * One sticker in a stack, in inches (~0.3 mm): laminated vinyl on its liner,
 * plus the air between stickers that never lie quite flat. A bag of 50 is
 * about 15 mm tall, like the bundles on the shelf at HQ.
 */
export const STICKER_THICKNESS_IN = 0.0118;
export const PER_BAG = 50;
/** Bags lie side by side this many across, then pile up. */
export const ACROSS = 4;

/**
 * Which pixels of a sticker's product photo are the sticker: everything except
 * the plain grey backdrop and its soft drop shadow, flooded in from the edges.
 * Returns 1 for sticker, 0 for backdrop, one byte per pixel.
 */
export function stickerMask(rgba: Uint8ClampedArray, w: number, h: number): Uint8Array {
	const lum = (i: number) => (rgba[i * 4] + rgba[i * 4 + 1] + rgba[i * 4 + 2]) / 3;
	const corners = [0, w - 1, (h - 1) * w, h * w - 1];
	const bg = corners.reduce((a, i) => a + lum(i), 0) / corners.length;
	// Transparent art (a PNG cutout) says where the sticker is by itself.
	const cutout = corners.some((i) => rgba[i * 4 + 3] < 128);
	// Otherwise backdrop and shadow are grey: no brighter than the backdrop (a
	// white die-cut border is), and not much darker (a black outline is).
	const backdrop = (i: number) => {
		if (cutout) return rgba[i * 4 + 3] < 128;
		const r = rgba[i * 4], g = rgba[i * 4 + 1], b = rgba[i * 4 + 2];
		const l = lum(i);
		return Math.max(r, g, b) - Math.min(r, g, b) < 18 && l <= bg + 6 && l >= bg - 60;
	};
	const mask = new Uint8Array(w * h).fill(1);
	const stack: number[] = [];
	const push = (i: number) => {
		if (mask[i] && backdrop(i)) {
			mask[i] = 0;
			stack.push(i);
		}
	};
	for (let x = 0; x < w; x++) push(x), push((h - 1) * w + x);
	for (let y = 0; y < h; y++) push(y * w), push(y * w + w - 1);
	while (stack.length) {
		const i = stack.pop()!;
		const x = i % w;
		if (x > 0) push(i - 1);
		if (x < w - 1) push(i + 1);
		if (i >= w) push(i - w);
		if (i < (h - 1) * w) push(i + w);
	}
	return mask;
}

export type Box = { x: number; y: number; w: number; h: number };

/** The smallest box holding every sticker pixel, or null if there are none. */
export function maskBounds(mask: Uint8Array, w: number, h: number): Box | null {
	let x0 = w, y0 = h, x1 = -1, y1 = -1;
	for (let y = 0; y < h; y++)
		for (let x = 0; x < w; x++)
			if (mask[y * w + x]) {
				x0 = Math.min(x0, x);
				x1 = Math.max(x1, x);
				y0 = Math.min(y0, y);
				y1 = Math.max(y1, y);
			}
	return x1 < 0 ? null : { x: x0, y: y0, w: x1 - x0 + 1, h: y1 - y0 + 1 };
}

/** A sticker's size in inches: as recorded, or about 3 inches across its long side. */
export function stickerSize(it: { width_in: number | null; height_in: number | null }, aspect: number) {
	if (it.width_in != null && it.height_in != null) return { w: it.width_in, h: it.height_in };
	return aspect >= 1 ? { w: 3, h: 3 / aspect } : { w: 3 * aspect, h: 3 };
}

export type Layout = {
	/** Stickers laid out loose, fanned so you can count them. */
	loose: number;
	/** Bags of 50, side by side ACROSS wide, then piled on top. */
	bags: { pile: number; level: number }[];
};

/** What a request for q stickers looks like; nothing requested yet shows one sticker. */
export function packLayout(q: number): Layout {
	if (q <= 0) return { loose: 1, bags: [] };
	const n = Math.floor(q / PER_BAG);
	return {
		loose: q % PER_BAG,
		bags: Array.from({ length: n }, (_, i) => ({ pile: i % ACROSS, level: Math.floor(i / ACROSS) }))
	};
}

/** Short tile label: "Pack of 50", "3 packs of 50", or "10 stickers" when loose. */
export function bagLabel(q: number): string {
	const bags = Math.floor(q / PER_BAG);
	if (bags === 0 || q % PER_BAG) return packLabel(q);
	return bags === 1 ? `Pack of ${PER_BAG}` : `${bags} packs of ${PER_BAG}`;
}

/** "150 stickers, in 3 bags of 50": what the preview is showing. */
export function packLabel(q: number): string {
	if (q <= 0) return '1 sticker';
	const bags = Math.floor(q / PER_BAG);
	if (bags === 0) return `${q} stickers`;
	return `${q} stickers, in ${bags === 1 ? 'a bag' : `${bags} bags of ${PER_BAG}`}`;
}
