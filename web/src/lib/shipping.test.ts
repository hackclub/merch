import { describe, expect, it } from 'vitest';
import { arrivalText, dateRange, ozText, spanText, summaryText, type ShippingEstimate } from './shipping';

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

	it('speaks in days, then weeks', () => {
		expect(spanText([4, 7])).toBe('4–7 days');
		expect(spanText([1, 1])).toBe('1 day');
		expect(spanText([9, 25])).toBe('1–4 weeks');
		expect(spanText([14, 15])).toBe('2 weeks');
	});

	it('summarizes the US and abroad', () => {
		const us = est({ handling_days: [0, 2], transit_days: [4, 7] });
		const intl = est({ handling_days: [1, 2], transit_days: [11, 26] });
		expect(summaryText({ us, international: intl })).toBe('Usually arrives in 4–9 days in the US, 2–4 weeks globally.');
		expect(summaryText({ us: est({ arrive_by: '' }), international: intl })).toBe('');
	});

	it('shows weights', () => {
		expect(ozText(3.25)).toBe('3.3 oz');
		expect(ozText(45.2)).toBe('2.8 lb');
	});
});
