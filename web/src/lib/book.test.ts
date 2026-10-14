import { describe, expect, it } from 'vitest';
import { bookState, leafAngle, pagesNear, settle, spreadLabel, spreadPages } from './book';

describe('spreadPages', () => {
	// The mini-magazine: 12 pages on 6 leaves, so spreads 0 (closed on the cover) to 6 (closed on the back).
	it('shows the cover alone, then facing pages, then the back cover alone', () => {
		expect(spreadPages(0, 12)).toEqual({ left: null, right: 0 });
		expect(spreadPages(1, 12)).toEqual({ left: 1, right: 2 });
		expect(spreadPages(5, 12)).toEqual({ left: 9, right: 10 });
		expect(spreadPages(6, 12)).toEqual({ left: 11, right: null });
	});

	it('leaves the last right page blank when the page count is odd', () => {
		expect(spreadPages(1, 3)).toEqual({ left: 1, right: 2 });
		expect(spreadPages(2, 3)).toEqual({ left: null, right: null });
	});
});

describe('spreadLabel', () => {
	it('names covers and page numbers people can find in the print', () => {
		expect(spreadLabel(0, 144)).toBe('Front cover');
		expect(spreadLabel(1, 144)).toBe('Pages 2–3 of 144');
		expect(spreadLabel(72, 144)).toBe('Back cover');
	});
});

describe('bookState', () => {
	it('turns one leaf at a time, stacking turned leaves on the left', () => {
		expect(bookState(0, 6)).toEqual({ leaf: 0, turn: 0, left: 0, right: 5 });
		expect(bookState(2.25, 6)).toEqual({ leaf: 2, turn: 0.25, left: 2, right: 3 });
		expect(bookState(6, 6)).toEqual({ leaf: 5, turn: 1, left: 5, right: 0 });
	});

	it('clamps positions past either cover', () => {
		expect(bookState(-1, 6)).toEqual(bookState(0, 6));
		expect(bookState(9, 6)).toEqual(bookState(6, 6));
	});
});

describe('leafAngle', () => {
	it('lies flat at rest and leads with the outer edge while turning', () => {
		expect(leafAngle(0, 1)).toBe(0);
		expect(leafAngle(1, 0)).toBeCloseTo(Math.PI);
		expect(leafAngle(1, 1)).toBeCloseTo(Math.PI);
		expect(leafAngle(0.3, 1)).toBeGreaterThan(leafAngle(0.3, 0));
		for (let s = 0; s <= 1; s += 0.1) {
			expect(leafAngle(0.5, s)).toBeGreaterThanOrEqual(0);
			expect(leafAngle(0.5, s)).toBeLessThanOrEqual(Math.PI);
		}
	});
});

describe('pagesNear', () => {
	it('loads the covers plus a few pages either side of where you are', () => {
		expect(pagesNear(0, 144)).toEqual([0, 1, 2, 3, 4, 143]);
		expect(pagesNear(10.5, 144)).toEqual([0, 17, 18, 19, 20, 21, 22, 23, 24, 143]);
		expect(pagesNear(72, 144)).toEqual([0, 139, 140, 141, 142, 143]);
	});
});

describe('settle', () => {
	it('finishes a turn once it is a fifth of the way over in the drag direction', () => {
		expect(settle(3.25, 1)).toBe(4);
		expect(settle(3.1, 1)).toBe(3);
		expect(settle(3.75, -1)).toBe(3);
		expect(settle(3.9, -1)).toBe(4);
		expect(settle(3.6, 0)).toBe(4);
	});
});
