// The admin requests dashboard: spend by day, the month's projection, whether
// international payments cover their postage, and the request list's filters.
import { formatCents } from './api';
import type { Costs } from './shipping';
import type { SwagRequest } from './types';

/** What went out on one day (New York), by kind of cost (GET /api/admin/dashboard). */
export type DaySpend = {
	day: string; // YYYY-MM-DD
	requests: number;
	items_cents: number;
	labor_cents: number;
	packing_cents: number;
	postage_cents: number;
	total_cents: number;
	postage_actual?: number; // requests whose postage is the label's real price
};

/** International postage payments against what that postage cost. */
export type Offset = {
	country?: string;
	requests: number;
	collected_cents: number;
	postage_cents: number;
	postage_actual: number; // how many postage figures are the label's real price
	other_cents: number; // items, labor and packing for those packages
	net_cents: number; // collected − postage
};

export type Dashboard = {
	summary: {
		days: DaySpend[];
		totals: DaySpend;
		month: { month: string; spent_cents: number; requests: number; daily_rate_cents: number; days_left: number; projected_cents: number };
		international: Offset;
		by_country: Offset[];
	};
	/** Shipped requests in the window, by id: actual names the warehouse's own figures. */
	costs: Record<string, Costs & { actual: string[] }>;
	queue: { requests: number; total_cents: number };
};

export const KINDS = ['items', 'labor', 'packing', 'postage'] as const;
export type Kind = (typeof KINDS)[number];

export type Filter = {
	status: string; // '' = all, 'open' = pending or awaiting payment
	q: string;
	region: '' | 'us' | 'intl';
	source: '' | 'app' | 'imported';
	purpose: string;
	sort: 'newest' | 'oldest';
};

const OPEN = ['pending', 'awaiting_payment'];
const STATUSES = ['pending', 'awaiting_payment', 'dispatched', 'rejected', 'cancelled'];

const haystack = (r: SwagRequest) => {
	const a = r.address;
	return [
		`#${r.id}`, r.user_email, `${a.first_name} ${a.last_name}`, a.city, a.state, a.postal_code, a.country,
		r.tracking_number, r.theseus_order_id, ...r.lines.flatMap((l) => [l.name, l.sku])
	].join('\n').toLowerCase();
};

export function filterRequests(requests: SwagRequest[], f: Filter): SwagRequest[] {
	const words = f.q.toLowerCase().split(/\s+/).filter(Boolean);
	const out = requests.filter((r) => {
		if (f.status === 'open' ? !OPEN.includes(r.status) : f.status && r.status !== f.status) return false;
		const intl = r.address.country.trim().toUpperCase() !== 'US';
		if ((f.region === 'us' && intl) || (f.region === 'intl' && !intl)) return false;
		if ((f.source === 'app' && r.source !== 'app') || (f.source === 'imported' && r.source === 'app')) return false;
		if (f.purpose && r.purpose !== f.purpose) return false;
		if (!words.length) return true;
		const h = haystack(r);
		return words.every((w) => h.includes(w));
	});
	const dir = f.sort === 'oldest' ? 1 : -1;
	return out.sort((a, b) => dir * (Date.parse(a.created_at) - Date.parse(b.created_at)) || dir * (a.id - b.id));
}

export function statusCounts(requests: SwagRequest[]): Record<string, number> {
	const out: Record<string, number> = { '': requests.length, open: 0 };
	for (const s of STATUSES) out[s] = 0;
	for (const r of requests) {
		out[r.status] = (out[r.status] ?? 0) + 1;
		if (OPEN.includes(r.status)) out.open++;
	}
	return out;
}

const dayLabel = (day: string) =>
	new Date(`${day}T12:00:00Z`).toLocaleDateString('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' });

/** One day's stacked bar in a chart height tall: items at the bottom, postage on top. */
export function bars(days: DaySpend[], height: number) {
	const most = Math.max(1, ...days.map((d) => d.total_cents));
	return days.map((d) => {
		let y = height;
		const segments = KINDS.map((kind) => {
			const cents = d[`${kind}_cents`];
			const h = (cents / most) * height;
			y -= h;
			return { kind, cents, y, h };
		});
		const parts = KINDS.map((k) => `${k} ${formatCents(d[`${k}_cents`])}`).join(', ');
		const title = `${dayLabel(d.day)}: ${formatCents(d.total_cents)} over ${d.requests} request${d.requests === 1 ? '' : 's'} (${parts})`;
		return { day: d.day, label: dayLabel(d.day), total: d.total_cents, segments, title };
	});
}

export function offsetText(o: Offset): string {
	if (!o.requests) return 'No international requests shipped in this window.';
	const pct = o.postage_cents ? ` (${Math.round((o.collected_cents / o.postage_cents) * 100)}%)` : '';
	const n = `${o.requests} request${o.requests === 1 ? '' : 's'}`;
	const head = o.net_cents >= 0 ? 'Covered' : 'Short';
	const tail = o.net_cents >= 0 ? `${formatCents(o.net_cents)} over` : `${formatCents(-o.net_cents)} under`;
	return `${head}: ${n} paid ${formatCents(o.collected_cents)} for ${formatCents(o.postage_cents)} of postage${pct}, ${tail}.`;
}

/** A shipped request's cost rows, marking what's estimated rather than mail.hackclub.com's own charge. */
export function shippedBreakdown(c: Costs & { actual: string[] }): [string, string][] {
	const est = (kind: string) => (c.actual.includes(kind) ? '' : ' (est.)');
	const unpriced = c.unpriced_skus.length ? ` + ${c.unpriced_skus.length} unpriced` : '';
	return [
		[`Items${est('items')}`, formatCents(c.items_cents) + unpriced],
		[`Labor${est('labor')}`, formatCents(c.labor_cents)],
		['Packing (est.)', formatCents(c.packing_cents)],
		[`Postage${est('postage')}`, c.postage_cents ? formatCents(c.postage_cents) : 'no history'],
		['Total', formatCents(c.total_cents)]
	];
}
