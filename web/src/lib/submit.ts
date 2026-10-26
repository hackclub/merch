/** Why a request can't be sent yet, in the order the form should point at them (first one wins focus).
 * Submit stays enabled so tapping it can say what's missing and log `submit_blocked`; a greyed-out
 * button fires no click and leaves no record (see AGENTS.md "Usage analytics"). */
export type SubmitBlocker = 'understood' | 'customs' | 'sticker_regroup' | 'cart_problem' | 'quote_error';

export type SubmitState = {
	understood: boolean;
	customsOK: boolean;
	intl: boolean;
	nudge: boolean;
	regroupOK: boolean;
	problems: string[];
	quoteError: string;
};

export function submitBlockers(s: SubmitState): SubmitBlocker[] {
	const out: SubmitBlocker[] = [];
	if (!s.understood) out.push('understood');
	if (s.intl && !s.customsOK) out.push('customs');
	if (s.nudge && !s.regroupOK) out.push('sticker_regroup');
	if (s.problems.length) out.push('cart_problem');
	if (s.quoteError) out.push('quote_error');
	return out;
}

/** What to tell them about a blocker. Cart and quote problems carry their own wording. */
export function blockerMessage(b: SubmitBlocker, problems: string[], quoteError: string): string {
	switch (b) {
		case 'understood':
			return 'Check “I understand, and I’ll only request what I’ll use.” to send this request.';
		case 'customs':
			return 'Check the customs box to send this request.';
		case 'sticker_regroup':
			return 'Check “I understand.” under Stickers to send this request.';
		case 'cart_problem':
			return problems[0] ?? 'Fix your request above to send it.';
		case 'quote_error':
			return quoteError || 'We couldn’t quote shipping for this request.';
	}
}

/** The item line a cart problem is about ("Pick a size for …", "…: only 2 left"), for scrolling to it. */
export function problemItemId(problems: string[], items: { id: number; name: string }[]): number | null {
	for (const p of problems) {
		const item = items.find((i) => i.name && p.includes(i.name));
		if (item) return item.id;
	}
	return null;
}

/** Props for the `submit_blocked` browser event: which form, why, how far they got. Nothing typed. */
export const submitBlockedProps = (form: 'request' | 'print', reasons: string[], r: { intl: boolean; items: number; signedIn: boolean }) => ({
	form,
	reasons,
	intl: r.intl,
	items: r.items,
	signed_in: r.signedIn
});
