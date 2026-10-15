import { describe, expect, it } from 'vitest';
import { clampPan, groundDelta, rotatePan, zoomToward } from './haulview';

describe('groundDelta', () => {
	it('turns a screen drag into a move on the table, stretching the foreshortened depth', () => {
		const d = groundDelta(10, 20, 0.5, Math.PI / 6); // sin 30° = 0.5
		expect(d.x).toBeCloseTo(5);
		expect(d.z).toBeCloseTo(20);
	});
	it('never stretches depth past 1/0.3, looking almost flat across the table', () => {
		expect(groundDelta(0, 10, 1, 0.01).z).toBeCloseTo(10 / 0.3);
	});
});

describe('zoomToward', () => {
	it('keeps the spot under the cursor still while zooming in', () => {
		// The cursor is 10 in right of the view's centre; zooming in to half the distance moves the centre halfway there.
		expect(zoomToward({ x: 0, z: 0 }, { x: 10, z: -4 }, 1, 0.5)).toEqual({ x: 5, z: -2 });
	});
	it('moves the centre away from the cursor when zooming out', () => {
		// The spot 8 in right of centre ends up twice as far, 16 in, from the new centre.
		expect(zoomToward({ x: 2, z: 0 }, { x: 10, z: 0 }, 0.5, 1)).toEqual({ x: -6, z: 0 });
	});
});

describe('rotatePan', () => {
	it('turns the view centre with the table, so the same spot stays in the middle', () => {
		const p = rotatePan({ x: 10, z: 0 }, Math.PI / 2);
		expect(p.x).toBeCloseTo(0);
		expect(p.z).toBeCloseTo(-10);
	});
});

describe('clampPan', () => {
	it('keeps the view centre within reach of the table', () => {
		expect(clampPan({ x: 50, z: -3 }, 20)).toEqual({ x: 20, z: -3 });
		expect(clampPan({ x: -50, z: 40 }, 20)).toEqual({ x: -20, z: 20 });
	});
});
