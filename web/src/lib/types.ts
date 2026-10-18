// Mirrors the Go JSON types in internal/swag and internal/store.
import type { Costs, ShippingEstimate } from './shipping';

export type Item = {
	id: number;
	sku: string;
	name: string;
	description: string;
	images: string[]; // CDN URLs in display order; the first is the thumbnail
	visible: boolean;
	sort_order: number;
	max_per_request: number;
	min_per_request: number; // fewest per request (all sizes together); 1 for most things
	quick_picks: number[]; // one-tap amounts on the shop, ascending; empty = worked out from the limits
	max_per_user: number | null;
	width_in: number | null; // physical size in inches, for printed items
	height_in: number | null;
	variants: Variant[]; // sizes, each its own warehouse SKU; empty = one size
	finish: string; // '' | 'matte' | 'gloss' | 'linen' | 'uncoated' | 'enamel' (an enamel pin)
	thickness_in: number | null;
	weight_oz: number | null; // one unit's shipping weight; null = worked out from its paper size
	photos: Photo[]; // real-world pictures people posted
	print_files: PrintFile[]; // source print files, for printing your own
	packs: string; // '' (any quantity) | 'fives' (5, 10, 15…) | 'postcard' (5, 10…25) | 'sticker' (3, 10), then multiples of 50
	binding: string; // '' (a single sheet) | 'staple' | 'perfect': booklets open in 3D | 'trifold': a sheet folded in thirds
	pages: string[]; // a booklet's page images in reading order, front cover first
	ships_with: string[]; // warehouse SKUs packed with every unit (the magazine's card)
	club_only: boolean; // not offered for personal requests, only clubs and events
	clip: string; // looping video, GIF or photo of the real thing, shown on its shop tile; '' = none
};

export type Variant = { label: string; sku: string; weight_oz?: number | null }; // weight_oz: one unit of this size; null = the item's
export type Photo = { url: string; caption: string; credit: string };
export type PrintFile = { url: string; label: string; bytes: number }; // label: e.g. 'Front'

/** A key an agent uses to review requests (/api/agent). The token is shown only when it's made. */
export type APIKey = {
	id: number;
	name: string;
	prefix: string;
	created_by_email: string;
	created_at: string;
	last_used_at: string | null;
	revoked_at: string | null;
};

// One email the site sent (or tried to), word for word.
export type SentEmail = {
	id: number;
	at: string;
	kind: 'received' | 'dispatched' | 'rejected';
	request_id: number | null;
	to: string;
	subject: string;
	preview: string;
	body: string;
	status: 'sent' | 'failed' | 'unsent';
	error: string;
};

export type Settings = {
	requests_open: boolean;
	international_shipping_fee_cents: number;
};

export type Catalog = {
	items: Item[];
	includes: Item[]; // what listed items ship with, often hidden items
	settings: Settings & { stripe_key: string }; // Stripe publishable key for the card form
	stock?: Record<string, SkuStock>; // by SKU, for listed items and their sizes; empty if the warehouse is down
};

// in_stock goes below zero when requests wait on a restock; inbound is what's on order.
export type SkuStock = { in_stock: number; inbound: number };

export type Address = {
	first_name: string;
	last_name: string;
	line_1: string;
	line_2: string;
	city: string;
	state: string;
	postal_code: string;
	country: string;
	phone_number: string;
};

/** What a request is for: for themselves and friends, a Hack Club they run, or an event. */
export type Purpose = 'personal' | 'club' | 'event';

export type Line = { item_id: number; quantity: number; sku?: string };

export type RequestStatus = 'awaiting_payment' | 'pending' | 'dispatched' | 'rejected' | 'cancelled';

export type SwagRequest = {
	id: number;
	user_id: number;
	user_email?: string;
	status: RequestStatus;
	purpose: Purpose | ''; // '' for imported history
	address: Address;
	shipping_fee_cents: number;
	paid_at: string | null;
	refunded_at?: string | null; // when shipping money went back
	refunded_cents?: number; // how much
	stripe_payment_intent?: string; // the card payment, if any
	note: string;
	admin_note: string;
	theseus_order_id: string;
	tracking_number: string;
	carrier: string;
	mailed_at: string | null;
	dispatched_at?: string | null; // when it went to the warehouse
	warehouse_costs?: { contents_cents: number | null; labor_cents: number | null; postage_cents: number | null }; // what mail.hackclub.com charged, once it says
	source: string; // 'app' | 'fillout' (imported history)
	airtable_record_id?: string;
	internal_note?: string; // admin-only
	created_at: string;
	lines: RequestLine[];
	stripe_key?: string; // pay by card on the page
	estimate?: ShippingEstimate; // while it waits to ship; postage only for admins
	costs?: Costs; // admins: what it costs Hack Club to send, while it waits to ship
};

export type SubmissionStatus = 'submitted' | 'approved' | 'printing' | 'stocked' | 'rejected';

export type Submission = {
	id: number;
	user_email?: string;
	program: string;
	title: string;
	description: string;
	kind: string;
	file_url: string;
	quantity: number;
	status: SubmissionStatus;
	admin_note: string;
	sku: string;
	created_at: string;
};

export type User = { id: number; email: string; name: string; slack_id: string; address?: Address };
export type Me = { user: User | null; roles: { author: boolean; admin: boolean } };

export type WarehouseSKU = {
	sku: string;
	name: string;
	category: string;
	in_stock: number;
	inbound: number | null;
	unit_cost: string;
};

/** Live warehouse numbers for a listed item (GET /api/items/{id}/stock). unit_cost_max is set
 * when the item's sizes cost different amounts; unit_cost is then the lowest. */
export type ItemStock = {
	in_stock: number;
	unit_cost: string | null;
	unit_cost_max: string | null;
	variants: { sku: string; label: string; in_stock: number; unit_cost: string | null }[]; // per size; empty = one size
};

/** One line of a request; requested_quantity is set when an admin shipped a different quantity (quantity 0 = not sent; requested_quantity 0 = added, not asked for). */
export interface RequestLine {
	item_id: number;
	sku: string;
	name: string;
	variant?: string;
	quantity: number;
	requested_quantity?: number;
}
