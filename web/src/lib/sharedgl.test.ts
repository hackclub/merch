import { describe, expect, it } from 'vitest';
import { grow, sourceRect } from './sharedgl';

describe('grow', () => {
	it('only ever grows the shared canvas, to fit the largest view drawn on it', () => {
		expect(grow({ w: 0, h: 0 }, { w: 300, h: 200 })).toEqual({ w: 300, h: 200 });
		expect(grow({ w: 300, h: 200 }, { w: 250, h: 260 })).toEqual({ w: 300, h: 260 });
		expect(grow({ w: 300, h: 260 }, { w: 100, h: 100 })).toEqual({ w: 300, h: 260 });
	});
});

describe('sourceRect', () => {
	it('finds a view drawn from the bottom-left corner (where GL draws) in the canvas image (top-left origin)', () => {
		expect(sourceRect(600, 400, 300)).toEqual([0, 300, 400, 300]);
		expect(sourceRect(300, 400, 300)).toEqual([0, 0, 400, 300]);
	});
});
