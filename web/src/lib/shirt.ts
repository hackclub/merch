// A T-shirt folded the way shops fold them: sleeves and sides tucked behind,
// the hem folded under, so the top shows the chest from the collar down. This is
// the shape (pure, for tests); shirt3d.ts builds the mesh. Units are inches; x
// runs left to right, z from the collar (-d/2) to the folded hem (+d/2).
import type { Box } from './stickers';

/**
 * Where the chest sits in a centred, front-on product shot (fractions of the
 * photo), for when we can't find the shirt's outline in it: the Hack Club tee's.
 */
export const SHIRT_PHOTO = { u0: 0.32, v0: 0.06, w: 0.36 };

// The shoulders slope down from the collar: each back corner is cut this much across (of w) and down (of d).
const SHOULDER = { across: 0.2, down: 0.08 };
// The crew neck: outer half-width (of w), its back and front from the back edge (of d), and the rib's width.
const NECK = { half: 0.31, back: 0.033, front: 0.23 };
const RIB = 0.55;

/** Signed distance from the shirt's outline, inches: positive inside. */
function outline(x: number, z: number, w: number, d: number): number {
	const a = SHOULDER.across * w, s = SHOULDER.down * d;
	const pts: [number, number][] = [
		[-w / 2, d / 2], [w / 2, d / 2], [w / 2, -d / 2 + s], [w / 2 - a, -d / 2], [-w / 2 + a, -d / 2], [-w / 2, -d / 2 + s]
	];
	let min = Infinity;
	for (let i = 0; i < pts.length; i++) {
		const [x1, z1] = pts[i];
		const [x2, z2] = pts[(i + 1) % pts.length];
		const ex = x2 - x1, ez = z2 - z1;
		// Going round the points in this order, the inside is on the positive side of each edge.
		min = Math.min(min, (ez * (x - x1) - ex * (z - z1)) / Math.hypot(ex, ez));
	}
	return min;
}

const smooth = (a: number, b: number, v: number) => {
	const k = Math.min(1, Math.max(0, (v - a) / (b - a)));
	return k * k * (3 - 2 * k);
};

/**
 * The top of one folded shirt t thick at (x, z): its height, how much to darken
 * it (1 = not at all; the inside of the neck is in shadow), and whether the point
 * is on the shirt at all. The folds round over to the table on every side.
 */
export function foldedShirtTop(x: number, z: number, w: number, d: number, t: number): { y: number; shade: number; inside: boolean } {
	const dist = outline(x, z, w, d);
	if (dist <= 0) return { y: 0, shade: 1, inside: dist >= -1e-9 };
	const e = Math.min(dist / (0.75 * t), 1);
	const round = Math.sqrt(1 - (1 - e) ** 2);
	// A little fuller in the middle, with a soft ripple in the cloth.
	const bulge = (1 - (2 * x / w) ** 2) * (1 - (2 * z / d) ** 2);
	const ripple = 0.012 * Math.sin(x * 1.3 + z * 0.4) * Math.sin(z * 0.9 - x * 0.2);
	let y = t * round * (0.94 + 0.06 * bulge) + ripple * e;
	let shade = 1;

	const A = NECK.half * w;
	const zb = -d / 2 + NECK.back * d, zf = -d / 2 + NECK.front * d;
	const zo = (zb + zf) / 2, B = (zf - zb) / 2;
	const outer = Math.hypot(x / A, (z - zo) / B);
	const inner = Math.hypot(x / (A - RIB), (z - zo) / Math.max(0.2, B - RIB * 0.9));
	if (inner < 1) {
		// Down into the neck opening, onto the inside of the back.
		const k = smooth(1, 0.75, inner);
		y *= 1 - 0.45 * k;
		shade = 1 - 0.4 * k;
	} else if (outer < 1) {
		// Across the rib, from its outer edge (0) to the opening (1): a rounded ridge.
		const f = (1 - outer) / (1 - outer + (inner - 1));
		y += 0.14 * Math.sin(Math.PI * f) ** 0.7 * e;
		shade = 0.96;
	}
	return { y, shade, inside: true };
}

