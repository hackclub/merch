import { describe, expect, it } from 'vitest';
import { coverCrop, finishInfo, sheetThickness } from './paper';

describe('coverCrop', () => {
	it('keeps art that already matches the sheet', () => {
		expect(coverCrop(11 / 17, 11 / 17)).toEqual({ repeatX: 1, repeatY: 1, offsetX: 0, offsetY: 0 });
	});

	it('trims top and bottom when the sheet is wider than the art, like a print shop', () => {
		// The Hackpad art is 11x17 (0.647); the card is 4x6 (0.667).
		const c = coverCrop(11 / 17, 4 / 6);
		expect(c.repeatX).toBe(1);
		expect(c.repeatY).toBeCloseTo(0.9706, 4);
		expect(c.offsetY).toBeCloseTo((1 - c.repeatY) / 2, 6);
	});

	it('trims the sides when the sheet is narrower than the art', () => {
		const c = coverCrop(1, 0.5);
		expect(c).toEqual({ repeatX: 0.5, repeatY: 1, offsetX: 0.25, offsetY: 0 });
	});
});

describe('finishInfo', () => {
	it('describes each paper and how it reflects light', () => {
		expect(finishInfo('linen').label).toBe('Linen card stock');
		expect(finishInfo('gloss').roughness).toBeLessThan(finishInfo('matte').roughness);
		expect(finishInfo('').label).toBe('');
	});
});

describe('sheetThickness', () => {
	it('uses the recorded thickness, or typical paper', () => {
		expect(sheetThickness({ thickness_in: 0.016 })).toBe(0.016);
		expect(sheetThickness({ thickness_in: null })).toBe(0.008);
	});
});
