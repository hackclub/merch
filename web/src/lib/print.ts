// Staff getting postcards and posters printed: the products (GET /api/print/products,
// from swag.PrintProducts), checking art before it uploads, the bleed template, and
// the estimate's wording. The server checks uploads again (swag.CheckArt).

export type PrintProduct = {
	key: string;
	name: string;
	paper: string;
	vendor: string;
	width_in: number; // trimmed, short side first
	height_in: number;
	bleed_in: number;
	safe_in: number;
	dpi: number;
	finish: string;
	thickness_in: number;
	postcard: boolean;
	min_quantity: number;
	max_quantity: number;
	step: number;
	bleed_width_in: number;
	bleed_height_in: number;
	safe_width_in: number;
	safe_height_in: number;
	bleed_width_px: number;
	bleed_height_px: number;
};

export type PrintEstimate = {
	product: string;
	quantity: number;
	print_cents: number;
	shipping_cents: number;
	total_cents: number;
	each_cents: number;
	min_days: number;
	max_days: number;
	ready_from: string; // YYYY-MM-DD, New York
	ready_by: string;
};

export type PrintRequest = {
	id: number;
	user_email?: string;
	user_name?: string;
	product: string;
	title: string;
	quantity: number;
	front_url: string;
	back_url: string;
	note: string;
	needed_by: string | null;
	estimate_cents: number;
	status: 'submitted' | 'ordered' | 'stocked' | 'rejected';
	admin_note: string;
	sku: string;
	initial_quantity: number; // sent to them once it's stocked
	shipment_request_id: number | null; // the request that became
	created_at: string;
};

export const printStatusLabel: Record<PrintRequest['status'], string> = {
	submitted: 'Submitted',
	ordered: 'Ordered from Vistaprint',
	stocked: 'In the warehouse',
	rejected: 'Not printed'
};

/** How art fits: full size with bleed, the trimmed size, or neither. Under the product's DPI (300) is refused.
 * Mirrors swag.CheckArt. */
export type ArtCheck = { fit: 'bleed' | 'trim' | 'wrong_shape'; dpi: number; problem?: string; warning?: string };

const TOLERANCE = 0.005;

export function checkArt(p: PrintProduct, w: number, h: number): ArtCheck {
	const short = Math.min(w, h);
	const long = Math.max(w, h);
	const want = `${p.bleed_width_px} x ${p.bleed_height_px} px (${p.bleed_width_in}" x ${p.bleed_height_in}" at ${p.dpi} DPI)`;
	if (short <= 0) return { fit: 'wrong_shape', dpi: 0, problem: 'The file has no picture in it.' };
	for (const [fit, sw, sh] of [['bleed', p.bleed_width_in, p.bleed_height_in], ['trim', p.width_in, p.height_in]] as const) {
		if (Math.abs(short / long / (sw / sh) - 1) > TOLERANCE) continue;
		const dpi = Math.floor(short / sw);
		if (dpi < p.dpi) return { fit, dpi, problem: `It's ${dpi} DPI, and prints need at least ${p.dpi} DPI. Export it at ${want}.` };
		const c: ArtCheck = { fit, dpi };
		if (fit === 'trim') c.warning = `No bleed: it's the trimmed size, so a white sliver may show at the edges. Best is ${want}, with the background running to the edge.`;
		return c;
	}
	return { fit: 'wrong_shape', dpi: 0, problem: `It's ${w} x ${h} px, which isn't the shape of a ${p.name}. Make it ${want}.` };
}

/** The part of the art that's left after trimming, in its pixels: what the 3D view shows. */
export function trimCrop(p: PrintProduct, w: number, h: number): { sx: number; sy: number; sw: number; sh: number } {
	if (checkArt(p, w, h).fit !== 'bleed') return { sx: 0, sy: 0, sw: w, sh: h };
	const cut = (p.bleed_in / p.bleed_width_in) * Math.min(w, h);
	return { sx: cut, sy: cut, sw: w - 2 * cut, sh: h - 2 * cut };
}

/** Where the trim and safe lines fall on the art, as insets in percent of its width (x) and height (y). */
export function guides(p: PrintProduct, w: number, h: number) {
	const bleed = checkArt(p, w, h).fit === 'bleed';
	const inch = Math.min(w, h) / (bleed ? p.bleed_width_in : p.width_in); // pixels per inch
	const inset = (inches: number) => ({ x: ((inches * inch) / w) * 100, y: ((inches * inch) / h) * 100 });
	const trim = bleed ? p.bleed_in : 0;
	// The safe line inside the trim box, in percent of the trimmed width and height.
	const tw = w - 2 * trim * inch;
	const th = h - 2 * trim * inch;
	return { trim: inset(trim), safe: inset(trim + p.safe_in), safeInTrim: { x: ((p.safe_in * inch) / tw) * 100, y: ((p.safe_in * inch) / th) * 100 } };
}

/** The trimmed sheet in inches, turned the way the art is (landscape art, landscape card). */
export const sheetFor = (p: PrintProduct, w: number, h: number) =>
	w > h ? { widthIn: p.height_in, heightIn: p.width_in } : { widthIn: p.width_in, heightIn: p.height_in };

const fmt = (n: number) => String(Math.round(n * 1000) / 1000);

