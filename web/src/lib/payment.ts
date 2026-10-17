import { track } from './track';
import type { Address, RequestStatus } from './types';

// payWhere tells international requesters how they'll pay shipping. The card
// form (Stripe) speaks for itself; HCB happens after they submit.
export function payWhere(card: boolean): string {
	return card
		? ''
		: "After you submit we'll send you to HCB to pay it; we ship once it's received.";
}

// billingDetails bills the shipping address, so the card form only asks for the card.
export function billingDetails(a: Address) {
	return {
		name: `${a.first_name.trim()} ${a.last_name.trim()}`.trim(),
		phone: a.phone_number.trim(),
		address: {
			line1: a.line_1.trim(),
			line2: a.line_2.trim(),
			city: a.city.trim(),
			state: a.state.trim(),
			postal_code: a.postal_code.trim(),
			country: a.country.trim().toUpperCase()
		}
	};
}

export type PayError = { type?: string; code?: string; decline_code?: string; message?: string };

// CardForm is the card field on the page (CardField.svelte, Stripe's Payment Element).
export type CardForm = {
	check(): Promise<string | null>; // what's wrong with what they typed, or null
	setAmount(cents: number): void;
	confirm(clientSecret: string, returnUrl: string): Promise<{ error?: PayError }>;
};

// payShipping charges the card for a request's shipping. It returns '' once the
// card goes through, or what went wrong. Card failures are recorded by their
// codes, never their message.
export async function payShipping(
	card: CardForm,
	requestId: number,
	start: (path: string) => Promise<{ client_secret: string; amount_cents: number }>,
	origin: string,
	report: (name: string, props: Record<string, unknown>) => void = track
): Promise<string> {
	let intent;
	try {
		intent = await start(`/api/requests/${requestId}/pay`);
	} catch (e) {
		return e instanceof Error ? e.message : 'Could not start the payment.';
	}
	card.setAmount(intent.amount_cents);
	// Cards that need a bank check (3-D Secure) open it over the page; a few
	// send people away and back to My requests.
	const { error } = await card.confirm(intent.client_secret, `${origin}/requests?paid=${requestId}`);
	if (!error) return '';
	report('payment_error', { request_id: requestId, type: error.type, code: error.code, decline_code: error.decline_code });
	return error.message || "Your card didn't go through.";
}

// paidNotice is what My requests says after a payment (?paid=<id>).
export function paidNotice(requests: { id: number; status: RequestStatus }[] | null, paid: string | null): string {
	const r = paid && requests?.find((r) => String(r.id) === paid);
	if (!r) return '';
	if (r.status === 'awaiting_payment') return `We're still waiting to hear from the bank about request #${r.id}. Reload in a minute.`;
	return `Thanks! We got your payment for request #${r.id}. We'll ship it soon.`;
}
