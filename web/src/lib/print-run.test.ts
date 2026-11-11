import { it, expect } from 'vitest';
import { nextOwner, poState, actionPreview, type PO, type PrintRun } from './print-run';
import { filterPrintRuns } from './print-run';
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
});
it('separates receipt, release and parcel dispatch', () => {
	expect(nextOwner({ stage: 'received', legacy_review: false, reconcile: false })).toBe('HQ: release listing');
	const r = { stage: 'received', request: { title: 'Demo cards', initial_quantity: 25 } } as PrintRun;
	expect(actionPreview(r, 'release', 800, '', '')).toContain('Your first parcel has not shipped');
	expect(actionPreview(r, 'approve', 800, '', '')).toContain('HQ will place the printer order next');
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
	expect(actionPreview({ ...r, links: [{ ...p, draft: true }] }, 'link', 800, '', '', now, true)).toContain(
		'does not confirm ordering'
	);
});
