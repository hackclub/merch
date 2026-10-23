import { describe, expect, it } from 'vitest';
import { blankAddress } from './address';
import { clearDraft, readDraft, resumeAddress, resumeStep, saveDraft, VERIFY_EMAIL_URL, verifyEmailProps, type Draft } from './draft';

const memory = () => {
	const m = new Map<string, string>();
	return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k), m };
};
const home = { ...blankAddress(), first_name: 'Orpheus', last_name: 'Dino', line_1: '15 Falls Rd', city: 'Shelburne', state: 'VT', postal_code: '05482' };
const draft: Draft = { address: home, note: 'Club fair, 40 people', understood: true, customs_ok: false, regroup_ok: false };

describe('request draft (signed-out Submit → verify email → back)', () => {
	it('keeps the form through the sign-in round trip', () => {
		const s = memory();
		saveDraft(s, draft);
		expect(readDraft(s)).toEqual(draft);
		clearDraft(s);
		expect(readDraft(s)).toBeNull();
	});

	it('ignores missing or broken drafts', () => {
		const s = memory();
		expect(readDraft(s)).toBeNull();
		s.setItem('merch:draft', '{nope');
		expect(readDraft(s)).toBeNull();
		s.setItem('merch:draft', '{"note":"x"}');
		expect(readDraft(s)).toBeNull();
	});

	it('logs which sign-in is the verify step', () => {
		expect(VERIFY_EMAIL_URL).toBe('/auth/login?for=request');
	});

	it('keeps the typed address, taking only a missing phone from Hack Club Auth', () => {
		const hca = { ...blankAddress(), first_name: 'O', last_name: 'D', line_1: '1 Other St', line_2: 'Apt 5', city: 'Elsewhere', postal_code: '10001', phone_number: '+1 555 0100' };
		expect(resumeAddress(home, hca)).toEqual({ ...home, phone_number: '+1 555 0100' });
		expect(resumeAddress(home, null)).toEqual(home);
		// Nothing typed: the Hack Club Auth address fills it, as it would for anyone signed in.
		expect(resumeAddress(blankAddress(), hca)).toEqual(hca);
	});

	it('sends US requests on return; abroad they still pay by card first', () => {
		expect(resumeStep(false)).toBe('submit');
		expect(resumeStep(true)).toBe('card');
	});

	it('logs the step without the address or note', () => {
		const p = verifyEmailProps(draft, { items: 12, intl: false, purpose: 'club' });
		expect(p).toEqual({ items: 12, intl: false, purpose: 'club', has_note: true });
		expect(JSON.stringify(p)).not.toMatch(/Falls|Orpheus|fair/);
	});
});
