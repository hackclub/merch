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
