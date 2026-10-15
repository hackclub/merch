import { describe, expect, it } from 'vitest';
import { addressComplete, addressSummary, blankAddress, prefillAddress } from './address';

describe('prefillAddress', () => {
	const hca = {
		first_name: 'Orpheus', last_name: 'Dino', line_1: '15 Falls Rd', line_2: 'Apt 2', city: 'Shelburne',
		state: 'VT', postal_code: '05482', country: 'US', phone_number: '+18025550199'
	};

	it('fills a blank form from the Hack Club Auth address', () => {
		expect(prefillAddress(blankAddress(), hca)).toEqual(hca);
	});

	it('never overwrites what the person already typed', () => {
		const typed = { ...blankAddress(), line_1: '1 Other St', city: 'Burlington' };
		const out = prefillAddress(typed, hca);
		expect(out.line_1).toBe('1 Other St');
		expect(out.city).toBe('Burlington');
		expect(out.first_name).toBe('Orpheus');
	});

	it('treats the default US country as blank so a non-US address can prefill it', () => {
		expect(prefillAddress(blankAddress(), { ...hca, country: 'GB' }).country).toBe('GB');
	});

	it('is a no-op without an address', () => {
		expect(prefillAddress(blankAddress(), undefined)).toEqual(blankAddress());
	});
});

describe('addressComplete', () => {
	const full = { ...blankAddress(), first_name: 'Orpheus', last_name: 'Dino', line_1: '15 Falls Rd', city: 'Shelburne', state: 'VT', postal_code: '05482' };

	it('needs a name, street, city, postal code and a 2-letter country', () => {
		expect(addressComplete(full)).toBe(true);
		for (const k of ['first_name', 'last_name', 'line_1', 'city', 'postal_code'] as const) expect(addressComplete({ ...full, [k]: ' ' })).toBe(false);
		expect(addressComplete({ ...full, country: 'USA' })).toBe(false);
		expect(addressComplete({ ...full, state: '', line_2: '' })).toBe(true);
	});

	it('needs a phone number outside the US, for customs', () => {
		const ca = { ...full, country: 'CA', state: 'ON', postal_code: 'M5V 2T6' };
		expect(addressComplete(ca)).toBe(false);
		expect(addressComplete({ ...ca, phone_number: '+1 416 555 0100' })).toBe(true);
	});
});

describe('addressSummary', () => {
	it('reads like a mailing label', () => {
		const a = { ...blankAddress(), first_name: 'Orpheus', last_name: 'Dino', line_1: '15 Falls Rd', line_2: 'Apt 2', city: 'Shelburne', state: 'VT', postal_code: '05482' };
		expect(addressSummary(a)).toEqual(['Orpheus Dino', '15 Falls Rd, Apt 2', 'Shelburne, VT 05482', 'United States']);
		expect(addressSummary({ ...a, line_2: '', state: '', country: 'gb', phone_number: '+44 20 7946 0000' })).toEqual(['Orpheus Dino', '15 Falls Rd', 'Shelburne 05482', 'United Kingdom', '+44 20 7946 0000']);
	});
});
