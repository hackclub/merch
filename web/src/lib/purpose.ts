import type { Item, Purpose } from './types';

const REASON =
	"In 2-3 sentences, how will you use it? Please include specifics (including estimated # of people you're getting swag for) and links. If you're requesting multiple t-shirts, please include the names of the people they're for.\n\n" +
	'This will be reviewed before we send your shipment, usually with 6-10 hour turnaround. More detail helps us approve more requests. We may adjust quantities up or down based on what you put here. (For events people often don\'t request enough stickers, for example)';

/** What a request is for. The request form asks this first, like the old Fillout forms. Mirrors swag.Purposes.
 * Clubs and events say how they'll use it (reason, with an example); personal requests don't.
 * A `beside` option sits on the right of the one before it, on one row (staff, the rare pick, next to clubs). */
export const PURPOSES: { value: Purpose; label: string; short: string; reason: string; example: string; beside?: boolean }[] = [
	{ value: 'personal', label: 'To share with friends (or at school, etc)', short: 'Personal', reason: '', example: '' },
	{
		value: 'event', label: "I'm running a hackathon or exhibiting at an event", short: 'Event',
		reason: REASON,
		example: 'e.g. Running https://example-hacks.dev on Nov 8, about 120 hackers expected.'
	},
	{
		value: 'club', label: 'I run a Hack Club', short: 'Hack Club',
		reason: REASON,
		example: 'e.g. Handing out stickers and mini-magazines at our school club fair on Sept 12.'
	},
	{
		value: 'staff', label: "I'm HQ staff", short: 'Staff', beside: true,
		reason: REASON,
		example: 'e.g. Hack Club table at Maker Faire Bay Area on Nov 8, about 300 visitors.'
	}
];

/** Clubs, events and staff hand swag out to groups: they get every item, the sticker-pack nudge and the table haul. */
export const forGroups = (purpose: Purpose | ''): boolean => purpose === 'club' || purpose === 'event' || purpose === 'staff';

/** Group requests must say how they'll use what they ask for. Mirrors swag.ErrNoReason. */
export const needsReason = forGroups;

/** Club-only items (the full-size magazine, the T-shirt) aren't offered for personal requests;
 * staff-only items (the Orpheus plushie) are offered only to staff. Mirrors Item.Offered. */
export const offered = (item: Item, purpose: Purpose | ''): boolean =>
	item.staff_only ? purpose === 'staff' : purpose !== 'personal' || !item.club_only;

/** The items to show for a purpose; everything until one is picked. */
export const itemsFor = (items: Item[], purpose: Purpose | ''): Item[] => items.filter((i) => offered(i, purpose));

/** Reads a purpose saved to localStorage; with none saved, "To share with friends" is picked. */
export const parsePurpose = (raw: string | null): Purpose | '' => PURPOSES.find((p) => p.value === raw)?.value ?? 'personal';

/** "Hack Club" for 'club'; '' for imported requests, which have no purpose. */
export const purposeLabel = (p: string): string => PURPOSES.find((x) => x.value === p)?.short ?? '';

/** What staff are here for: swag from the shop (the default), or postcards and posters printed for them. */
export type StaffNeed = 'swag' | 'print';

export const STAFF_NEEDS: { value: StaffNeed; label: string }[] = [
	{ value: 'swag', label: 'I want to request swag' },
	{ value: 'print', label: 'I want to get postcards / posters made' }
];

/** Reads a saved staff choice; requesting swag unless they picked printing. */
export const parseNeed = (raw: string | null): StaffNeed => (raw === 'print' ? 'print' : 'swag');

/** The print flow replaces the swag list only for staff who asked to get something made. */
export const printing = (purpose: Purpose | '', need: StaffNeed): boolean => purpose === 'staff' && need === 'print';
