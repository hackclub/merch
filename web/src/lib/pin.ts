// Enamel pins in 3D: pure data and math; the WebGL side lives in pin3d.ts. A pin
// is die-struck metal cut to the outline of its product photo: its metal lines
// (the gold in the photo) stand proud, the enamel between them sits a little
// lower, and the back carries posts with butterfly clutches. Units are inches.
import { sizeLabel } from './scale';

/** Pins are items with the enamel finish. */
export const isPin = (item: { finish: string }) => item.finish === 'enamel';

/** The metal blank, front to back. */
export const BODY_IN = 0.05;
/** How far the metal lines stand above the enamel. */
export const RELIEF_IN = 0.012;
/** Enamel sits this far up the relief (hard enamel nearly fills it; 0 would be bare recesses). */
export const ENAMEL_FILL = 0.35;
/** The post, from the back of the pin to its tip, and the clutch holding it. */
export const POST_IN = 0.3;
export const CLUTCH_IN = 0.32;
/** The metal rim round the outline, inches. */
const RIM_IN = 0.025;
/** What a pin with no measurements is: about an inch across. */
const DEFAULT_IN = 1;

/** How a pin lies in a request's pile: face up, resting on its clutches. */
export const PIN_PILE_H = BODY_IN + RELIEF_IN + POST_IN;
/** Space between pins laid out side by side. */
export const PIN_GAP_IN = 0.3;

/** A pin's size in inches, keeping its photo's aspect inside the measured box. */
export function pinSize(widthIn: number | null, heightIn: number | null, aspect: number): { w: number; h: number } {
	if (widthIn == null && heightIn == null) return aspect >= 1 ? { w: DEFAULT_IN, h: DEFAULT_IN / aspect } : { w: DEFAULT_IN * aspect, h: DEFAULT_IN };
	const W = widthIn ?? (heightIn as number) * aspect;
	const H = heightIn ?? (widthIn as number) / aspect;
	return W / H > aspect ? { w: H * aspect, h: H } : { w: W, h: W / aspect };
}

/** Whether a pixel is plated metal: gold (warm and saturated) or a bright specular glint. */
export function isMetal(r: number, g: number, b: number): boolean {
	const max = Math.max(r, g, b), min = Math.min(r, g, b);
	const v = max / 255;
	const s = max ? (max - min) / max : 0;
	if (v > 0.9 && s < 0.2) return true; // a highlight off the polished metal
	if (s < 0.22 || v < 0.3) return false;
	let h = 0;
	if (max === r) h = ((g - b) / (max - min)) * 60;
	else if (max === g) h = ((b - r) / (max - min) + 2) * 60;
	else h = ((r - g) / (max - min) + 4) * 60;
	if (h < 0) h += 360;
	return h >= 28 && h <= 70;
}

/**
 * Which of the pin's pixels are metal (isMetal), with stray pixels smoothed
 * away by a 3 × 3 majority vote, twice: glare on the enamel and dark spots on
 * the gold in a photo shouldn't dent the relief.
 */
export function metalMap(rgba: Uint8ClampedArray, mask: Uint8Array, w: number, h: number): Uint8Array {
	let metal = new Uint8Array(w * h);
	for (let i = 0; i < metal.length; i++) metal[i] = mask[i] && isMetal(rgba[i * 4], rgba[i * 4 + 1], rgba[i * 4 + 2]) ? 1 : 0;
	for (let pass = 0; pass < 2; pass++) {
		const next = new Uint8Array(w * h);
		for (let y = 0; y < h; y++)
			for (let x = 0; x < w; x++) {
				const i = y * w + x;
				if (!mask[i]) continue;
				let on = 0, n = 0;
				for (let yy = Math.max(0, y - 1); yy <= Math.min(h - 1, y + 1); yy++)
					for (let xx = Math.max(0, x - 1); xx <= Math.min(w - 1, x + 1); xx++) {
						const j = yy * w + xx;
						if (mask[j]) (on += metal[j]), n++;
					}
				next[i] = on * 2 > n ? 1 : 0;
			}
		metal = next;
	}
	return metal;
}