/**
 * Which part of a shirt's product photo the folded top shows (fractions of the
 * photo, v down from the top): the middle of the chest from just above the
 * collar, as wide as about half the shirt with its sleeves, at the fold's
 * aspect. box is the shirt in an mw × mh mask of the photo (maskBounds), null if
 * not found; imgAspect and footAspect are width over height of the photo and
 * the folded shirt.
 */
export function shirtCrop(box: Box | null, mw: number, mh: number, imgAspect: number, footAspect: number) {
	const h = (w: number) => (w * imgAspect) / footAspect;
	if (!box) return { ...SHIRT_PHOTO, h: h(SHIRT_PHOTO.w) };
	const w = 0.49 * (box.w / mw);
	return { u0: (box.x + box.w / 2) / mw - w / 2, v0: box.y / mh - 0.015, w, h: h(w) };
}

/**
 * Chest width (armpit to armpit) and body length (shoulder to hem) in inches of
 * a typical unisex tee in each size; sizes we don't know are shown as an M.
 */
const SHIRT_SIZES: Record<string, { chest: number; length: number }> = {
	XS: { chest: 16.5, length: 27 },
	S: { chest: 18, length: 28 },
	M: { chest: 20, length: 29 },
	L: { chest: 22, length: 30 },
	XL: { chest: 24, length: 31 },
	XXL: { chest: 26, length: 32 },
	'2XL': { chest: 26, length: 32 }
};

export const shirtSize = (label: string) => SHIRT_SIZES[label.trim().toUpperCase()] ?? SHIRT_SIZES.M;

/**
 * Whether a photo is a product shot of the shirt alone: transparent around the
 * edges, or nearly every pixel on its border the plain backdrop of its corners.
 * An event photo (people wearing it) isn't, so it can't be the shirt's back.
 */
export function isProductShot(rgba: Uint8ClampedArray, w: number, h: number): boolean {
	const corners = [0, w - 1, (h - 1) * w, h * w - 1];
	const avg = (c: number) => corners.reduce((a, i) => a + rgba[i * 4 + c], 0) / corners.length;
	const bg = [avg(0), avg(1), avg(2)];
	const plain = (i: number) => {
		if (rgba[i * 4 + 3] < 128) return true;
		const r = rgba[i * 4], g = rgba[i * 4 + 1], b = rgba[i * 4 + 2];
		return Math.max(r, g, b) - Math.min(r, g, b) < 24 && Math.abs(r - bg[0]) + Math.abs(g - bg[1]) + Math.abs(b - bg[2]) < 60;
	};
	let n = 0, ok = 0;
	const check = (i: number) => (n++, (ok += plain(i) ? 1 : 0));
	for (let x = 0; x < w; x++) check(x), check((h - 1) * w + x);
	for (let y = 1; y < h - 1; y++) check(y * w), check(y * w + w - 1);
	return ok >= n * 0.9;
}

/**
 * How far each pixel of a mask is from its nearest pixel outside it (or the
 * image's edge), in pixels: 0 outside. Two chamfer passes, close to Euclidean.
 */
export function edgeDistance(mask: Uint8Array, w: number, h: number): Float32Array {
	const d = new Float32Array(w * h);
	for (let i = 0; i < d.length; i++) d[i] = mask[i] ? Infinity : 0;
	const at = (x: number, y: number) => (x < 0 || y < 0 || x >= w || y >= h ? 0 : d[y * w + x]);
	const D = Math.SQRT2;
	for (let y = 0; y < h; y++)
		for (let x = 0; x < w; x++) {
			const i = y * w + x;
			if (d[i]) d[i] = Math.min(d[i], at(x - 1, y) + 1, at(x, y - 1) + 1, at(x - 1, y - 1) + D, at(x + 1, y - 1) + D);
		}
	for (let y = h - 1; y >= 0; y--)
		for (let x = w - 1; x >= 0; x--) {
			const i = y * w + x;
			if (d[i]) d[i] = Math.min(d[i], at(x + 1, y) + 1, at(x, y + 1) + 1, at(x + 1, y + 1) + D, at(x - 1, y + 1) + D);
		}
	return d;
}

