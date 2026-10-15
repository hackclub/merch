import type { Address } from './types';

/** One address the server's Google Places proxy suggests. */
export type AddressSuggestion = { place_id: string; main: string; secondary: string };

/** Matches the server's minimum; shorter queries match too much to be worth a call. */
export const wantsSearch = (q: string) => [...q.trim()].length >= 4;

/**
 * Fill the address fields from a picked place. The name and phone stay, and a
 * field the place lacks keeps what was typed. Line 2 always follows the place,
 * so an old apartment number doesn't stick to a new street.
 */
export function applyPlace(current: Address, place: Address): Address {
	const out = { ...current };
	for (const k of ['line_1', 'city', 'state', 'postal_code', 'country'] as const) {
		if (place[k]) out[k] = place[k];
	}
	out.line_2 = place.line_2;
	return out;
}

/** Next highlighted row for arrow keys; -1 means none. */
export function moveHighlight(index: number, delta: number, length: number): number {
	if (length === 0) return -1;
	if (index < 0) return delta > 0 ? 0 : length - 1;
	return (index + delta + length) % length;
}

/**
 * Debounce lookups and ignore stale answers. Each call resolves to the
 * suggestions, or null when a later call replaced it.
 */
export function createSearcher(fetcher: (q: string) => Promise<AddressSuggestion[]>, delay = 250) {
	let seq = 0;
	let timer: ReturnType<typeof setTimeout> | undefined;
	let cancelPending: (() => void) | undefined;
	return (q: string): Promise<AddressSuggestion[] | null> => {
		const mine = ++seq;
		clearTimeout(timer);
		cancelPending?.();
		cancelPending = undefined;
		if (!wantsSearch(q)) return Promise.resolve([]);
		return new Promise((resolve, reject) => {
			cancelPending = () => resolve(null);
			timer = setTimeout(() => {
				cancelPending = undefined;
				fetcher(q.trim()).then((r) => resolve(mine === seq ? r : null), (e) => (mine === seq ? reject(e) : resolve(null)));
			}, delay);
		});
	};
}

/** A random token grouping one address search into one billed Places session. */
export const newSessionToken = () => crypto.randomUUID();

/** The region ("GB") of a browser language ("en-GB", or "ja" → "JP"), to bias address suggestions; "" if unknown. */
export function regionOf(lang: string): string {
	try {
		return new Intl.Locale(lang).maximize().region ?? '';
	} catch {
		return '';
	}
}
