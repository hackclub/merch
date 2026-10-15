import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { applyPlace, createSearcher, moveHighlight, wantsSearch, regionOf } from './addressSearch';
import { blankAddress } from './address';

describe('applyPlace', () => {
	const typed = { ...blankAddress(), first_name: 'Orpheus', last_name: 'Dino', line_1: '15 fal', line_2: 'Old apt', phone_number: '+18025550199' };
	const place = { ...blankAddress(), line_1: '15 Falls Rd', city: 'Shelburne', state: 'VT', postal_code: '05482', country: 'US' };

	it('fills the address but keeps the name and phone', () => {
		expect(applyPlace(typed, place)).toEqual({ ...place, first_name: 'Orpheus', last_name: 'Dino', phone_number: '+18025550199', line_2: '' });
	});

	it('takes the apartment from the place when it has one', () => {
		expect(applyPlace(typed, { ...place, line_2: 'Apt 2' }).line_2).toBe('Apt 2');
	});

	it('keeps what was typed when the place lacks a field', () => {
		expect(applyPlace(typed, { ...place, line_1: '' }).line_1).toBe('15 fal');
	});
});

describe('wantsSearch', () => {
	it('waits for a few characters', () => {
		expect(wantsSearch('15 ')).toBe(false);
		expect(wantsSearch('15 F')).toBe(true);
	});
});

describe('moveHighlight', () => {
	it('wraps around the list', () => {
		expect(moveHighlight(-1, 1, 3)).toBe(0);
		expect(moveHighlight(2, 1, 3)).toBe(0);
		expect(moveHighlight(0, -1, 3)).toBe(2);
		expect(moveHighlight(-1, -1, 3)).toBe(2);
		expect(moveHighlight(0, 1, 0)).toBe(-1);
	});
});

describe('createSearcher', () => {
	beforeEach(() => vi.useFakeTimers());
	afterEach(() => vi.useRealTimers());

	it('debounces keystrokes into one call', async () => {
		const fetcher = vi.fn(async (q: string) => [{ place_id: q, main: q, secondary: '' }]);
		const search = createSearcher(fetcher, 200);
		const a = search('15 F');
		const b = search('15 Fa');
		const c = search('15 Fal');
		await vi.advanceTimersByTimeAsync(200);
		expect(fetcher).toHaveBeenCalledTimes(1);
		expect(fetcher).toHaveBeenCalledWith('15 Fal');
		expect(await a).toBeNull();
		expect(await b).toBeNull();
		expect((await c)?.[0].place_id).toBe('15 Fal');
	});

	it('drops a slow answer once a newer query has gone out', async () => {
		let release: (v: { place_id: string; main: string; secondary: string }[]) => void = () => {};
		const fetcher = vi.fn((q: string) =>
			q === 'slow' ? new Promise<{ place_id: string; main: string; secondary: string }[]>((r) => (release = r)) : Promise.resolve([{ place_id: q, main: q, secondary: '' }])
		);
		const search = createSearcher(fetcher, 10);
		const slow = search('slow');
		await vi.advanceTimersByTimeAsync(10);
		const fast = search('fast');
		await vi.advanceTimersByTimeAsync(10);
		release([{ place_id: 'slow', main: 'slow', secondary: '' }]);
		expect(await slow).toBeNull();
		expect((await fast)?.[0].place_id).toBe('fast');
	});

	it('answers short queries with nothing, without calling out', async () => {
		const fetcher = vi.fn(async () => []);
		const search = createSearcher(fetcher, 10);
		expect(await search('15')).toEqual([]);
		expect(fetcher).not.toHaveBeenCalled();
	});
});

describe('regionOf', () => {
	it("reads the region from the browser's language", () => {
		expect(regionOf('en-GB')).toBe('GB');
		expect(regionOf('pt-BR')).toBe('BR');
		// A bare language gets its most likely region.
		expect(regionOf('ja')).toBe('JP');
		expect(regionOf('not a locale!')).toBe('');
	});
});
