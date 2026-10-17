import { describe, expect, it } from 'vitest';
import { billingDetails, paidNotice, payShipping, type CardForm } from './payment';
import type { Address } from './types';

const de: Address = { first_name: 'Orpheus', last_name: 'Dino', line_1: 'Teststr 1', line_2: '', city: 'Berlin', state: '', postal_code: '10115', country: 'de', phone_number: '+49301234567' };

function fakeCard(result: Awaited<ReturnType<CardForm['confirm']>>) {
	const calls: string[] = [];
	const card: CardForm = {
		check: async () => null,
		setAmount: (c) => void calls.push(`amount ${c}`),
		confirm: async (secret, returnUrl) => {
			calls.push(`confirm ${secret} ${returnUrl}`);
			return result;
		}
	};
	return { card, calls };
}

describe('payment', () => {
	it('bills the shipping address, so the card form only asks for the card', () => {
		expect(billingDetails(de)).toEqual({
			name: 'Orpheus Dino',
			phone: '+49301234567',
			address: { line1: 'Teststr 1', line2: '', city: 'Berlin', state: '', postal_code: '10115', country: 'DE' }
		});
	});

	it('pays a request: start the payment, match the amount, confirm the card', async () => {
		const { card, calls } = fakeCard({});
		const events: unknown[] = [];
		const err = await payShipping(card, 7, async (path) => {
			calls.push(`post ${path}`);
			return { client_secret: 'pi_1_secret', amount_cents: 900 };
		}, 'http://app.test', (n, p) => events.push([n, p]));
		expect(err).toBe('');
		expect(calls).toEqual(['post /api/requests/7/pay', 'amount 900', 'confirm pi_1_secret http://app.test/requests?paid=7']);
		expect(events).toEqual([]);
	});

	it('says why a card failed and records the reason, not the message', async () => {
		const { card } = fakeCard({ error: { type: 'card_error', code: 'card_declined', decline_code: 'insufficient_funds', message: 'Your card has insufficient funds.' } });
		const events: unknown[] = [];
		const err = await payShipping(card, 7, async () => ({ client_secret: 's', amount_cents: 900 }), 'http://app.test', (n, p) => events.push([n, p]));
		expect(err).toBe('Your card has insufficient funds.');
		expect(events).toEqual([['payment_error', { request_id: 7, type: 'card_error', code: 'card_declined', decline_code: 'insufficient_funds' }]]);
	});

	it('passes on the server refusing to start a payment', async () => {
		const { card, calls } = fakeCard({});
		const err = await payShipping(card, 7, async () => { throw new Error('this request is already paid'); }, 'http://app.test', () => {});
		expect(err).toBe('this request is already paid');
		expect(calls).toEqual([]);
	});

	it('thanks people once the payment shows up', () => {
		const reqs = [
			{ id: 3, status: 'pending' as const },
			{ id: 4, status: 'awaiting_payment' as const }
		];
		expect(paidNotice(reqs, '3')).toBe("Thanks! We got your payment for request #3. We'll ship it soon.");
		expect(paidNotice(reqs, '4')).toBe("We're still waiting to hear from the bank about request #4. Reload in a minute.");
		expect(paidNotice(reqs, null)).toBe('');
		expect(paidNotice(reqs, '99')).toBe('');
		expect(paidNotice(null, '3')).toBe('');
	});
});
