import type { Item, Purpose } from './types';

const REASON =
	"In 2-3 sentences, how will you use it? Please include specifics (including estimated # of people you're getting swag for) and links. If you're requesting multiple t-shirts, please include the names of the people they're for.\n\n" +
	'This will be reviewed before we send your shipment, usually with 6-10 hour turnaround. More detail helps us approve more requests. We may adjust quantities up or down based on what you put here. (For events people often don\'t request enough stickers, for example)';

/** What a request is for. The request form asks this first, like the old Fillout forms. Mirrors swag.Purposes.
 * Clubs and events say how they'll use it (reason, with an example); personal requests don't. */
export const PURPOSES: { value: Purpose; label: string; short: string; reason: string; example: string }[] = [
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
	}
];

/** Club and event requests must say how they'll use what they ask for. Mirrors swag.ErrNoReason. */
export const needsReason = (purpose: Purpose | ''): boolean => purpose === 'club' || purpose === 'event';

/** Club-only items (the full-size magazine, the T-shirt) aren't offered for personal requests. Mirrors Item.Offered. */
export const offered = (item: Item, purpose: Purpose | ''): boolean => purpose !== 'personal' || !item.club_only;

/** The items to show for a purpose; everything until one is picked. */
export const itemsFor = (items: Item[], purpose: Purpose | ''): Item[] => items.filter((i) => offered(i, purpose));

/** Reads a purpose saved to localStorage; with none saved, "To share with friends" is picked. */
export const parsePurpose = (raw: string | null): Purpose | '' => PURPOSES.find((p) => p.value === raw)?.value ?? 'personal';

/** "Hack Club" for 'club'; '' for imported requests, which have no purpose. */
export const purposeLabel = (p: string): string => PURPOSES.find((x) => x.value === p)?.short ?? '';
