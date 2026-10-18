import { describe, expect, it } from 'vitest';
import { bars, filterRequests, offsetText, shippedBreakdown, statusCounts, type DaySpend } from './dashboard';
import type { SwagRequest } from './types';

const req = (over: Partial<SwagRequest>): SwagRequest => ({
	id: 1, user_id: 1, user_email: 'orpheus@hackclub.com', status: 'pending', purpose: 'personal',
	address: { first_name: 'Orpheus', last_name: 'Dino', line_1: '15 Falls Rd', city: 'Shelburne', state: 'VT', postal_code: '05482', country: 'US' },
	shipping_fee_cents: 0, paid_at: null, note: '', admin_note: '', theseus_order_id: '', tracking_number: '', carrier: '',
	mailed_at: null, source: 'app', created_at: '2026-10-17T12:00:00Z',
	lines: [{ item_id: 1, name: 'Mini-Magazine', quantity: 2, sku: 'Pri/Bok/Mini25/1st', variant: '' }],
	...over
}) as SwagRequest;

const all = [
	req({ id: 1 }),
	req({ id: 2, status: 'awaiting_payment', address: { ...req({}).address, city: 'Berlin', state: '', country: 'DE' }, created_at: '2026-10-16T12:00:00Z' }),
	req({ id: 3, status: 'dispatched', user_email: 'a@x.com', tracking_number: '9400111', created_at: '2026-10-15T12:00:00Z' }),
	req({ id: 4, status: 'dispatched', source: 'fillout', purpose: '', created_at: '2026-09-01T12:00:00Z',
		lines: [{ item_id: 2, name: 'Sticker', quantity: 5, sku: 'Sti/Bra/O&H/Lap', variant: '' }] })
];

describe('filterRequests', () => {
	const f = (over = {}) => filterRequests(all, { status: '', q: '', region: '', source: '', purpose: '', sort: 'newest', ...over }).map((r) => r.id);
	it('filters by status, with "open" meaning anything still to act on', () => {
		expect(f({ status: 'dispatched' })).toEqual([3, 4]);
		expect(f({ status: 'open' })).toEqual([1, 2]);
		expect(f()).toEqual([1, 2, 3, 4]);
	});
	it('searches ids, people, places, items and tracking', () => {
		expect(f({ q: '#3' })).toEqual([3]);
		expect(f({ q: 'a@x' })).toEqual([3]);
		expect(f({ q: 'berlin' })).toEqual([2]);
		expect(f({ q: 'sti/bra' })).toEqual([4]);
		expect(f({ q: 'mini-mag' })).toEqual([1, 2, 3]);
		expect(f({ q: '9400111' })).toEqual([3]);
		expect(f({ q: 'dino berlin' })).toEqual([2]); // every word matches somewhere
	});
	it('filters by region, source and purpose, and sorts', () => {
		expect(f({ region: 'intl' })).toEqual([2]);
		expect(f({ region: 'us' })).toEqual([1, 3, 4]);
		expect(f({ source: 'imported' })).toEqual([4]);
		expect(f({ source: 'app' })).toEqual([1, 2, 3]);
		expect(f({ purpose: 'personal' })).toEqual([1, 2, 3]);
		expect(f({ sort: 'oldest' })).toEqual([4, 3, 2, 1]);
	});
});

it('counts requests by status, and the open ones', () => {
	expect(statusCounts(all)).toEqual({ '': 4, open: 2, pending: 1, awaiting_payment: 1, dispatched: 2, rejected: 0, cancelled: 0 });
});

it('stacks each day by kind of cost, scaled to the biggest day', () => {
	const day = (d: string, items: number, labor: number, packing: number, postage: number): DaySpend =>
		({ day: d, requests: 1, items_cents: items, labor_cents: labor, packing_cents: packing, postage_cents: postage, total_cents: items + labor + packing + postage });
	const b = bars([day('2026-10-16', 100, 100, 0, 200), day('2026-10-17', 0, 0, 0, 0)], 100);
	expect(b[0].segments.map((s) => [s.kind, s.y, s.h])).toEqual([['items', 75, 25], ['labor', 50, 25], ['packing', 50, 0], ['postage', 0, 50]]);
	expect(b[1].segments.every((s) => s.h === 0)).toBe(true);
	expect(b[0].title).toBe('Oct 16: $4.00 over 1 request (items $1.00, labor $1.00, packing $0.00, postage $2.00)');
	expect(bars([day('2026-10-17', 0, 0, 0, 0)], 100)[0].segments[0].h).toBe(0);
});

it('says whether international payments cover their postage', () => {
	const o = { requests: 4, collected_cents: 5000, postage_cents: 4000, postage_actual: 1, other_cents: 2000, net_cents: 1000 };
	expect(offsetText(o)).toBe('Covered: 4 requests paid $50.00 for $40.00 of postage (125%), $10.00 over.');
	expect(offsetText({ ...o, collected_cents: 3000, net_cents: -1000 })).toBe('Short: 4 requests paid $30.00 for $40.00 of postage (75%), $10.00 under.');
	expect(offsetText({ ...o, requests: 0, collected_cents: 0, postage_cents: 0, net_cents: 0 })).toBe('No international requests shipped in this window.');
});

it("marks a shipped request's estimated costs", () => {
	const c = { items_cents: 1215, labor_cents: 300, packing_cents: 50, postage_cents: 471, total_cents: 2036, unpriced_skus: [], actual: ['items', 'labor'] };
	expect(shippedBreakdown(c)).toEqual([
		['Items', '$12.15'], ['Labor', '$3.00'], ['Packing (est.)', '$0.50'], ['Postage (est.)', '$4.71'], ['Total', '$20.36']
	]);
});
