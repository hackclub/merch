import { describe, expect, it } from 'vitest';
import { arrivalText, costBreakdown, costRows, costToYou, worthText, dateRange, ozText, spanText, summaryText, type Costs, type ShippingEstimate } from './shipping';

const est = (over: Partial<ShippingEstimate>): ShippingEstimate => ({
	package: 'Mailer',
	weight_oz: 8,
	class: 'parcel',
	service: 'Asendia',
	handling_days: [1, 2],
	transit_days: [7, 12],
	arrive_from: '2026-10-21',
	arrive_by: '2026-10-28',
	postage_cents: [0, 0],
	postage_typical_cents: 0,
	fee_cents: 0,
	transit_basis: 'GB',
	transit_samples: 40,
	postage_basis: '',
	postage_samples: 0,
	handling_samples: 100,
	...over
});

describe('shipping', () => {
	it('formats date ranges', () => {
		expect(dateRange('2026-10-21', '2026-10-28')).toBe('Oct 21–28');
		expect(dateRange('2026-10-30', '2026-11-04')).toBe('Oct 30 – Nov 4');
		expect(dateRange('2026-10-21', '2026-10-21')).toBe('Oct 21');
	});

	it('says when it arrives, or nothing without history', () => {
		expect(arrivalText(est({}))).toBe('Arrives about Oct 21–28');
		expect(arrivalText(est({ arrive_from: '', arrive_by: '' }))).toBe('');
		expect(arrivalText(null)).toBe('');
	});

	it('speaks in business days, then weeks', () => {
		expect(spanText([4, 8])).toBe('4–8 business days');
		expect(spanText([1, 1])).toBe('1 business day');
		expect(spanText([9, 25])).toBe('2–5 weeks');
		// Up to 17 business days is over 3 weeks: round the far end up, like days.
		expect(spanText([9, 17])).toBe('2–4 weeks');
		expect(spanText([10, 15])).toBe('2–3 weeks');
		expect(spanText([13, 15])).toBe('3 weeks');
	});

	it('summarizes time in the mail, after the next-day shipping it follows', () => {
		const us = est({ handling_days: [0, 1], transit_days: [4, 7] });
		const intl = est({ handling_days: [0, 2], transit_days: [9, 23] });
		expect(summaryText({ us, international: intl })).toBe('4–7 day delivery in the US. 2–5 weeks globally.');
		expect(summaryText({ us: est({ arrive_by: '' }), international: intl })).toBe('');
	});

	it('shows weights', () => {
		expect(ozText(3.25)).toBe('3.3 oz');
		expect(ozText(45.2)).toBe('2.8 lb');
	});
});

describe('costRows', () => {
	const c = (over: Partial<Costs>): Costs => ({
		items_cents: 945, labor_cents: 260, packing_cents: 50, postage_cents: 512, total_cents: 1767, unpriced_skus: [], ...over
	});
	const both = { us: c({}), international: c({ postage_cents: 1400, total_cents: 2655 }) };
	it('shows only the side the request ships to, labor and packing together', () => {
		expect(costRows(both, false)).toEqual([
			['Items', '$9.45 USD'],
			['Labor + packing', '$3.10 USD'],
			['Postage (estimated)', '$5.12 USD'],
			['Total', '$17.67 USD']
		]);
		expect(costRows(both, true)).toEqual([
			['Items', '$9.45 USD'],
			['Labor + packing', '$3.10 USD'],
			['Postage (estimated)', '$14.00 USD'],
			['Total', '$26.55 USD']
		]);
	});
	it('marks what it has no figure for', () => {
		const rows = costRows({ us: c({ postage_cents: 0, unpriced_skus: ['Sti/New/1st'] }), international: null }, false);
		expect(rows[0]).toEqual(['Items', '$9.45 USD + 1 unpriced']);
		expect(rows[2]).toEqual(['Postage (estimated)', 'no history']);
		expect(rows[3]).toEqual(['Total', '$17.67+ USD']);
		expect(costRows({ us: c({}), international: null }, true)).toEqual([
			['Items', '—'],
			['Labor + packing', '—'],
			['Postage (estimated)', '—'],
			['Total', 'over 3 lb']
		]);
	});
});

describe('worthText', () => {
	const c = (total_cents: number): Costs => ({
		items_cents: 0, labor_cents: 0, packing_cents: 0, postage_cents: 100, total_cents, unpriced_skus: []
	});
	it('says the approved hours that make it worth it under $30, at $5 an hour', () => {
		expect(worthText(c(2900))).toBe('If you get 1 teen to join Hack Club and ship 6 hours of work, then this is worth it!');
		expect(worthText(c(320))).toBe('If you get 1 teen to join Hack Club and ship 1 hour of work, then this is worth it!');
		expect(worthText(c(1767))).toBe('If you get 1 teen to join Hack Club and ship 4 hours of work, then this is worth it!');
	});
	it('counts teens who each ship a weighted project from $30, rounded up', () => {
		expect(worthText(c(3000))).toBe('If you get 1 teen to join Hack Club and ship a weighted project, then this is worth it!');
		expect(worthText(c(12000))).toBe('If you get 3 teens to join Hack Club and each ship a weighted project, then this is worth it!');
		expect(worthText(null)).toBe('');
	});
});

describe('costToYou', () => {
	it('is free in the US, and the shipping fee abroad', () => {
		expect(costToYou(false, 2149)).toBe('$0 USD');
		expect(costToYou(true, 2200)).toBe('$22.00 USD for shipping');
		expect(costToYou(true, 0)).toBe('$0 USD');
	});
});

describe('costBreakdown', () => {
	const c = (over: Partial<Costs>): Costs => ({
		items_cents: 945, labor_cents: 260, packing_cents: 50, postage_cents: 512, total_cents: 1767, unpriced_skus: [], ...over
	});
	it('lists materials, labor, packing, postage and the total for one request', () => {
		expect(costBreakdown(c({}))).toEqual([
			['Materials', '$9.45'],
			['Labor', '$2.60'],
			['Packing', '$0.50'],
			['Postage (est.)', '$5.12'],
			['Total', '$17.67']
		]);
	});
	it('marks what it has no figure for', () => {
		const rows = costBreakdown(c({ postage_cents: 0, unpriced_skus: ['Sti/New/1st'] }));
		expect(rows[0]).toEqual(['Materials', '$9.45 + Sti/New/1st unpriced']);
		expect(rows[3]).toEqual(['Postage (est.)', 'no history']);
		expect(rows[4]).toEqual(['Total', '$17.67+']);
	});
});
