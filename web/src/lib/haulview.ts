// The math behind steering the 3D haul (Haul3D.svelte): where the view is centred on the
// table (pan, inches, in the scene's axes: x right, z toward the camera) as people drag,
// scroll, pinch and turn it. The camera always looks along -z, so screen right is +x.

export type Pan = { x: number; z: number };

/** A drag of dx, dy CSS pixels as a move on the tabletop, at `scale` inches per pixel, seen from `elevation` radians. */
export function groundDelta(dx: number, dy: number, scale: number, elevation: number): Pan {
	return { x: dx * scale, z: (dy * scale) / Math.max(0.3, Math.sin(elevation)) };
}

/** The new view centre after zooming from z0 to z1 toward `at`, so the spot under the cursor stays put. */
export function zoomToward(pan: Pan, at: Pan, z0: number, z1: number): Pan {
	const k = 1 - z1 / z0;
	return { x: pan.x + (at.x - pan.x) * k, z: pan.z + (at.z - pan.z) * k };
}

/** The view centre after the table turns by `by` radians (as group.rotation.y does), so it stays on the same spot. */
export function rotatePan(pan: Pan, by: number): Pan {
	const c = Math.cos(by);
	const s = Math.sin(by);
	return { x: pan.x * c + pan.z * s, z: -pan.x * s + pan.z * c };
}

/** Keeps the view centre within `reach` inches of the middle of the table each way. */
export function clampPan(pan: Pan, reach: number): Pan {
	const c = (v: number) => Math.min(reach, Math.max(-reach, v));
	return { x: c(pan.x), z: c(pan.z) };
}
