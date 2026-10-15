import type { Address } from './types';

export const blankAddress = (): Address => ({
	first_name: '', last_name: '', line_1: '', line_2: '', city: '', state: '', postal_code: '', country: 'US', phone_number: ''
});

/**
 * Fill empty fields from the person's Hack Club Auth address. Anything already
 * typed wins. The default "US" country counts as empty.
 */
export function prefillAddress(current: Address, from: Address | null | undefined): Address {
	if (!from) return current;
	const out = { ...current };
	const blank = blankAddress();
	for (const k of Object.keys(out) as (keyof Address)[]) {
		const untouched = out[k].trim() === '' || (k === 'country' && out[k] === blank.country);
		if (untouched && from[k]) out[k] = from[k];
	}
	return out;
}

const filled = (s: string) => s.trim() !== '';

/** Whether an address has everything we need to ship to it (as the server checks), so the form can fold it away. */
export const addressComplete = (a: Address): boolean =>
	[a.first_name, a.last_name, a.line_1, a.city, a.postal_code].every(filled) &&
	/^[A-Za-z]{2}$/.test(a.country.trim()) &&
	(a.country.trim().toUpperCase() === 'US' || filled(a.phone_number));

const regions = new Intl.DisplayNames(['en'], { type: 'region' });

/** The address as mailing-label lines: name, street, "City, ST 12345", country, then phone if any. */
export function addressSummary(a: Address): string[] {
	const code = a.country.trim().toUpperCase();
	const place = [a.city.trim() + (filled(a.state) ? `, ${a.state.trim()}` : ''), a.postal_code.trim()].filter(filled).join(' ');
	return [
		`${a.first_name.trim()} ${a.last_name.trim()}`,
		[a.line_1, a.line_2].filter(filled).map((s) => s.trim()).join(', '),
		place,
		/^[A-Z]{2}$/.test(code) ? (regions.of(code) ?? code) : code,
		a.phone_number.trim()
	].filter(filled);
}