/**
 * A full-size template to design on, at the product's DPI in pixels: the bleed (tinted, trimmed off),
 * the trim line, and the safe area (dashed) to keep text and logos inside.
 */
export function templateSVG(p: PrintProduct, landscape: boolean): string {
	const [bw, bh] = landscape ? [p.bleed_height_in, p.bleed_width_in] : [p.bleed_width_in, p.bleed_height_in];
	const [sw, sh] = landscape ? [p.safe_height_in, p.safe_width_in] : [p.safe_width_in, p.safe_height_in];
	const [tw, th] = landscape ? [p.height_in, p.width_in] : [p.width_in, p.height_in];
	const px = (inches: number) => fmt(inches * p.dpi);
	const b = p.bleed_in * p.dpi;
	const s = (p.bleed_in + p.safe_in) * p.dpi;
	const font = Math.round(p.dpi * (p.postcard ? 0.09 : 0.18));
	const line = Math.max(2, Math.round(p.dpi / 100));
	return `<svg xmlns="http://www.w3.org/2000/svg" width="${fmt(bw)}in" height="${fmt(bh)}in" viewBox="0 0 ${px(bw)} ${px(bh)}">
<title>${p.name} template: ${fmt(bw)}" x ${fmt(bh)}" with bleed</title>
<rect width="${px(bw)}" height="${px(bh)}" fill="#ec3750" fill-opacity="0.18"/>
<rect x="${fmt(b)}" y="${fmt(b)}" width="${px(tw)}" height="${px(th)}" fill="#fff" stroke="#000" stroke-width="${line}"/>
<rect x="${fmt(s)}" y="${fmt(s)}" width="${px(sw)}" height="${px(sh)}" fill="none" stroke="#338eda" stroke-width="${line}" stroke-dasharray="${line * 6} ${line * 4}"/>
<g font-family="sans-serif" font-size="${font}" fill="#333">
<text x="${fmt(s + font)}" y="${fmt(s + font * 2)}">${p.name} · ${p.vendor}</text>
<text x="${fmt(s + font)}" y="${fmt(s + font * 3.4)}" fill="#ec3750">Red: bleed, ${fmt(bw)}" x ${fmt(bh)}" (${px(bw)} x ${px(bh)} px). Run the background to this edge; it's cut off.</text>
<text x="${fmt(s + font)}" y="${fmt(s + font * 4.8)}">Black: trim, ${fmt(tw)}" x ${fmt(th)}". The finished size.</text>
<text x="${fmt(s + font)}" y="${fmt(s + font * 6.2)}" fill="#338eda">Blue dashes: safe area, ${fmt(sw)}" x ${fmt(sh)}". Keep text and logos inside.</text>
<text x="${fmt(s + font)}" y="${fmt(s + font * 7.6)}">Export a PNG or JPEG at ${p.dpi} DPI, without these guides.</text>
</g>
</svg>`;
}

const usd = (cents: number) => `$${(cents / 100).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;

/** "About $119.58 for 1,000 (12¢ each)". */
export function estimateLine(e: PrintEstimate): string {
	const each = e.each_cents < 100 ? `${Math.round(e.each_cents)}¢` : usd(Math.round(e.each_cents));
	return `About ${usd(e.total_cents)} for ${e.quantity.toLocaleString('en-US')} (${each} each)`;
}

const day = (iso: string) =>
	new Date(`${iso}T12:00:00Z`).toLocaleDateString('en-US', { weekday: 'short', month: 'short', day: 'numeric', timeZone: 'UTC' });

/** "In our warehouse between Fri, Oct 30 and Fri, Nov 6 (5–10 business days)". */
export const readyText = (e: PrintEstimate) =>
	`In our warehouse between ${day(e.ready_from)} and ${day(e.ready_by)} (${e.min_days}–${e.max_days} business days)`;

/** A heads-up when they need them before the estimate's latest day; '' otherwise. */
export function neededByNote(neededBy: string, e: PrintEstimate): string {
	if (!neededBy || neededBy >= e.ready_by) return '';
	return `That's before ${day(e.ready_by)}, so they might not make it. Say why in the note and we'll try to rush it (express shipping, or printing locally).`;
}

/** Quick picks for how many to print. */
export const quantityPresets = (p: PrintProduct): number[] =>
	p.postcard ? [250, 500, 1000, 2500, 5000, 10000] : [25, 100, 250, 500, 1000, 3000];

/** What the `print_art` event logs about a picked file: how it fits and its size, never its name or pixels. */
export const printArtProps = (product: string, side: string, check: ArtCheck | null, bytes: number, type = '') =>
	check
		? { product, side, fit: check.fit, dpi: check.dpi, problem: !!check.problem, warning: !!check.warning, mb: Math.round(bytes / 1e5) / 10 }
		: { product, side, fit: 'unreadable', problem: true, type: type.slice(0, 40) };

/** How many to send them first: postcards in the shop's packs (5s to 25, then 50s), posters a few at a time; up to the run. */
export function firstShipmentOptions(p: PrintProduct, run: number): number[] {
	const all = p.postcard ? [5, 10, 15, 20, 25, 50, 100, 150, 200, 250] : [1, 2, 3, 5, 10, 15, 20, 25];
	return [0, ...all.filter((n) => n <= run)];
}