/** How far each pixel is from the shirt's outline, in pixels: positive on the shirt (edgeDistance), negative off it. */
export function signedEdgeDistance(mask: Uint8Array, w: number, h: number): Float32Array {
	const inside = edgeDistance(mask, w, h);
	const outside = edgeDistance(mask.map((m) => (m ? 0 : 1)), w, h);
	return inside.map((d, i) => (mask[i] ? d : -outside[i]));
}

/**
 * The 3D shirt's edge runs this many pixels in from the outline in its photo,
 * where the backdrop no longer bleeds into the cloth. The photo is solid up to
 * there and fades over the two pixels outside it, so the edge is never see-through
 * and alphaTest trims anything past it smoothly instead of in pixel steps.
 */
export const CUT_PX = 3;
export const cutAlpha = (fromEdge: number) => Math.min(1, Math.max(0, (fromEdge - CUT_PX + 2) / 2));

/** sd at (x, y), between pixels. */
function sample(sd: Float32Array, w: number, h: number, x: number, y: number): number {
	x = Math.min(w - 1, Math.max(0, x));
	y = Math.min(h - 1, Math.max(0, y));
	const x0 = Math.min(w - 2, Math.floor(x)), y0 = Math.min(h - 2, Math.floor(y));
	const fx = x - x0, fy = y - y0;
	const at = (i: number, j: number) => sd[(y0 + j) * w + x0 + i];
	return (at(0, 0) * (1 - fx) + at(1, 0) * fx) * (1 - fy) + (at(0, 1) * (1 - fx) + at(1, 1) * fx) * fy;
}

/**
 * Where the cut line is nearest a point near it, on or off the shirt: the point
 * moved straight in or out along the distance field. The 3D shirt puts its edge
 * vertices there, so its front and back meet along the trimmed edge.
 */
export function onCut(sd: Float32Array, w: number, h: number, x: number, y: number): [number, number] {
	for (let k = 0; k < 4; k++) {
		const gx = (sample(sd, w, h, x + 1, y) - sample(sd, w, h, x - 1, y)) / 2;
		const gy = (sample(sd, w, h, x, y + 1) - sample(sd, w, h, x, y - 1)) / 2;
		const g = Math.hypot(gx, gy);
		if (g < 1e-6) break;
		const step = (CUT_PX - sample(sd, w, h, x, y)) / g;
		if (Math.abs(step) < 0.05) break;
		x += (gx / g) * step;
		y += (gy / g) * step;
	}
	return [x, y];
}

/** How far cloth stands out from its outline, inches: rounding over within `round` of the edge, up to `depth`. */
export function clothDepth(fromEdge: number, round: number, depth: number): number {
	const e = Math.min(Math.max(fromEdge, 0) / round, 1);
	return depth * Math.sqrt(1 - (1 - e) ** 2);
}

/**
 * The shirt's own color, for the parts of it no photo shows: the median of the
 * cloth just inside its outline (prints sit in the middle), as [r, g, b].
 */
export function fabricColor(rgba: Uint8ClampedArray, mask: Uint8Array, dist: Float32Array, w: number): [number, number, number] {
	const far = Math.max(3, w * 0.04);
	const ch: number[][] = [[], [], []];
	for (let i = 0; i < mask.length; i++) {
		if (!mask[i] || dist[i] < 1.5 || dist[i] > far) continue;
		for (let c = 0; c < 3; c++) ch[c].push(rgba[i * 4 + c]);
	}
	if (!ch[0].length) return [43, 44, 49];
	const median = (v: number[]) => v.sort((a, b) => a - b)[Math.floor(v.length / 2)];
	return [median(ch[0]), median(ch[1]), median(ch[2])];
}
