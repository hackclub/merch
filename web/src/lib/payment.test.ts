import { describe, expect, it } from 'vitest';
import { paidNotice, payLabel, payWhere } from './payment';

describe('payment', () => {
	it('says where international shipping is paid', () => {
		expect(payWhere(true)).toBe("After you submit we'll take you to a secure page to pay it by card; we ship once it's paid.");
		expect(payWhere(false)).toBe("After you submit we'll send you to HCB to pay it; we ship once it's received.");
		expect(payLabel('/api/requests/3/pay')).toBe('Pay shipping');
		expect(payLabel('https://hcb.hackclub.com/donations/start/swag?amount=900')).toBe('Pay shipping on HCB');
	});

	it('thanks people back from paying, once the payment shows up', () => {
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
