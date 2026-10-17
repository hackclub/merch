import { describe, expect, it } from 'vitest';
import { backorderNote, countText } from './backorder';
import type { Item } from './types';

const item = (over: Partial<Item> = {}) => ({ id: 1, sku: 'Pri/Hav/4x6/1st', variants: [], ...over }) as Item;
const stock = {
	'Pri/Hav/4x6/1st': { in_stock: -503, inbound: 15000 },
	'Pri/Wel/4x6/1st': { in_stock: 900, inbound: 0 },
	'Sti/Gone': { in_stock: 0, inbound: 0 },
	'Swa/Shirt/HC': { in_stock: 300, inbound: 500 },
	'Swa/Shirt/HC/S': { in_stock: 0, inbound: 500 },
	'Swa/Shirt/HC/M': { in_stock: 300, inbound: 0 }
};

describe('backorders', () => {
	it('rounds big counts', () => {
		expect(countText(15000)).toBe('15k');
		expect(countText(1500)).toBe('1.5k');
		expect(countText(500)).toBe('500');
	});

	it('says what is inbound when the warehouse is short', () => {
		const note = 'We have 15k inbound to the Hack Club warehouse. Yours ship as soon as they arrive.';
		expect(backorderNote(item(), stock, {}, {})).toBe(note);
		expect(backorderNote(item(), stock, { 1: 25 }, {})).toBe(note);
		expect(backorderNote(item({ sku: 'Pri/Wel/4x6/1st' }), stock, { 1: 25 }, {})).toBe('');
		expect(backorderNote(item({ sku: 'Pri/Wel/4x6/1st' }), stock, { 1: 1000 }, {})).toBe(''); // short, but nothing coming
		expect(backorderNote(item({ sku: 'Sti/Gone' }), stock, {}, {})).toBe('');
		expect(backorderNote(item({ sku: 'Sti/Unknown' }), stock, {}, {})).toBe('');
		expect(backorderNote(item(), undefined, {}, {})).toBe(''); // warehouse down
	});

	it('looks at the sizes picked', () => {
		const shirt = item({ sku: 'Swa/Shirt/HC', variants: [{ label: 'S', sku: 'Swa/Shirt/HC/S' }, { label: 'M', sku: 'Swa/Shirt/HC/M' }] });
		expect(backorderNote(shirt, stock, {}, {})).toBe('');
		expect(backorderNote(shirt, stock, { 1: 1 }, { 1: { 'Swa/Shirt/HC/M': 1 } })).toBe('');
		expect(backorderNote(shirt, stock, { 1: 2 }, { 1: { 'Swa/Shirt/HC/S': 1, 'Swa/Shirt/HC/M': 1 } })).toBe(
			'We have 500 size S inbound to the Hack Club warehouse. Yours ship as soon as they arrive.'
		);
	});
});
