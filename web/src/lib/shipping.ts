// Shipping estimates from the server (internal/swag/estimate.go): when a
// package should arrive, read off how similar packages went before.

import { formatCents } from './api';

export type ShippingEstimate = {
	package: string;
	weight_oz: number; // packed
	class: 'letter' | 'parcel' | '';
	service: string;
	handling_days: [number, number];
	transit_days: [number, number];
	arrive_from: string; // YYYY-MM-DD, '' without history
	arrive_by: string;
	postage_cents: [number, number]; // admins only; [0, 0] otherwise
	postage_typical_cents: number; // admins only
	fee_cents: number; // what the requester pays: typical postage abroad, rounded up to a dollar; 0 in the US
	transit_basis: string;
	transit_samples: number;
	postage_basis: string;
	postage_samples: number;
	handling_samples: number;
};

export type ShippingSummary = { us: ShippingEstimate; international: ShippingEstimate };

// weight_oz includes the packaging; contents_oz doesn't.
export type Packing = { package: { name: string; class: string }; weight_oz: number; contents_oz: number; unknown_weight: boolean };

const day = (iso: string) => new Date(`${iso}T12:00:00`);
const md = (d: Date) => d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });

// "Oct 21–28", "Oct 30 – Nov 4", or "Oct 21" when both ends are the same day.
export function dateRange(from: string, by: string): string {
	const a = day(from);
	const b = day(by);
	if (from === by) return md(a);
	if (a.getMonth() === b.getMonth()) return `${md(a)}–${b.getDate()}`;
	return `${md(a)} – ${md(b)}`;
}

// "Arrives about Oct 21–28", or '' when there's no history to go on.
export function arrivalText(e: ShippingEstimate | null | undefined): string {
	if (!e?.arrive_from || !e.arrive_by) return '';
	return `Arrives about ${dateRange(e.arrive_from, e.arrive_by)}`;
}

// Business days in words: "3–6 business days", or "2–5 weeks" past two weeks.
export function spanText([lo, hi]: [number, number]): string {
	if (hi > 10) {
		// Rounded outward at the far end, like the days: 17 business days is "4 weeks".
		const a = Math.max(1, Math.round(lo / 5));
		const b = Math.max(a, Math.ceil(hi / 5));
		return a === b ? `${b} weeks` : `${a}–${b} weeks`;
	}
	return lo === hi ? `${hi} business day${hi === 1 ? '' : 's'}` : `${lo}–${hi} business days`;
}

// The shop intro's delivery line, after "Shipped next day Mon–Fri.": time in
// the mail (business days), or '' without history.
export function summaryText(s: ShippingSummary | null): string {
	if (!s?.us.arrive_by || !s.international.arrive_by) return '';
	const [lo, hi] = s.us.transit_days;
	const us = lo === hi ? `${hi} day` : `${lo}–${hi} day`;
	return `${us} delivery in the US. ${spanText(s.international.transit_days)} globally.`;
}

export const ozText = (oz: number) => (oz >= 16 ? `${(oz / 16).toFixed(1)} lb` : `${oz.toFixed(1)} oz`);

// What a request costs Hack Club to send (internal/swag/costs.go).
export type Costs = {
	items_cents: number; // at the warehouse's unit costs, leaving out unpriced_skus
	labor_cents: number; // AGH: $1.80 an order + $0.20 a SKU
	packing_cents: number;
	postage_cents: number; // typical postage; 0 without history
	total_cents: number;
	unpriced_skus: string[];
};
// null when it can't ship that way (over 3 lb abroad).
export type CostSides = { us: Costs | null; international: Costs | null };

// The cost table's rows: [label, US, abroad].
export function costRows(c: CostSides): [string, string, string][] {
	const side = (x: Costs | null, f: (x: Costs) => string) => (x ? f(x) : '—');
	const items = (x: Costs) => formatCents(x.items_cents) + (x.unpriced_skus.length ? ` + ${x.unpriced_skus.length} unpriced` : '');
	const postage = (x: Costs) => (x.postage_cents ? formatCents(x.postage_cents) : 'no history');
	// A total missing an item's cost or the postage is a floor.
	const total = (x: Costs) => formatCents(x.total_cents) + (x.unpriced_skus.length || !x.postage_cents ? '+' : '');
	const row = (label: string, f: (x: Costs) => string): [string, string, string] => [label, side(c.us, f), side(c.international, f)];
	return [
		row('Items', items),
		row('Labor + packing', (x) => formatCents(x.labor_cents + x.packing_cents)),
		row('Postage (estimated)', postage),
		['Total', side(c.us, total), c.international ? total(c.international) : 'over 3 lb']
	];
}

// Hack Club's marketing goal: spend at most $50 per new teenager who joins and ships a weighted
// project (10 approved hours, so $5 an hour) in one of our programs.
export const CENTS_PER_NEW_TEEN = 5000;
const CENTS_PER_HOUR = CENTS_PER_NEW_TEEN / 10;

// What a request has to bring in to be worth its cost: under $30, the approved hours one new
// teen ships; from $30, how many new teens each ship a weighted project. Both round up.
export function worthText(x: Costs | null): string {
	if (!x) return '';
	if (x.total_cents < 3000) {
		const h = Math.max(1, Math.ceil(x.total_cents / CENTS_PER_HOUR));
		return `If you get 1 teen to join Hack Club and ship ${h} hour${h === 1 ? '' : 's'} of work, then this is worth it!`;
	}
	const n = Math.ceil(x.total_cents / CENTS_PER_NEW_TEEN);
	return n === 1
		? 'If you get 1 teen to join Hack Club and ship a weighted project, then this is worth it!'
		: `If you get ${n} teens to join Hack Club and each ship a weighted project, then this is worth it!`;
}

// What the requester pays: nothing in the US, the shipping fee abroad.
export function costToYou(intl: boolean, feeCents: number): string {
	return intl && feeCents > 0 ? `${formatCents(feeCents)} for shipping` : '$0';
}
