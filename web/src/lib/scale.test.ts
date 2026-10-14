import { describe, expect, it } from 'vitest';
import { PHONE, hasSize, pixelsPerInch, sizeLabel } from './scale';

describe('sizeLabel', () => {
	it('gives inches and centimeters', () => {
		expect(sizeLabel(11, 17)).toBe('11 × 17 in (28 × 43 cm)');
		expect(sizeLabel(4, 6)).toBe('4 × 6 in (10 × 15 cm)');
		expect(sizeLabel(8.5, 11)).toBe('8.5 × 11 in (22 × 28 cm)');
	});
});

describe('hasSize', () => {
	it('needs both dimensions', () => {
		expect(hasSize({ width_in: 11, height_in: 17 })).toBe(true);
		expect(hasSize({ width_in: null, height_in: 17 })).toBe(false);
		expect(hasSize({ width_in: null, height_in: null })).toBe(false);
	});
});

describe('pixelsPerInch', () => {
	// The sheet and the phone stand side by side on one floor at the same scale.
	it('fits a big poster and the phone into the stage', () => {
		const ppi = pixelsPerInch({ w: 11, h: 17 }, { w: 600, h: 500 });
		expect(17 * ppi).toBeLessThanOrEqual(500 * 0.8);
		expect((11 + PHONE.w + 2) * ppi).toBeLessThanOrEqual(600 * 0.8);
	});

	it('lets a small card grow, but never smaller than the phone allows', () => {
		const ppi = pixelsPerInch({ w: 4, h: 6 }, { w: 600, h: 500 });
		expect(PHONE.h * ppi).toBeLessThanOrEqual(500 * 0.8);
		expect(ppi).toBeGreaterThan(pixelsPerInch({ w: 11, h: 17 }, { w: 600, h: 500 }));
	});

	it('is limited by width on a narrow stage', () => {
		const ppi = pixelsPerInch({ w: 11, h: 17 }, { w: 300, h: 900 });
		expect((11 + PHONE.w + 2) * ppi).toBeCloseTo(300 * 0.8);
	});
});
