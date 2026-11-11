import { it, expect } from 'vitest';
import { nextOwner, poState, actionPreview, type PO, type PrintRun } from './print-run';
import {
	filterPrintRuns,
	printPOStatus,
	actionLabel,
	approvedQuantityLabel,
	stockStale,
	poLinkingMessage,
	printEventLabel,
	approvalReviewer,
	releaseBlockedReason,
	poSyncAge
} from './print-run';
it('explains why HQ must reconcile or refresh evidence before releasing', () => {
	const run = { reconcile: false, legacy_review: false, poll_error: '', source_stale: false, links: [] };
	expect(releaseBlockedReason(run)).toBe('');
	expect(releaseBlockedReason({ ...run, reconcile: true })).toBe(
		'Review and reconcile the warehouse evidence before releasing this listing.'
	);
	expect(releaseBlockedReason({ ...run, source_stale: true })).toBe(
		'Not yet confirmed in latest copy. Refresh the PO copy before releasing.'
	);
	expect(releaseBlockedReason({ ...run, poll_error: 'source unavailable' })).toBe(
		'Not yet confirmed in latest copy. Refresh the PO copy before releasing.'
	);
	expect(printPOStatus({ ...run, source_stale: true })).toBe('Not yet confirmed in latest copy');
	expect(poSyncAge('2026-11-11T10:00:00Z', Date.parse('2026-11-11T12:15:00Z'))).toBe('135 minutes old');
	expect(poSyncAge('', Date.now())).toBe('Sync age unknown');
});
it('labels automatic stage changes separately from evidence-only observations', () => {
	expect(
		printEventLabel({ stage: 'ordered', action: 'observe', details: { stage_changed: true } } as any, 'approved', true)
	).toBe('Ordered from printer · Warehouse PO sync');
	expect(
		printEventLabel({ stage: 'received', action: 'observe', details: { stage_changed: true } } as any, 'ordered', true)
	).toBe('Received at warehouse · Warehouse PO sync');
	expect(
		printEventLabel({ stage: 'ordered', action: 'observe', details: { stage_changed: false } } as any, 'ordered', true)
	).toBe('Warehouse evidence updated');
	expect(printEventLabel({ stage: 'received', action: 'observe' } as any, 'ordered', true)).toBe(
		'Received at warehouse · Warehouse PO sync'
	);
	expect(approvalReviewer(false, 1)).toBe('HQ');
	expect(approvalReviewer(true, 1)).toBe('reviewer #1');
	for (const stage of ['cancelled', 'rejected'])
		expect(nextOwner({ stage, legacy_review: false, reconcile: true })).toBe('Closed');
});
it('gives legacy runs a truthful owner, approval label and needs-action state', () => {
	const base = {
		origin: 'legacy',
		stage: 'approved',
		legacy_review: true,
		reconcile: true,
		approved_quantity: 0
	} as PrintRun;
	expect(nextOwner(base, true)).toBe('HQ: audit legacy run');
	expect(nextOwner(base, false)).toBe('HQ review');
	expect(approvedQuantityLabel(base)).toBe('Unknown (legacy)');
	for (const stage of ['available', 'rejected', 'cancelled']) {
		const r = { ...base, stage };
		expect(nextOwner(r, true)).toBe('Closed');
		expect(filterPrintRuns([r], { origin: '', stage: '', owner: 0, needs: true })).toHaveLength(0);
	}
});
it('uses human action labels and the server stock lifetime', () => {
	expect(actionLabel('observe')).toBe('Warehouse evidence updated');
	expect(actionLabel('audit_legacy')).toBe('Legacy run audited');
	const at = '2026-11-11T16:00:00Z',
		now = Date.parse(at) + 90000;
	expect(stockStale(at, 120, now)).toBe(false);
	expect(stockStale(at, 60, now)).toBe(true);
	expect(poLinkingMessage(false)).toContain('Requests stay saved');
	expect(poLinkingMessage(true)).toBe('');
});
it('does not report a retained completed flag as confirmation when the latest poll failed', () => {
	const r = { poll_error: 'Not yet confirmed in latest copy', links: [{ completed: true }] } as PrintRun;
	expect(printPOStatus(r)).toBe('Not yet confirmed in latest copy');
});
it('keeps reopened released runs in the needs-action queue', () => {
	const r = {
		id: 1,
		stage: 'available',
		origin: 'staff',
		user_id: 2,
		reconcile: true,
		legacy_review: false
	} as PrintRun;
	expect(filterPrintRuns([r], { origin: '', stage: '', owner: 0, needs: true })).toHaveLength(1);
	expect(filterPrintRuns([{ ...r, reconcile: false }], { origin: '', stage: '', owner: 0, needs: true })).toHaveLength(
		0
	);
});
it('states stale evidence and unavailable partial receipts carefully', () => {
	const p = { completed: false, synced_at: '2026-11-11T14:00:00Z' } as PO;
	expect(poState(p, Date.parse('2026-11-11T14:01:00Z'))).toBe('Received quantity unavailable');
	expect(poState(p, Date.parse('2026-11-11T16:00:00Z'))).toBe('Not yet confirmed in latest copy');
	expect(poState({ ...p, status: 'PO complete (whole order)' }, Date.parse('2026-11-11T16:00:00Z'))).toBe(
		'Not yet confirmed in latest copy'
	);
});
it('separates receipt, release and parcel dispatch', () => {
	expect(nextOwner({ stage: 'received', legacy_review: false, reconcile: false })).toBe('HQ: release listing');
	const r = { stage: 'received', request: { title: 'Demo cards', initial_quantity: 25 } } as PrintRun;
	expect(actionPreview(r, 'release', 800, '', '')).toContain('Your first parcel has not shipped');
	expect(actionPreview(r, 'approve', 800, '', '')).toContain('HQ will place the printer order next');
	expect(actionPreview(r, 'review_order', 800, 'Quantity reviewed with printer', '')).toBe(
		'Quantity reviewed with printer'
	);
});
it('previews only confirmed linked PO stage updates', () => {
	const now = Date.parse('2026-11-11T15:00:00Z');
	const p = {
		po_id: 42,
		line_id: 10,
		sku: 'Demo/Card',
		quantity: 800,
		completed: true,
		draft: false,
		deleted: false,
		created_at: '2026-11-11T14:30:00Z',
		synced_at: '2026-11-11T14:59:00Z'
	} as PO;
	const r = {
		request: { title: 'Demo cards' },
		approved_quantity: 800,
		approved_at: '2026-11-11T14:00:00Z',
		sku: p.sku,
		links: [p]
	} as PrintRun;
	expect(actionPreview(r, 'link', 800, '', '', now, false)).toContain('Automatic confirmation is off');
	expect(actionPreview(r, 'link', 800, '', '', now, true)).toContain('marked your linked purchase order complete');
	const split = {
		...r,
		links: [
			{ ...p, quantity: 400 },
			{ ...p, po_id: 43, line_id: 11, quantity: 400, synced_at: '2026-11-11T14:58:00Z' }
		]
	};
	expect(actionPreview(split, 'link', 800, '', '', now, true)).toContain('does not confirm ordering');
	expect(actionPreview({ ...r, links: [{ ...p, draft: true }] }, 'link', 800, '', '', now, true)).toContain(
		'does not confirm ordering'
	);
});
