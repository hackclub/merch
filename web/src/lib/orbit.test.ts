import { describe, expect, it } from 'vitest';
import { MAX_ZOOM_IN, TILT, firstUse, orbitOffset, pinchZoom, tiltBy, wheelZoom, zoomStep } from './orbit';

describe('zoomStep (the + and − buttons)', () => {
	it('steps through whole view, half and close up', () => {
		expect(zoomStep(1, 1)).toBe(0.5);
		expect(zoomStep(0.5, 1)).toBe(0.22);
		expect(zoomStep(0.22, 1)).toBe(0.22);
		expect(zoomStep(0.22, -1)).toBe(0.5);
		expect(zoomStep(0.5, -1)).toBe(1);
		expect(zoomStep(1, -1)).toBe(1);
	});
	it('steps from wherever the wheel left it to the next stop', () => {
		expect(zoomStep(0.7, 1)).toBe(0.5);
		expect(zoomStep(0.7, -1)).toBe(1);
		expect(zoomStep(0.3, 1)).toBe(0.22);
	});
});

describe('wheelZoom', () => {
	it('scrolling down (positive deltaY) zooms out, up zooms in, smoothly', () => {
		expect(wheelZoom(0.5, 100)).toBeGreaterThan(0.5);
		expect(wheelZoom(0.5, -100)).toBeLessThan(0.5);
		expect(wheelZoom(0.5, -1)).toBeCloseTo(0.5, 2);
	});
	it('stops at the whole view and close up', () => {
		expect(wheelZoom(1, 5000)).toBe(1);
		expect(wheelZoom(0.2, -5000)).toBe(MAX_ZOOM_IN);
	});
});

describe('pinchZoom', () => {
	it('spreading two fingers zooms in by how far they spread', () => {
		expect(pinchZoom(1, 100, 200)).toBe(0.5);
		expect(pinchZoom(0.5, 200, 100)).toBe(1);
		expect(pinchZoom(0.5, 100, 10_000)).toBe(MAX_ZOOM_IN);
	});
});

describe('tiltBy', () => {
	it('dragging up looks from above, down from below, within limits', () => {
		expect(tiltBy(0, -50)).toBeGreaterThan(0);
		expect(tiltBy(0, 50)).toBeLessThan(0);
		expect(tiltBy(0, -10_000)).toBe(TILT.max);
		expect(tiltBy(0, 10_000)).toBe(TILT.min);
	});
});

describe('orbitOffset', () => {
	it('keeps the camera where it was with no tilt', () => {
		const o = orbitOffset({ x: 1, y: 2, z: 10 }, 0);
		expect(o).toEqual({ x: 1, y: 2, z: 10 });
	});
	it('swings the camera over the top at the same distance', () => {
		const o = orbitOffset({ x: 0, y: 0, z: 10 }, Math.PI / 4);
		expect(o.y).toBeCloseTo(7.071, 2);
		expect(o.z).toBeCloseTo(7.071, 2);
		expect(Math.hypot(o.x, o.y, o.z)).toBeCloseTo(10);
	});
});

describe('firstUse', () => {
	it('says yes once per gesture, so view3d_interact logs each gesture once per view', () => {
		const first = firstUse();
		expect(first('wheel')).toBe(true);
		expect(first('wheel')).toBe(false);
		expect(first('tilt')).toBe(true);
		expect(firstUse()('wheel')).toBe(true);
	});
});
