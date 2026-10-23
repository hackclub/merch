// Free looking around a 3D view (Product3D's `free` mode, the print request
// form): the wheel and pinch zoom smoothly, the buttons step, and dragging up
// or down swings the camera over or under the print.

/** Zoom stops for the + and − buttons: 1 is the whole scene, smaller is closer. */
export const ZOOMS = [1, 0.5, 0.22];
/** As close as the wheel or a pinch goes: near enough to see the paper's grain. */
export const MAX_ZOOM_IN = 0.15;
/** How far the camera swings over (+) or under (−) the print, radians. */
export const TILT = { min: -0.6, max: 1.3 };

const clamp = (n: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, n));
const clampZoom = (z: number) => clamp(z, MAX_ZOOM_IN, 1);

/** The next button stop in (dir 1) or out (dir −1) from wherever the zoom is. */
export function zoomStep(zoom: number, dir: 1 | -1): number {
	const eps = 1e-6;
	if (dir > 0) return ZOOMS.find((z) => z < zoom - eps) ?? ZOOMS[ZOOMS.length - 1];
	return [...ZOOMS].reverse().find((z) => z > zoom + eps) ?? ZOOMS[0];
}

/** A wheel turn: down (positive deltaY) zooms out, up zooms in, in proportion. */
export const wheelZoom = (zoom: number, deltaY: number) => clampZoom(zoom * Math.exp(deltaY * 0.0015));

/** Two fingers that started `from` px apart and are now `to` apart. */
export const pinchZoom = (zoom: number, from: number, to: number) => clampZoom((zoom * from) / to);

/** A vertical drag of dy px (down is positive) from tilt. */
export const tiltBy = (tilt: number, dy: number) => clamp(tilt - dy * 0.008, TILT.min, TILT.max);

/** The camera's offset from what it looks at, swung over the top by tilt radians (about the x axis). */
export function orbitOffset(o: { x: number; y: number; z: number }, tilt: number) {
	const [c, s] = [Math.cos(tilt), Math.sin(tilt)];
	return { x: o.x, y: o.y * c + o.z * s, z: o.z * c - o.y * s };
}

/** True the first time each gesture is used (per view), for one view3d_interact event each. */
export function firstUse() {
	const seen = new Set<string>();
	return (how: string) => !seen.has(how) && !!seen.add(how);
}
