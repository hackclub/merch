import type { RequestStatus } from './types';

// payWhere tells international requesters how they'll pay shipping.
export function payWhere(card: boolean): string {
	return card
		? "After you submit we'll take you to a secure page to pay it by card; we ship once it's paid."
		: "After you submit we'll send you to HCB to pay it; we ship once it's received.";
}

// payLabel names a request's payment link: our own (Stripe) or HCB's.
export function payLabel(url: string): string {
	return url.startsWith('/') ? 'Pay shipping' : 'Pay shipping on HCB';
}

// paidNotice is what My requests says to someone Stripe sent back (?paid=<id>).
export function paidNotice(requests: { id: number; status: RequestStatus }[] | null, paid: string | null): string {
	const r = paid && requests?.find((r) => String(r.id) === paid);
	if (!r) return '';
	if (r.status === 'awaiting_payment') return `We're still waiting to hear from the bank about request #${r.id}. Reload in a minute.`;
	return `Thanks! We got your payment for request #${r.id}. We'll ship it soon.`;
}
