// Shipping estimates from the server (internal/swag/estimate.go): when a
// package should arrive, read off how similar packages went before.

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

// Order to doorstep, in days: handling plus transit.
export const totalDays = (e: ShippingEstimate): [number, number] => [
	e.handling_days[0] + e.transit_days[0],
	e.handling_days[1] + e.transit_days[1]
];

// "3–6 days", or "2–4 weeks" past two weeks.
export function spanText([lo, hi]: [number, number]): string {
	if (hi > 14) {
		const w = (n: number) => Math.max(1, Math.round(n / 7));
		return w(lo) === w(hi) ? `${w(hi)} weeks` : `${w(lo)}–${w(hi)} weeks`;
	}
	return lo === hi ? `${hi} day${hi === 1 ? '' : 's'}` : `${lo}–${hi} days`;
}

// The shop intro's delivery line, or '' without history.
export function summaryText(s: ShippingSummary | null): string {
	if (!s?.us.arrive_by || !s.international.arrive_by) return '';
	return `Usually arrives in ${spanText(totalDays(s.us))} in the US, ${spanText(totalDays(s.international))} globally.`;
}

export const ozText = (oz: number) => (oz >= 16 ? `${(oz / 16).toFixed(1)} lb` : `${oz.toFixed(1)} oz`);
