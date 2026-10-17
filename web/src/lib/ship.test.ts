import { describe, expect, it } from 'vitest';
import { lineText, shipBody } from './ship';

const lines = [
	{ item_id: 1, sku: 'Sti/A', name: 'Sticker', quantity: 3 },
	{ item_id: 2, sku: 'Swa/Shirt/M', name: 'Shirt', variant: 'M', quantity: 1 }
];

describe('shipBody', () => {
	it('sends only the quantities the admin changed, and the note', () => {
		expect(shipBody(lines, { 'Sti/A': 3, 'Swa/Shirt/M': 0 }, ' Out of shirts. ')).toEqual({
			lines: [{ sku: 'Swa/Shirt/M', quantity: 0 }],
			note: 'Out of shirts.'
		});
	});
	it('ships as asked with no edits', () => {
		expect(shipBody(lines, {}, '')).toEqual({ lines: [], note: '' });
	});
});

describe('lineText', () => {
	it('says what shipped and what was asked for', () => {
		expect(lineText(lines[1])).toBe('1 × Shirt (M)');
		expect(lineText({ ...lines[0], quantity: 2, requested_quantity: 3 })).toBe('2 × Sticker (you asked for 3)');
		expect(lineText({ ...lines[0], quantity: 0, requested_quantity: 3 })).toBe('Sticker: not sent (you asked for 3)');
	});
});
