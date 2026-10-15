import type { Item, Purpose } from './types';

/** What a request is for. The request form asks this first, like the old Fillout forms. Mirrors swag.Purposes.
 * Clubs and events say how they'll use it (reason, with an example); personal requests don't. */
export const PURPOSES: { value: Purpose; label: string; short: string; reason: string; example: string }[] = [
	{ value: 'personal', label: 'I just want some stuff personally / to share with my friends', short: 'Personal', reason: '', example: '' },
	{
		value: 'club', label: 'I run a Hack Club', short: 'Hack Club',
		reason: 'In 1–2 sentences, how will you use it? This will be reviewed before we send your shipment, usually with 6-10 hour turnaround. More detail helps us approve more requests.',
		example: 'e.g. Handing out stickers and mini-magazines at our school club fair on Sept 12.'
	},
	{
		value: 'event', label: 'I am running an event or exhibiting at one', short: 'Event',
		reason: 'In 1–2 sentences, how will you use it? Link the event, and if you are running it, say how many people you expect. This will be reviewed before we send your shipment, usually with 6-10 hour turnaround. More detail helps us approve more requests.',
		example: 'e.g. Running https://example-hacks.dev on Nov 8, about 120 hackers expected.'
	}
];

/** Club and event requests must say how they'll use what they ask for. Mirrors swag.ErrNoReason. */
export const needsReason = (purpose: Purpose | ''): boolean => purpose === 'club' || purpose === 'event';

/** Club-only items (the full-size magazine, the T-shirt) aren't offered for personal requests. Mirrors Item.Offered. */
export const offered = (item: Item, purpose: Purpose | ''): boolean => purpose !== 'personal' || !item.club_only;

/** The items to show for a purpose; everything until one is picked. */
export const itemsFor = (items: Item[], purpose: Purpose | ''): Item[] => items.filter((i) => offered(i, purpose));

/** Reads a purpose saved to localStorage; anything else is "not picked yet". */
export const parsePurpose = (raw: string | null): Purpose | '' => PURPOSES.find((p) => p.value === raw)?.value ?? '';

/** "Hack Club" for 'club'; '' for imported requests, which have no purpose. */
export const purposeLabel = (p: string): string => PURPOSES.find((x) => x.value === p)?.short ?? '';
