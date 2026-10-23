import { describe, expect, it } from 'vitest';
import { forGroups, itemsFor, needsReason, offered, parseNeed, parsePurpose, printing, PURPOSES, purposeLabel, STAFF_NEEDS } from './purpose';
import type { Item } from './types';

const base: Item = { id: 1, sku: 'Sti/A', name: 'Stickers', description: '', images: [], visible: true, sort_order: 0, max_per_request: 3, min_per_request: 1, quick_picks: [], max_per_user: null, width_in: null, height_in: null, variants: [], finish: '', thickness_in: null, weight_oz: null, photos: [], print_files: [], packs: '', binding: '', pages: [], ships_with: [], club_only: false, staff_only: false, model: '', allow_backorder: false, clip: '' };
const shirt: Item = { ...base, id: 2, sku: 'Swa/Shirt/HC', name: 'T-shirt', club_only: true };

describe('purpose', () => {
	it('asks the four kinds of request, personal first, staff last', () => {
		expect(PURPOSES.map((p) => p.value)).toEqual(['personal', 'event', 'club', 'staff']);
		expect(PURPOSES.find((p) => p.value === 'staff')?.label).toBe("I'm Hack Club staff");
	});

	it('gives staff everything clubs and events get', () => {
		expect(offered(shirt, 'staff')).toBe(true);
		expect(needsReason('staff')).toBe(true);
		expect(forGroups('staff')).toBe(true);
		expect(forGroups('club')).toBe(true);
		expect(forGroups('event')).toBe(true);
		expect(forGroups('personal')).toBe(false);
		expect(forGroups('')).toBe(false);
		expect(parsePurpose('staff')).toBe('staff');
		expect(purposeLabel('staff')).toBe('Staff');
	});

	it('asks clubs and events, not personal requests, how they will use it', () => {
		expect(needsReason('personal')).toBe(false);
		expect(needsReason('club')).toBe(true);
		expect(needsReason('event')).toBe(true);
		expect(PURPOSES.find((p) => p.value === 'personal')?.reason).toBe('');
		for (const p of PURPOSES.filter((p) => p.value !== 'personal')) expect(p.reason).toMatch(/^In 2-3 sentences, how will you use it\? Please include specifics \(including estimated # of people you're getting swag for\) and links\. If you're requesting multiple t-shirts, please include the names of the people they're for\.\n\n/);
		for (const p of PURPOSES.filter((p) => p.value !== 'personal')) expect(p.reason).toMatch(/This will be reviewed before we send your shipment, usually with 6-10 hour turnaround\. More detail helps us approve more requests\. We may adjust quantities up or down based on what you put here\. \(For events people often don't request enough stickers, for example\)$/);
	});

	it('leaves club-only items out of personal requests', () => {
		expect(offered(shirt, 'personal')).toBe(false);
		expect(offered(shirt, 'club')).toBe(true);
		expect(offered(shirt, 'event')).toBe(true);
		expect(itemsFor([base, shirt], 'personal')).toEqual([base]);
		expect(itemsFor([base, shirt], 'event')).toEqual([base, shirt]);
		// Before they've answered, show everything.
		expect(itemsFor([base, shirt], '')).toEqual([base, shirt]);
	});

	it('offers staff-only items (the plushie) only to staff', () => {
		const plush: Item = { ...base, id: 3, sku: 'Swa/HC/Plu/Orp', name: 'Orpheus Plushie', staff_only: true };
		expect(offered(plush, 'staff')).toBe(true);
		for (const p of ['personal', 'club', 'event', ''] as const) expect(offered(plush, p)).toBe(false);
		expect(itemsFor([base, shirt, plush], 'club')).toEqual([base, shirt]);
		expect(itemsFor([base, shirt, plush], 'staff')).toEqual([base, shirt, plush]);
	});

	it('reads a saved purpose, defaulting to sharing with friends', () => {
		expect(parsePurpose('club')).toBe('club');
		expect(parsePurpose('resale')).toBe('personal');
		expect(parsePurpose(null)).toBe('personal');
	});

	it('labels past requests, including imported ones with no purpose', () => {
		expect(purposeLabel('event')).toBe('Event');
		expect(purposeLabel('')).toBe('');
	});

	it('asks staff whether they want swag or something printed, swag first', () => {
		expect(STAFF_NEEDS.map((n) => n.value)).toEqual(['swag', 'print']);
		expect(STAFF_NEEDS[1].label).toBe('I want to get postcards / posters made');
		expect(parseNeed('print')).toBe('print');
		expect(parseNeed(null)).toBe('swag');
		expect(parseNeed('banner')).toBe('swag');
		expect(printing('staff', 'print')).toBe(true);
		// Only staff get things printed; everyone else always requests swag.
		expect(printing('club', 'print')).toBe(false);
		expect(printing('staff', 'swag')).toBe(false);
	});
});
