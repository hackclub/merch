import { prefillAddress } from './address';
import type { Address, Purpose } from './types';

/** Signed-out people fill the whole request form; Submit has them sign in with Hack Club to verify their email.
 * The form waits in sessionStorage (this tab only) through the round trip, then the request sends itself. */
export type Draft = { address: Address; note: string; understood: boolean; customs_ok: boolean; regroup_ok: boolean };

type Store = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

const KEY = 'merch:draft';

/** Sign-in from the form's Submit, so `login_start` says it's the verify step. */
export const VERIFY_EMAIL_URL = '/auth/login?for=request';

export const saveDraft = (s: Store, d: Draft) => s.setItem(KEY, JSON.stringify(d));

export const clearDraft = (s: Store) => s.removeItem(KEY);

export function readDraft(s: Store): Draft | null {
	try {
		const d = JSON.parse(s.getItem(KEY) ?? 'null');
		if (!d || typeof d !== 'object' || !d.address || typeof d.address.line_1 !== 'string') return null;
		return { address: d.address, note: String(d.note ?? ''), understood: !!d.understood, customs_ok: !!d.customs_ok, regroup_ok: !!d.regroup_ok };
	} catch {
		return null;
	}
}

/** The address they typed wins. Hack Club Auth fills only a missing phone, or the whole address when none was typed,
 * so a typed address never picks up another address's apartment number. */
export function resumeAddress(typed: Address, hca: Address | null | undefined): Address {
	if (!typed.line_1.trim()) return prefillAddress(typed, hca);
	return typed.phone_number.trim() || !hca?.phone_number ? typed : { ...typed, phone_number: hca.phone_number };
}

/** Back from verifying: US requests send themselves; abroad they still enter their card to pay the postage. */
export const resumeStep = (intl: boolean): 'submit' | 'card' => (intl ? 'card' : 'submit');

/** What the `verify_email` event logs: counts and choices, never the address or note. */
export const verifyEmailProps = (d: Draft, r: { items: number; intl: boolean; purpose: Purpose | '' }) => ({
	items: r.items, intl: r.intl, purpose: r.purpose, has_note: d.note.trim() !== ''
});
