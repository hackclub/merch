import type { PrintRequest } from './print';
import type { SwagRequest } from './types';
export const stages = ['submitted', 'approved', 'ordered', 'in_transit', 'received', 'available'];
export const stageLabel: Record<string, string> = {
	submitted: 'Submitted',
	approved: 'Approved',
	ordered: 'Ordered from printer',
	in_transit: 'In transit',
	received: 'Received at warehouse',
	available: 'Available',
	rejected: 'Rejected',
	cancelled: 'Cancelled'
};
export type PrintFilter = { origin: string; stage: string; owner: number; needs: boolean };
export function filterPrintRuns(runs: PrintRun[], f: PrintFilter): PrintRun[] {
	return runs.filter(
		(r) =>
			(!f.origin || r.origin === f.origin) &&
			(!f.stage || r.stage === f.stage) &&
			(!f.owner || r.user_id === f.owner) &&
			(!f.needs || r.reconcile || r.legacy_review || !['available', 'cancelled', 'rejected'].includes(r.stage))
	);
}
export type PO = {
	po_id: number;
	line_id: number;
	sku: string;
	order_number: string;
	supplier: string;
	quantity: number;
	draft: boolean;
	deleted: boolean;
	completed: boolean;
	created_at: string | null;
	required_by: string | null;
	completed_at: string | null;
	synced_at: string;
	polled_at: string;
};
export type PrintRun = {
	id: number;
	user_id: number;
	user_name?: string;
	user_email?: string;
	origin: string;
	stage: string;
	version: number;
	request: PrintRequest & { front_preview_url?: string; back_preview_url?: string };
	approved_quantity: number;
	approved_at: string | null;
	approved_by: number;
	specifications: string;
	revision: number;
	sku: string;
	item_id: number;
	public_note: string;
	internal_note?: string;
	tracking: string;
	eta: string;
	eta_source: string;
	dispatched_at: string | null;
	reviewed_existing_order: boolean;
	reconcile: boolean;
	legacy_review: boolean;
	legacy_status: string;
	slack_opt_in: boolean;
	parent_id: number;
	last_poll_at: string | null;
	poll_error: string;
	stock?: PrintDetail['stock'];
	shipment_status: string;
	shipment_request_id: number | null;
	created_at: string;
	links: PO[];
};
export type PrintEvent = {
	details?: { specifications?: string; approved_quantity?: number; art_revision?: number; update_source?: string };
	id: number;
	stage: string;
	action: string;
	actor_id: number | null;
	at: string | null;
	recorded_at: string;
	note: string;
	source: string;
	version: number;
};
export type Notice = {
	id: number;
	channel: string;
	subject: string;
	body: string;
	status: string;
	attempts: number;
	error?: string;
};
export type PrintDetail = {
	confirmations?: { version: number; url: string; actor_id: number | null; at: string }[];
	run: PrintRun;
	history: PrintEvent[];
	notifications: Notice[];
	stock: {
		synced_at: string | null;
		warehouse_sellable: number | null;
		held: number | null;
		available: number | null;
		inbound: number | null;
		visible: boolean;
	};
	shipment: SwagRequest | null;
	po_automation: boolean;
};
export function nextOwner(r: Pick<PrintRun, 'stage' | 'legacy_review' | 'reconcile'>): string {
	if (r.legacy_review || r.reconcile) return 'HQ: reconcile warehouse evidence';
	return (
		(
			{
				submitted: 'HQ: review artwork and quantity',
				approved: 'HQ: link printer order and PO',
				ordered: 'HQ: record printer dispatch',
				in_transit: 'Warehouse: complete linked PO',
				received: 'HQ: release listing',
				available: 'Shipment review / requester postage',
				rejected: 'Requester: revise and resubmit',
				cancelled: 'Closed'
			} as Record<string, string>
		)[r.stage] ?? 'HQ: review'
	);
}
export function poState(p: PO, now = Date.now()): string {
	if (!p.synced_at || now - new Date(p.synced_at).getTime() > 3600000) return 'Not yet confirmed in latest copy';
	if (p.draft || p.deleted) return 'Needs HQ review: draft or deleted PO';
	return p.completed ? 'PO complete (whole order)' : 'Received quantity unavailable';
}
export function actionPreview(
	r: PrintRun,
	action: string,
	quantity: number,
	note: string,
	eta: string,
	now = Date.now(),
	automatic = true
): string {
	const item = r.request.title;
	switch (action) {
		case 'link':
		case 'replace_links': {
			if (!automatic) return 'Automatic confirmation is off. This saves explicit PO links without confirming a stage.';
			const seen = new Set<string>();
			const valid =
				r.links.length > 0 &&
				r.links.every((p) => {
					const key = p.po_id + ':' + p.line_id;
					if (seen.has(key)) return false;
					seen.add(key);
					return (
						p.po_id > 0 &&
						p.line_id > 0 &&
						p.sku === r.sku &&
						!p.draft &&
						!p.deleted &&
						!!p.created_at &&
						new Date(p.created_at).getTime() <= now &&
						!!p.synced_at &&
						new Date(p.synced_at).getTime() <= now &&
						now - new Date(p.synced_at).getTime() <= 3600000 &&
						(r.reviewed_existing_order || (!!r.approved_at && new Date(p.created_at) >= new Date(r.approved_at)))
					);
				}) &&
				r.links.reduce((n, p) => n + p.quantity, 0) === r.approved_quantity;
			if (!valid)
				return 'This evidence needs HQ review. Saving links does not confirm ordering or send a stage update.';
			if (r.links.every((p) => p.completed))
				return 'The warehouse marked your linked purchase order complete. Your print run is received at the warehouse. The shop listing awaits release. Your first parcel has not shipped.';
			const dates = r.links.flatMap((p) => (p.required_by ? [p.required_by.slice(0, 10)] : [])).sort();
			return `Your linked purchase order confirms ${r.approved_quantity} ${item} ordered. Expected warehouse date: ${dates.at(-1) || 'not confirmed'}. We will tell you when the printer ships.`;
		}
		case 'retry_notifications':
			return 'Retries failed messages with their original saved wording and event keys. The print stage stays unchanged.';
		case 'resolve_notification':
			return 'Records the verified provider delivery outcome. A message is retried only after HQ confirms it was not delivered.';
		case 'approve':
			return `Your ${item} request is approved for ${quantity}. HQ will place the printer order next.`;
		case 'reject':
			return `Your ${item} request was rejected: ${note}. No order was placed.`;
		case 'cancel':
			return `Your ${item} print request was cancelled. ${note}. ${['ordered', 'in_transit'].includes(r.stage) ? 'Printer confirmed cancellation.' : 'No order was placed.'}`;
		case 'dispatch':
			return `Your print run has shipped from the printer to the warehouse. Estimated arrival: ${eta || r.eta || 'not confirmed'}. This is not your first parcel.`;
		case 'release':
			return `${item} is available in the shop. Your first ${r.request.initial_quantity} will be linked to a shipment request, awaiting review or postage payment. Your first parcel has not shipped.`;
		case 'audit_legacy':
			return 'Records verified legacy evidence without replaying historical email or creating a second parcel.';
		case 'review_receipt':
			return note + ' Source: warehouse confirmation. Received quantity unavailable. The current stage and first parcel stay linked.';
		case 'refresh_po':
			return 'Checks the latest PO copy. A newly confirmed stage sends its corresponding requester update; stale evidence does not advance receipt.';
		case 'update':
			return note + (eta ? ` Expected warehouse arrival: ${eta} (HQ update). This is not a delivery promise.` : '');
		default:
			return 'This saves a timeline event. No stage email is sent until the stage changes.';
	}
}
