import { describe, expect, it } from 'vitest';
import { blockerMessage, problemItemId, submitBlockedProps, submitBlockers } from './submit';

const ready = { understood: true, customsOK: true, intl: false, nudge: false, regroupOK: false, problems: [] as string[], quoteError: '' };

describe('submitBlockers', () => {
	it('is empty when the request can go', () => {
		expect(submitBlockers(ready)).toEqual([]);
		expect(submitBlockers({ ...ready, intl: true })).toEqual([]);
	});

	it('names each blocker on its own', () => {
		expect(submitBlockers({ ...ready, understood: false })).toEqual(['understood']);
		expect(submitBlockers({ ...ready, intl: true, customsOK: false })).toEqual(['customs']);
		expect(submitBlockers({ ...ready, nudge: true })).toEqual(['sticker_regroup']);
		expect(submitBlockers({ ...ready, problems: ['Pick a size for T-shirt.'] })).toEqual(['cart_problem']);
		expect(submitBlockers({ ...ready, quoteError: 'Over 3 lb.' })).toEqual(['quote_error']);
	});

	it('only asks for customs abroad, and only nudges about stickers while the note shows', () => {
		expect(submitBlockers({ ...ready, customsOK: false })).toEqual([]); // US
		expect(submitBlockers({ ...ready, nudge: true, regroupOK: true })).toEqual([]);
		expect(submitBlockers({ ...ready, regroupOK: false })).toEqual([]); // no nudge showing
	});

	it('keeps the declared order when several apply, so the first is what to focus', () => {
		expect(
			submitBlockers({ understood: false, customsOK: false, intl: true, nudge: true, regroupOK: false, problems: ['x'], quoteError: 'y' })
		).toEqual(['understood', 'customs', 'sticker_regroup', 'cart_problem', 'quote_error']);
	});
});

describe('blockerMessage', () => {
	it('says what to do, using the cart or quote problem itself when there is one', () => {
		expect(blockerMessage('understood', [], '')).toMatch(/I understand/);
		expect(blockerMessage('customs', [], '')).toMatch(/customs/);
		expect(blockerMessage('sticker_regroup', [], '')).toMatch(/Stickers/);
		expect(blockerMessage('cart_problem', ['Pick a size for T-shirt.'], '')).toBe('Pick a size for T-shirt.');
		expect(blockerMessage('quote_error', [], 'Over 3 lb.')).toBe('Over 3 lb.');
	});
});

describe('problemItemId', () => {
	const items = [
		{ id: 3, name: 'T-shirt' },
		{ id: 7, name: 'Hack Club enamel pin' }
	];
	it('finds the item a cart problem names, so a blocked submit can scroll to its line', () => {
		expect(problemItemId(['Pick a size for T-shirt.'], items)).toBe(3);
		expect(problemItemId(['Hack Club enamel pin: only 2 left.'], items)).toBe(7);
		expect(problemItemId(['Pick a size for T-shirt.', 'Hack Club enamel pin: out of stock.'], items)).toBe(3);
	});

	it('has no line to scroll to for problems that name no item', () => {
		expect(problemItemId(['Pick at least one item.'], items)).toBeNull();
		expect(problemItemId([], items)).toBeNull();
	});
});

describe('submitBlockedProps', () => {
	it('logs which form, why, and how far they got — never anything they typed', () => {
		expect(submitBlockedProps('request', ['understood', 'quote_error'], { intl: true, items: 4, signedIn: false })).toEqual({
			form: 'request',
			reasons: ['understood', 'quote_error'],
			intl: true,
			items: 4,
			signed_in: false
		});
		expect(Object.keys(submitBlockedProps('print', ['art_missing'], { intl: false, items: 0, signedIn: true }))).toEqual([
			'form',
			'reasons',
			'intl',
			'items',
			'signed_in'
		]);
	});
});
