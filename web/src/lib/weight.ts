// The 3 lb limit abroad, shown while people pick. The server packs the request
// (internal/swag/packing.go) and is the only judge of its weight: a quote's
// packing gives the weight and limit, and the 3 lb refusal (a 422) gives how far
// over it is and what each line weighs. Nothing here packs anything.
import type { Packing } from './shipping';

// One request line's weight, what it ships with included.
export type LineWeight = { item_id: number; sku?: string; name: string; quantity: number; oz: number };

// The estimate's 422 over 3 lb abroad.
export type TooHeavy = {
	error: string;
	weight_oz: number; // packed
	limit_oz: number;
	tare_oz: number;
	contents_oz: number;
	package: string;
	lines: LineWeight[]; // heaviest first
};

export type WeightMeter = {
	weightOz: number;
	limitOz: number;
	pct: number; // of the limit, up to 100
	over: boolean;
	overOz: number;
	heaviest: LineWeight[]; // when over, the 3 heaviest lines
	lines: number; // when over, how many lines the request has
	text: string;
	heaviestText: string;
};

/** The 3 lb refusal's body, or null for any other error. */
export function tooHeavy(data: unknown): TooHeavy | null {
	const d = data as TooHeavy | undefined;
	return d && typeof d === 'object' && typeof d.weight_oz === 'number' && typeof d.limit_oz === 'number' && Array.isArray(d.lines) ? d : null;
}

// "3 lb", "1.9 lb", "4.8 oz".
const weight = (oz: number) => (oz >= 16 ? `${+(oz / 16).toFixed(1)} lb` : `${+oz.toFixed(1)} oz`);

/** The meter for a quote's packing or the 3 lb refusal; null when there's no limit (the US) or nothing to go on. */
export function weightMeter(packing: Packing | null | undefined, heavy: TooHeavy | null): WeightMeter | null {
	const limit = weight(heavy?.limit_oz ?? packing?.limit_oz ?? 0);
	if (heavy) {
		const heaviest = heavy.lines.slice(0, 3);
		const overOz = heavy.weight_oz - heavy.limit_oz;
		return {
			weightOz: heavy.weight_oz,
			limitOz: heavy.limit_oz,
			pct: 100,
			over: true,
			overOz,
			heaviest,
			lines: heavy.lines.length,
			text: `${weight(heavy.weight_oz)} packed: ${weight(overOz)} over the ${limit} limit abroad. Take some out to send it.`,
			heaviestText: `Heaviest: ${heaviest.map((l) => `${l.quantity} × ${l.name} (${weight(l.oz)})`).join(', ')}.`
		};
	}
	if (!packing?.limit_oz) return null;
	return {
		weightOz: packing.weight_oz,
		limitOz: packing.limit_oz,
		pct: Math.min(100, (packing.weight_oz / packing.limit_oz) * 100),
		over: false,
		overOz: 0,
		heaviest: [],
		lines: 0,
		// Some item has no weight, so it's heavier than this.
		text: `${packing.unknown_weight ? 'At least ' : ''}${weight(packing.weight_oz)} of the ${limit} limit abroad, packed.`,
		heaviestText: ''
	};
}

/** The note on an item's row in the list when it's one of the heaviest in a request over the limit. */
export function heavyNote(m: WeightMeter | null, itemID: number): string {
	const l = m?.heaviest.find((l) => l.item_id === itemID);
	return l ? `Heaviest in your box: ${weight(l.oz)}.` : '';
}

/** Whether to log `weight_limit`: once each time the request goes over, again only after it's back under. */
export function weightCrossing(wasOver: boolean, m: WeightMeter | null): { fire: boolean; over: boolean } {
	const over = !!m?.over;
	return { fire: over && !wasOver, over };
}

/** The `weight_limit` event's props: numbers and the country, never item names. */
export function weightLimitProps(m: WeightMeter, country: string) {
	const r = (oz: number) => Math.round(oz * 10) / 10;
	return { over_oz: r(m.overOz), weight_oz: r(m.weightOz), lines: m.lines, country: country.trim().toUpperCase() };
}
