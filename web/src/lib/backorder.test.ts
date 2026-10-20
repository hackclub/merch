import { describe, expect, it } from 'vitest';
import { backorderNote, countText, shortNote, soldOut, stockErrors } from './backorder';
import type { Item } from './types';

const item = (over: Partial<Item> = {}) => ({ id: 1, name: 'Haven Postcard', sku: 'Pri/Hav/4x6/1st', variants: [], allow_backorder: true, ...over }) as Item;
const stock = {
	'Pri/Hav/4x6/1st': { in_stock: -503, inbound: 15000 },
	'Pri/Wel/4x6/1st': { in_stock: 900, inbound: 0 },
	'Sti/Gone': { in_stock: 0, inbound: 0 },
	'Swa/HC/Pin/1st': { in_stock: 2, inbound: 0 },
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
		const note = '15k are inbound to Hack Club HQ. Your package will ship on arrival.';
		expect(backorderNote(item(), stock, {}, {})).toBe(note);
		expect(backorderNote(item(), stock, { 1: 25 }, {})).toBe(note);
		expect(backorderNote(item({ sku: 'Pri/Wel/4x6/1st' }), stock, { 1: 25 }, {})).toBe('');
		expect(backorderNote(item({ sku: 'Pri/Wel/4x6/1st' }), stock, { 1: 1000 }, {})).toBe(''); // short, but nothing coming
		expect(backorderNote(item({ sku: 'Sti/Gone' }), stock, {}, {})).toBe('');
		expect(backorderNote(item({ sku: 'Sti/Unknown' }), stock, {}, {})).toBe('');
		expect(backorderNote(item(), undefined, {}, {})).toBe(''); // warehouse down
		expect(backorderNote(item({ allow_backorder: false }), stock, {}, {})).toBe(''); // can't be backordered: it's out of stock instead
	});

	it('looks at the sizes picked', () => {
		const shirt = item({ sku: 'Swa/Shirt/HC', variants: [{ label: 'S', sku: 'Swa/Shirt/HC/S' }, { label: 'M', sku: 'Swa/Shirt/HC/M' }] });
		expect(backorderNote(shirt, stock, {}, {})).toBe('');
		expect(backorderNote(shirt, stock, { 1: 1 }, { 1: { 'Swa/Shirt/HC/M': 1 } })).toBe('');
		expect(backorderNote(shirt, stock, { 1: 2 }, { 1: { 'Swa/Shirt/HC/S': 1, 'Swa/Shirt/HC/M': 1 } })).toBe(
			'500 size S are inbound to Hack Club HQ. Your package will ship on arrival.'
		);
	});

	it('says what is left of items that cannot be backordered', () => {
		const pin = item({ name: 'Hack Club enamel pin', sku: 'Swa/HC/Pin/1st', allow_backorder: false });
		expect(shortNote(pin, stock, {}, {})).toBe('');
		expect(shortNote(pin, stock, { 1: 2 }, {})).toBe('');
		expect(shortNote(pin, stock, { 1: 3 }, {})).toBe('Only 2 left.');
		expect(shortNote(item({ allow_backorder: false }), stock, {}, {})).toBe('Out of stock.');
		expect(shortNote(item(), stock, { 1: 3 }, {})).toBe(''); // backordered instead
		expect(shortNote(item({ sku: 'Sti/Unknown', allow_backorder: false }), stock, {}, {})).toBe('Out of stock.'); // the warehouse omits what it's out of
		expect(shortNote(pin, undefined, { 1: 3 }, {})).toBe(''); // warehouse down
		const shirt = item({ name: 'T-shirt', sku: 'Swa/Shirt/HC', allow_backorder: false, variants: [{ label: 'S', sku: 'Swa/Shirt/HC/S' }, { label: 'M', sku: 'Swa/Shirt/HC/M' }] });
		expect(shortNote(shirt, stock, {}, {})).toBe('');
		expect(shortNote(shirt, stock, { 1: 1 }, { 1: { 'Swa/Shirt/HC/S': 1 } })).toBe('Size S is out of stock.');

		expect(soldOut(item({ allow_backorder: false }), stock)).toBe(true);
		expect(soldOut(item(), stock)).toBe(false);
		expect(soldOut(pin, stock)).toBe(false);
		expect(soldOut(shirt, stock)).toBe(false); // M is in
		expect(soldOut(shirt, { 'Swa/Shirt/HC/S': { in_stock: 0, inbound: 0 } })).toBe(true);
		expect(soldOut(pin, undefined)).toBe(false);

		expect(stockErrors({ 1: 3 }, [pin], {}, stock)).toEqual(['Hack Club enamel pin: only 2 left.']);
		expect(stockErrors({ 1: 2 }, [pin], {}, stock)).toEqual([]);
	});
});
