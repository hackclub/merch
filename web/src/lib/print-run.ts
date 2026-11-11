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
			(!f.needs ||
				(r.origin === 'legacy' && ['available', 'cancelled', 'rejected'].includes(r.stage)
					? false
					: r.reconcile || r.legacy_review || !['available', 'cancelled', 'rejected'].includes(r.stage)))
	);
}
export type PO = {
	status?: string;
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
	po_line_ordered?: number | null;
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
	details?: {
		stage_changed?: boolean;
		specifications?: string;
		approved_quantity?: number;
		art_revision?: number;
		update_source?: string;
	};
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
		max_age_seconds: number;
		synced_at: string | null;
		warehouse_sellable: number | null;
		held: number | null;
		available: number | null;
		inbound: number | null;
		visible: boolean;
	};
	shipment: SwagRequest | null;
	po_automation: boolean;
	po_linking_available: boolean;
};
export function nextOwner(
	r: Pick<PrintRun, 'stage' | 'legacy_review' | 'reconcile'> & { origin?: string },
	admin = true
): string {
	if (['rejected', 'cancelled'].includes(r.stage)) return 'Closed';
	if (r.origin === 'legacy') {
		if (['available', 'rejected', 'cancelled'].includes(r.stage)) return 'Closed';
		return admin ? 'HQ: audit legacy run' : 'HQ review';
	}
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
	if (
		!p.synced_at ||
		!Number.isFinite(Date.parse(p.synced_at)) ||
		Date.parse(p.synced_at) > now ||
		now - new Date(p.synced_at).getTime() > 3600000
	)
		return 'Not yet confirmed in latest copy';
	if (p.status) return p.status;
	if (p.draft || p.deleted) return 'Needs HQ review: draft or deleted PO';
	return p.completed ? 'PO complete (whole order)' : 'Received quantity unavailable';
}
export function approvedQuantityLabel(r: Pick<PrintRun, 'approved_quantity' | 'origin'>): number | string {
	return r.approved_quantity || (r.origin === 'legacy' ? 'Unknown (legacy)' : 'Not yet approved');
}
export function stockStale(synced: string | null | undefined, maxAgeSeconds: number, now = Date.now()): boolean {
	return !synced || !Number.isFinite(Date.parse(synced)) || now - Date.parse(synced) > maxAgeSeconds * 1000;
}
export function poLinkingMessage(available: boolean): string {
	return available
		? ''
		: 'PO linking is waiting for its read-only connection. Requests stay saved while HQ finishes setup.';
}
export function actionLabel(action: string): string {
	return (
		(
			{
				submit: 'Print request submitted',
				approve: 'Print run approved',
				link: 'Purchase order linked',
				replace_links: 'Purchase order links corrected',
				observe: 'Warehouse evidence updated',
				dispatch: 'Printer dispatch recorded',
				release: 'Released in shop',
				reject: 'Print request rejected',
				cancel: 'Print request cancelled',
				update: 'HQ update',
				eta_update: 'Expected arrival updated',
				review_order: 'Order discrepancy reviewed',
				review_receipt: 'Corrected receipt reviewed',
				audit_legacy: 'Legacy run audited',
				legacy_import: 'Legacy run imported',
				legacy_submission: 'Original print request submitted',
				legacy_status_change: 'Historical stage recorded',
				preferences: 'Notification preferences updated',
				first_shipment: 'First shipment updated',
				retry_notifications: 'Failed notifications scheduled for retry',
				resolve_notification: 'Notification delivery reconciled',
				reorder: 'Another print run requested',
				refresh_po: 'Check latest PO copy'
			} as Record<string, string>
		)[action] || 'Print request updated'
	);
}
export function printEventLabel(event: PrintEvent, previousStage: string | undefined, admin: boolean): string {
	if (event.action !== 'observe') return actionLabel(event.action);
	if (!admin) return stageLabel[event.stage] || 'Warehouse update';
	const changed = event.details?.stage_changed ?? (previousStage !== undefined && previousStage !== event.stage);
	return changed ? `${stageLabel[event.stage]} · Warehouse PO sync` : actionLabel('observe');
}
export function approvalReviewer(admin: boolean, id: number): string {
	return admin ? `reviewer #${id}` : 'HQ';
}
export function printPOStatus(r: Pick<PrintRun, 'poll_error' | 'links'>, now = Date.now()): string {
	if (r.poll_error) return 'Not yet confirmed in latest copy';
	if (!r.links.length) return 'Not linked';
	const states = r.links.map((p) => poState(p, now));
	if (states.includes('Not yet confirmed in latest copy')) return 'Not yet confirmed in latest copy';
	if (states.some((s) => s.startsWith('Needs HQ review'))) return 'Needs HQ review';
	return r.links.every((p) => p.completed) ? 'PO complete (whole order)' : 'Received quantity unavailable';
}
export function actionPreview(
	r: PrintRun,
	action: string,
	quantity: number,
	note: string,
	eta: string,
	now = Date.now(),
	automatic = true,
	updateSource = 'HQ update'
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
						p.quantity > 0 &&
						Date.parse(p.synced_at) === Date.parse(r.links[0].synced_at) &&
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
			return `Your ${item} print request was cancelled. ${note}. ${['ordered', 'in_transit'].includes(r.stage) || r.links?.some((p) => !p.draft && !p.deleted && !!p.created_at) ? 'Printer confirmed cancellation.' : 'No order was placed.'}`;
		case 'dispatch':
			return `Your print run has shipped from the printer to the warehouse. Estimated arrival: ${eta || r.eta || 'not confirmed'}. This is not your first parcel.`;
		case 'release':
			return `${item} is available in the shop. Your first ${r.request.initial_quantity} will be linked to a shipment request, awaiting review or postage payment. Your first parcel has not shipped.`;
		case 'audit_legacy':
			return 'Records verified legacy evidence without replaying historical email or creating a second parcel.';
		case 'review_receipt':
			return note + ' Source: ' + updateSource + '. Received quantity unavailable.';
		case 'refresh_po':
			return 'Checks the latest PO copy. A newly confirmed stage sends its corresponding requester update; stale evidence does not advance receipt.';
		case 'update':
			return (
				note + (eta ? ` Expected warehouse arrival: ${eta} (${updateSource}). This is not a delivery promise.` : '')
			);
		case 'review_order':
			return note;
		default:
			return 'This saves a timeline event. No stage email is sent until the stage changes.';
	}
}
