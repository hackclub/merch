// Mirrors the Go JSON types in internal/swag and internal/store.
import type { ShippingEstimate } from './shipping';

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
	finish: string; // '' | 'matte' | 'gloss' | 'linen' | 'uncoated'
	thickness_in: number | null;
	weight_oz: number | null; // one unit's shipping weight; null = worked out from its paper size
	photos: Photo[]; // real-world pictures people posted
	packs: string; // '' (any quantity) | 'fives' (5, 10, 15…) | 'postcard' (5, 10…25) | 'sticker' (3, 10), then multiples of 50
	binding: string; // '' (a single sheet) | 'staple' | 'perfect': booklets open in 3D | 'trifold': a sheet folded in thirds
	pages: string[]; // a booklet's page images in reading order, front cover first
	ships_with: string[]; // warehouse SKUs packed with every unit (the magazine's card)
	club_only: boolean; // not offered for personal requests, only clubs and events
	clip: string; // looping video, GIF or photo of the real thing, shown on its shop tile; '' = none
};

export type Variant = { label: string; sku: string };
export type Photo = { url: string; caption: string; credit: string };

export type Settings = {
	requests_open: boolean;
	max_items_per_request: number;
	request_cooldown_days: number;
	international_shipping_fee_cents: number;
};

export type Catalog = {
	items: Item[];
	includes: Item[]; // what listed items ship with, often hidden items
	settings: Settings & { hcb_payment_url: string; stripe_key?: string };
};

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
	note: string;
	admin_note: string;
	theseus_order_id: string;
	tracking_number: string;
	carrier: string;
	mailed_at: string | null;
	source: string; // 'app' | 'fillout' (imported history)
	airtable_record_id?: string;
	internal_note?: string; // admin-only
	created_at: string;
	lines: { item_id: number; sku: string; name: string; variant?: string; quantity: number }[];
	payment_url?: string; // HCB
	stripe_key?: string; // pay by card on the page
	estimate?: ShippingEstimate; // while it waits to ship; postage only for admins
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
