import { describe, expect, it } from 'vitest';
import { modelGrid, modelLabel, modelSize } from './model';

describe('3D models', () => {
	it('use the measured size, or a plush toy\'s', () => {
		// Deep enough for a tail that reaches back as far as it's tall, so it never pokes into the next pile.
		expect(modelSize({ width_in: 7, height_in: 11 })).toEqual({ w: 7, h: 11, d: 11 });
		expect(modelSize({ width_in: null, height_in: null })).toEqual({ w: 8, h: 10, d: 10 });
		expect(modelSize({ width_in: null, height_in: 12.5 })).toEqual({ w: 10, h: 12.5, d: 12.5 });
	});

	it('sit three to a row', () => {
		expect(modelGrid(1)).toEqual({ cols: 1, rows: 1 });
		expect(modelGrid(3)).toEqual({ cols: 3, rows: 1 });
		expect(modelGrid(5)).toEqual({ cols: 3, rows: 2 });
	});

	it('are labeled by how tall they stand', () => {
		expect(modelLabel(10)).toBe('10 in tall (25 cm)');
		expect(modelLabel(null)).toBe('About 10 in tall (25 cm)');
	});
});
