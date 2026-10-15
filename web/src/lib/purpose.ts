import type { Item, Purpose } from './types';

/** What a request is for. The request form asks this first, like the old Fillout forms. Mirrors swag.Purposes. */
export const PURPOSES: { value: Purpose; label: string; short: string; note: string }[] = [
	{ value: 'personal', label: 'I just want some stuff personally / to share with my friends', short: 'Personal', note: 'Anything we should know?' },
	{ value: 'club', label: 'I run a Hack Club', short: 'Hack Club', note: 'Which club is it, and how will you use this?' },
	{ value: 'event', label: 'I am running an event or exhibiting at one', short: 'Event', note: 'Which event, when, and about how many people?' }
];

/** Club-only items (the full-size magazine, the T-shirt) aren't offered for personal requests. Mirrors Item.Offered. */
export const offered = (item: Item, purpose: Purpose | ''): boolean => purpose !== 'personal' || !item.club_only;

/** The items to show for a purpose; everything until one is picked. */
export const itemsFor = (items: Item[], purpose: Purpose | ''): Item[] => items.filter((i) => offered(i, purpose));

/** Reads a purpose saved to localStorage; anything else is "not picked yet". */
export const parsePurpose = (raw: string | null): Purpose | '' => PURPOSES.find((p) => p.value === raw)?.value ?? '';

/** "Hack Club" for 'club'; '' for imported requests, which have no purpose. */
export const purposeLabel = (p: string): string => PURPOSES.find((x) => x.value === p)?.short ?? '';
