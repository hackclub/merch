import { describe, expect, it } from 'vitest';
import { cardFlip, postcardLabel, postcardStacks } from './postcards';

describe('postcardStacks', () => {
	it('bundles every 50 and leaves the rest loose', () => {
		expect(postcardStacks(15, 6, 4).stacks.map((s) => [s.cards, s.band])).toEqual([[15, false]]);
		expect(postcardStacks(50, 6, 4).stacks.map((s) => [s.cards, s.band])).toEqual([[50, true]]);
		expect(postcardStacks(150, 6, 4).stacks.map((s) => [s.cards, s.band])).toEqual([[50, true], [50, true], [50, true]]);
		expect(postcardStacks(0, 6, 4).stacks.map((s) => s.cards)).toEqual([1]);
	});

	it('lays stacks out side by side, three across, centred', () => {
		const { stacks, width, depth } = postcardStacks(200, 6, 4);
		expect(stacks).toHaveLength(4);
		expect(new Set(stacks.slice(0, 3).map((s) => s.z)).size).toBe(1);
		expect(stacks[3].z).toBeGreaterThan(stacks[0].z);
		expect(stacks[0].x - 3).toBeCloseTo(-width / 2);
		expect(depth).toBeGreaterThan(8);
	});
});

describe('postcardLabel', () => {
	it('says how they come', () => {
		expect(postcardLabel(1)).toBe('1 postcard');
		expect(postcardLabel(15)).toBe('15 postcards');
		expect(postcardLabel(50)).toBe('50 postcards · a bundle of 50');
		expect(postcardLabel(150)).toBe('150 postcards · 3 bundles of 50');
	});
});

describe('cardFlip', () => {
	it('turns the loose stack over about its long axis, front up at 0 and back up at 1', () => {
		expect(cardFlip(0, 4)).toEqual({ rotX: 0, lift: 0 });
		const over = cardFlip(1, 4);
		expect(over.rotX).toBeCloseTo(Math.PI);
		expect(over.lift).toBeCloseTo(0);
		expect(cardFlip(-1, 4)).toEqual(cardFlip(0, 4));
		expect(cardFlip(2, 4).rotX).toBeCloseTo(Math.PI);
	});

	it('lifts the stack off the table while it turns, so no edge drags along it', () => {
		for (const h of [0.016, 0.08, 0.8]) {
			for (let p = 0.1; p < 0.95; p += 0.05) {
				const { rotX, lift } = cardFlip(p, 4);
				// The turned box's lowest point, measured from where its bottom rested.
				const lowest = h / 2 + lift - ((4 / 2) * Math.abs(Math.sin(rotX)) + (h / 2) * Math.abs(Math.cos(rotX)));
				expect(lowest).toBeGreaterThan(0.1);
			}
			const mid = cardFlip(0.5, 4);
			expect(mid.lift - 2).toBeGreaterThanOrEqual(0.5); // held up off the table, half an inch at least
		}
	});
});
