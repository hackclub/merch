import { describe, expect, it } from 'vitest';
import type { Item, SkuStock } from './types';
import { lockedSizes, newerNote, useUpBlock, useUpErrors, useUpNote } from './useup';

const sizes = (base: string, labels: string[]) => labels.map((label) => ({ label, sku: `${base}/${label}` }));
const old = { id: 603, name: 'Hack Club T-Shirt', sku: 'Swa/Shirt/HC', visible: true, club_only: true, staff_only: false, allow_backorder: false, variants: sizes('Swa/Shirt/HC', ['S', 'M', 'L', 'XL']) } as Item;
const heidi = {
	id: 970, name: 'Heidi T-Shirt', sku: 'Swa/Shirt/HHw', visible: true, club_only: true, staff_only: false, allow_backorder: true, use_up_first: 603,
	variants: sizes('Swa/Shirt/HHw', ['S', 'M', 'L', 'XL', 'XXL'])
} as Item;
const items = [old, heidi];
// As on 2026-11-19: the old shirt is out of M (the warehouse omits it); the Heidi shirt is all inbound.
const stock: Record<string, SkuStock> = {
	'Swa/Shirt/HC/S': { in_stock: 37, inbound: 0 }, 'Swa/Shirt/HC/L': { in_stock: 75, inbound: 0 }, 'Swa/Shirt/HC/XL': { in_stock: 99, inbound: 0 },
	'Swa/Shirt/HHw/M': { in_stock: 0, inbound: 260 }
};

describe('useUpBlock', () => {
	it('blocks a size the older item still has', () => {
		expect(useUpBlock(heidi, items, stock, 'Swa/Shirt/HHw/S', 'club')).toEqual({ old, label: 'S' });
	});
	it("lets through a size the older item is out of, or never came in", () => {
		expect(useUpBlock(heidi, items, stock, 'Swa/Shirt/HHw/M', 'club')).toBeNull();
		expect(useUpBlock(heidi, items, stock, 'Swa/Shirt/HHw/XXL', 'club')).toBeNull();
	});
	it('counts open holds: none left once requests hold the rest', () => {
		expect(useUpBlock(heidi, items, { 'Swa/Shirt/HC/S': { in_stock: 0, inbound: 0 } }, 'Swa/Shirt/HHw/S', 'club')).toBeNull();
		expect(useUpBlock(heidi, items, { 'Swa/Shirt/HC/S': { in_stock: -2, inbound: 0 } }, 'Swa/Shirt/HHw/S', 'club')).toBeNull();
	});
	it("doesn't block when the warehouse is down, or the older item isn't listed or offered", () => {
		expect(useUpBlock(heidi, items, undefined, 'Swa/Shirt/HHw/S', 'club')).toBeNull();
		expect(useUpBlock(heidi, items, {}, 'Swa/Shirt/HHw/S', 'club')).toBeNull();
		expect(useUpBlock(heidi, [heidi], stock, 'Swa/Shirt/HHw/S', 'club')).toBeNull();
		expect(useUpBlock({ ...heidi, club_only: false }, items, stock, 'Swa/Shirt/HHw/S', 'personal')).toBeNull();
		expect(useUpBlock(old, items, stock, 'Swa/Shirt/HC/S', 'club')).toBeNull();
	});
	it('works for items without sizes', () => {
		const a = { id: 1, name: 'Old sticker', sku: 'Sti/Old', visible: true, variants: [] } as unknown as Item;
		const b = { id: 2, name: 'New sticker', sku: 'Sti/New', visible: true, variants: [], use_up_first: 1 } as unknown as Item;
		expect(useUpBlock(b, [a, b], { 'Sti/Old': { in_stock: 4, inbound: 0 } }, undefined, 'club')).toEqual({ old: a, label: '' });
		expect(useUpBlock(b, [a, b], { 'Sti/Old': { in_stock: 0, inbound: 0 } }, undefined, 'club')).toBeNull();
	});
});

describe('lockedSizes', () => {
	it('lists the sizes that wait on the older item', () => {
		expect([...lockedSizes(heidi, items, stock, 'club')]).toEqual(['Swa/Shirt/HHw/S', 'Swa/Shirt/HHw/L', 'Swa/Shirt/HHw/XL']);
		expect(lockedSizes(old, items, stock, 'club').size).toBe(0);
	});
});

describe('useUpNote', () => {
	it('says which sizes are open now and where to get the rest', () => {
		expect(useUpNote(heidi, items, stock, 'club')).toBe(
			"We're giving out the Hack Club T-Shirt first, so this one comes only in M and XXL for now. For S, L or XL, get the Hack Club T-Shirt."
		);
	});
	it('says when no size is open yet', () => {
		const full = { ...stock, 'Swa/Shirt/HC/M': { in_stock: 5, inbound: 0 } };
		const noXXL = { ...heidi, variants: sizes('Swa/Shirt/HHw', ['S', 'M', 'L', 'XL']) };
		expect(useUpNote(noXXL, [old, noXXL], full, 'club')).toBe(
			"We're giving out the Hack Club T-Shirt first. Each size of this one opens once the Hack Club T-Shirt runs out of it."
		);
	});
	it('is empty once nothing waits', () => {
		expect(useUpNote(heidi, items, {}, 'club')).toBe('');
		expect(useUpNote(old, items, stock, 'club')).toBe('');
	});
	it('handles a single size and items without sizes', () => {
		const one = { ...stock, 'Swa/Shirt/HC/L': { in_stock: 0, inbound: 0 }, 'Swa/Shirt/HC/XL': { in_stock: 0, inbound: 0 } };
		expect(useUpNote(heidi, items, one, 'club')).toBe(
			"We're giving out the Hack Club T-Shirt first, so this one comes only in M, L, XL and XXL for now. For S, get the Hack Club T-Shirt."
		);
		const a = { id: 1, name: 'Old sticker', sku: 'Sti/Old', visible: true, variants: [] } as unknown as Item;
		const b = { id: 2, name: 'New sticker', sku: 'Sti/New', visible: true, variants: [], use_up_first: 1 } as unknown as Item;
		expect(useUpNote(b, [a, b], { 'Sti/Old': { in_stock: 4, inbound: 0 } }, 'club')).toBe("We're giving out the Old sticker first. Get that until it runs out.");
	});
});

describe('newerNote', () => {
	it("points the older item's sold-out sizes at the newer one", () => {
		expect(newerNote(old, items, stock, 'club')).toBe('Out of M? Get the Heidi T-Shirt.');
	});
	it('is empty when the older item has every size, or nothing names it', () => {
		const full = { ...stock, 'Swa/Shirt/HC/M': { in_stock: 5, inbound: 0 } };
		expect(newerNote(old, items, full, 'club')).toBe('');
		expect(newerNote(old, [old], stock, 'club')).toBe('');
		expect(newerNote(heidi, items, stock, 'club')).toBe('');
		expect(newerNote(old, items, stock, 'personal')).toBe(''); // the newer one isn't offered to them
	});
});

describe('useUpErrors', () => {
	it('names each picked size that waits on the older item', () => {
		const cart = { 970: 3 };
		const picked = { 970: { 'Swa/Shirt/HHw/S': 1, 'Swa/Shirt/HHw/M': 2 } };
		expect(useUpErrors(cart, items, picked, stock, 'club')).toEqual(['Heidi T-Shirt: size S is still available as the Hack Club T-Shirt; get that instead.']);
		expect(useUpErrors(cart, items, { 970: { 'Swa/Shirt/HHw/M': 3 } }, stock, 'club')).toEqual([]);
	});
});