/** Keeps only the biggest connected piece of mask (dust and specks in the backdrop drop out). */
export function largestPiece(mask: Uint8Array, w: number, h: number): Uint8Array {
	const label = new Int32Array(w * h);
	let best = 0, bestSize = 0, next = 0;
	const stack: number[] = [];
	for (let start = 0; start < mask.length; start++) {
		if (!mask[start] || label[start]) continue;
		const id = ++next;
		let size = 0;
		label[start] = id;
		stack.push(start);
		while (stack.length) {
			const i = stack.pop()!;
			size++;
			const x = i % w;
			for (const j of [x > 0 ? i - 1 : -1, x < w - 1 ? i + 1 : -1, i - w, i + w]) {
				if (j >= 0 && j < mask.length && mask[j] && !label[j]) {
					label[j] = id;
					stack.push(j);
				}
			}
		}
		if (size > bestSize) (best = id), (bestSize = size);
	}
	const out = new Uint8Array(w * h);
	for (let i = 0; i < out.length; i++) out[i] = label[i] === best && best ? 1 : 0;
	// Fill holes: anything not reachable from the border is part of the pin.
	const outside = new Uint8Array(w * h);
	const push = (i: number) => {
		if (!out[i] && !outside[i]) (outside[i] = 1), stack.push(i);
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
	for (let i = 0; i < out.length; i++) if (!outside[i]) out[i] = 1;
	return out;
}

/** Shaves a pixel off the mask's edge: the anti-aliased halo where a photo's backdrop meets the pin. */
export function erode(mask: Uint8Array, w: number, h: number): Uint8Array {
	const out = new Uint8Array(w * h);
	for (let y = 1; y < h - 1; y++)
		for (let x = 1; x < w - 1; x++) {
			const i = y * w + x;
			out[i] = mask[i] && mask[i - 1] && mask[i + 1] && mask[i - w] && mask[i + w] ? 1 : 0;
		}
	return out;
}

/**
 * The face's color for one pixel of the photo: plating a little richer than a
 * photo of it (the 3D view adds its own reflections), enamel a little deeper.
 * A white glint off the plating takes the pin's `gold` instead, or it would
 * render as silver.
 */
export function faceColor(r: number, g: number, b: number, metal: boolean, gold: [number, number, number]): [number, number, number] {
	const c = (v: number) => Math.max(0, Math.min(255, Math.round(v)));
	if (!metal) return [c(r * 0.82), c(g * 0.82), c(b * 0.82)];
	const max = Math.max(r, g, b);
	if (max === 0 || (max - Math.min(r, g, b)) / max < 0.22) {
		const k = Math.min(1.2, Math.max(0.8, (r + g + b) / Math.max(1, gold[0] + gold[1] + gold[2])));
		[r, g, b] = [gold[0] * k, gold[1] * k, gold[2] * k];
	}
	const l = (r + g + b) / 3;
	// Warmed a touch toward orange: in shadow, a yellow photo-gold reads olive.
	return [c((l + (r - l) * 1.3) * 0.92), c((l + (g - l) * 1.3) * 0.86), c((l + (b - l) * 1.3) * 0.8)];
}

/** The average color of the plating in the photo: its metal pixels that aren't glints. */
export function plating(rgba: Uint8ClampedArray, metal: Uint8Array): [number, number, number] | null {
	let r = 0, g = 0, b = 0, n = 0;
	for (let i = 0; i < metal.length; i++) {
		if (!metal[i]) continue;
		const R = rgba[i * 4], G = rgba[i * 4 + 1], B = rgba[i * 4 + 2];
		const max = Math.max(R, G, B);
		if (!max || (max - Math.min(R, G, B)) / max < 0.22) continue;
		(r += R), (g += G), (b += B), n++;
	}
	return n >= 50 ? [r / n, g / n, b / n] : null;
}

export type Point = [number, number];

/**
 * The outline of mask's one piece, as a closed polygon through pixel corners
 * (x right, y down), traced clockwise on screen, averaged over `radius`
 * corners each way (so pixel stair-steps become a smooth curve) and
 * simplified to within `tolerance` pixels.
 */
export function traceOutline(mask: Uint8Array, w: number, h: number, tolerance = 0.7, radius = 0): Point[] {
	const on = (x: number, y: number) => x >= 0 && y >= 0 && x < w && y < h && mask[y * w + x] === 1;
	let sx = -1, sy = -1;
	for (let i = 0; i < mask.length && sx < 0; i++) if (mask[i]) (sx = i % w), (sy = Math.floor(i / w));
	if (sx < 0) return [];
	// Walk the lattice of pixel corners with the pin on our right, starting along
	// the top edge of the first pixel, heading right. Left of heading (dx, dy) is
	// (dy, -dx) with y down.
	const pts: Point[] = [];
	let x = sx, y = sy, dx = 1, dy = 0;
	const limit = 4 * (w + 1) * (h + 1);
	for (let n = 0; n < limit; n++) {
		pts.push([x, y]);
		x += dx;
		y += dy;
		// The pixels just ahead of the corner, on our left and on our right.
		const aheadL = on(Math.floor(x + (dx + dy) / 2), Math.floor(y + (dy - dx) / 2));
		const aheadR = on(Math.floor(x + (dx - dy) / 2), Math.floor(y + (dy + dx) / 2));
		if (aheadL) [dx, dy] = [dy, -dx];
		else if (!aheadR) [dx, dy] = [-dy, dx];
		if (x === sx && y === sy && dx === 1 && dy === 0) break;
	}
	return simplify(corners(smooth(pts, Math.max(0, Math.round(radius)))), tolerance);
}

/** Each point moved to the average of the 2r + 1 points round it on the ring. */
function smooth(pts: Point[], r: number): Point[] {
	if (r === 0 || pts.length < 2 * r + 3) return pts;
	return pts.map((_, i) => {
		let x = 0, y = 0;
		for (let k = -r; k <= r; k++) {
			const p = pts[(i + k + pts.length) % pts.length];
			x += p[0];
			y += p[1];
		}
		return [x / (2 * r + 1), y / (2 * r + 1)];
	});
}

/** Drops the points in the middle of straight runs. */
function corners(pts: Point[]): Point[] {
	return pts.filter((p, i) => {
		const a = pts[(i + pts.length - 1) % pts.length], b = pts[(i + 1) % pts.length];
		return Math.abs((p[0] - a[0]) * (b[1] - p[1]) - (p[1] - a[1]) * (b[0] - p[0])) > 1e-9;
	});
}

/** Douglas–Peucker on a closed ring, split at its first point and the point farthest from it. */
function simplify(pts: Point[], tol: number): Point[] {
	if (pts.length < 8) return pts;
	let far = 0, farD = 0;
	for (let i = 1; i < pts.length; i++) {
		const d = Math.hypot(pts[i][0] - pts[0][0], pts[i][1] - pts[0][1]);
		if (d > farD) (farD = d), (far = i);
	}
	const run = (seg: Point[]): Point[] => {
		const keep = new Uint8Array(seg.length);
		keep[0] = keep[seg.length - 1] = 1;
		const go = (i: number, j: number) => {
			const [x0, y0] = seg[i], [x1, y1] = seg[j];
			const len = Math.hypot(x1 - x0, y1 - y0) || 1;
			let k = -1, kd = tol;
			for (let m = i + 1; m < j; m++) {
				const d = Math.abs((x1 - x0) * (y0 - seg[m][1]) - (x0 - seg[m][0]) * (y1 - y0)) / len;
				if (d > kd) (kd = d), (k = m);
			}
			if (k >= 0) (keep[k] = 1), go(i, k), go(k, j);
		};
		go(0, seg.length - 1);
		return seg.filter((_, i) => keep[i]);
	};
	const first = run(pts.slice(0, far + 1));
	const second = run([...pts.slice(far), pts[0]]);
	return [...first.slice(0, -1), ...second.slice(0, -1)];
}

/** Signed area of a ring (positive when clockwise on screen, y down). */
export function ringArea(pts: Point[]): number {
	let a = 0;
	for (let i = 0; i < pts.length; i++) {
		const [x0, y0] = pts[i], [x1, y1] = pts[(i + 1) % pts.length];
		a += x0 * y1 - x1 * y0;
	}
	return a / 2;
}

/**
 * The pin's face as heights 0…1 per pixel: metal at 1, enamel at ENAMEL_FILL,
 * a metal rim round the outline, softened over `soften` pixels so the struck
 * edges are rounded. Off the pin is 0.
 */
export function reliefHeights(metal: Uint8Array, mask: Uint8Array, w: number, h: number, pxPerIn: number, soften = 1): Float32Array {
	const rim = Math.max(1, Math.round(RIM_IN * pxPerIn));
	const near = edgeDistance(mask, w, h, rim + 1);
	const raw = new Float32Array(w * h);
	for (let i = 0; i < raw.length; i++) raw[i] = !mask[i] ? 0 : metal[i] || near[i] <= rim ? 1 : ENAMEL_FILL;
	return soften > 0 ? boxBlur(raw, mask, w, h, soften) : raw;
}

/** Each pin pixel's distance in pixels (4-connected steps) to the nearest pixel off it, up to cap. */
export function edgeDistance(mask: Uint8Array, w: number, h: number, cap: number): Uint16Array {
	const d = new Uint16Array(w * h).fill(cap);
	let frontier: number[] = [];
	for (let i = 0; i < mask.length; i++) {
		if (!mask[i]) continue;
		const x = i % w, y = Math.floor(i / w);
		if (x === 0 || y === 0 || x === w - 1 || y === h - 1 || !mask[i - 1] || !mask[i + 1] || !mask[i - w] || !mask[i + w]) {
			d[i] = 1;
			frontier.push(i);
		}
	}
	for (let step = 2; step < cap && frontier.length; step++) {
		const next: number[] = [];
		for (const i of frontier) {
			const x = i % w;
			for (const j of [x > 0 ? i - 1 : -1, x < w - 1 ? i + 1 : -1, i - w, i + w]) {
				if (j >= 0 && j < mask.length && mask[j] && d[j] > step) (d[j] = step), next.push(j);
			}
		}
		frontier = next;
	}
	return d;
}

/** Averages heights over a (2r+1)² box, inside the pin only. */
function boxBlur(src: Float32Array, mask: Uint8Array, w: number, h: number, r: number): Float32Array {
	const out = new Float32Array(w * h);
	for (let y = 0; y < h; y++)
		for (let x = 0; x < w; x++) {
			const i = y * w + x;
			if (!mask[i]) continue;
			let sum = 0, n = 0;
			for (let yy = Math.max(0, y - r); yy <= Math.min(h - 1, y + r); yy++)
				for (let xx = Math.max(0, x - r); xx <= Math.min(w - 1, x + r); xx++) {
					const j = yy * w + xx;
					if (mask[j]) (sum += src[j]), n++;
				}
			out[i] = sum / n;
		}
	return out;
}

/**
 * Where the posts go, as fractions of the pin's box (0…1, y down): one in the
 * middle of a small pin, two a little in from the ends of a wide one (or a tall
 * one), each on the pin at its column's middle.
 */
export function postSpots(mask: Uint8Array, w: number, h: number, size: { w: number; h: number }): Point[] {
	const wide = size.w >= 1.4 && size.w >= size.h;
	const tall = size.h >= 1.4 && size.h > size.w;
	const at = (fx: number, fy: number): Point => {
		if (tall) {
			// The middle of the pin's row at fy.
			const y = Math.min(h - 1, Math.round(fy * (h - 1)));
			const xs = [...Array(w).keys()].filter((x) => mask[y * w + x]);
			return xs.length ? [(xs[0] + xs[xs.length - 1] + 1) / 2 / w, (y + 0.5) / h] : [fx, fy];
		}
		const x = Math.min(w - 1, Math.round(fx * (w - 1)));
		const ys = [...Array(h).keys()].filter((y) => mask[y * w + x]);
		return ys.length ? [(x + 0.5) / w, (ys[0] + ys[ys.length - 1] + 1) / 2 / h] : [fx, fy];
	};
	if (wide) return [at(0.27, 0.5), at(0.73, 0.5)];
	if (tall) return [at(0.5, 0.27), at(0.5, 0.73)];
	return [at(0.5, 0.5)];
}

/** How n pins lie on the table: rows of up to 5, face up. */
export function pinGrid(n: number): { cols: number; rows: number } {
	const cols = Math.max(1, Math.min(5, n));
	return { cols, rows: Math.max(1, Math.ceil(n / cols)) };
}

/** What the product page's 3D view says about a pin: "2 × 1 in · Gold metal and enamel". */
export const pinFacts = (widthIn: number | null, heightIn: number | null) =>
	`${widthIn != null && heightIn != null ? `${sizeLabel(widthIn, heightIn)} · ` : ''}Gold metal and enamel`;

/** "1 pin", "25 pins". */
export const pinLabel = (n: number) => `${Math.max(1, n)} pin${Math.max(1, n) === 1 ? '' : 's'}`;
