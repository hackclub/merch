import type { Item } from './types';

/**
 * A shelf on the shop page. Items join the first section whose SKU prefix
 * matches; the featured SKU leads its section as a big tile. Program sections
 * link to the program, so people know what a Hackpad poster is for.
 */
export type Section = {
	key: string;
	title: string;
	blurb: string;
	link?: { href: string; label: string };
	prefixes: string[];
	featured?: string;
};

export const SECTIONS: Section[] = [
	{
		key: 'magazines',
		title: 'Hack Club Magazine',
		blurb: 'What teenagers built in 2025, in print. Hand them out, leave them on a table.',
		link: { href: 'https://magazine.hackclub.com', label: 'Read it online' },
		prefixes: ['Pri/Bok/'],
		featured: 'Pri/Bok/Mini25/1st'
	},
	{
		key: 'hackpad',
		title: 'Hackpad',
		blurb: 'Design your own macropad and we ship it to you. Put these up at school.',
		link: { href: 'https://hackpad.hackclub.com', label: 'About Hackpad' },
		prefixes: ['Pri/HPd/'],
		featured: 'Pri/HPd/11x/1st'
	},
	{
		key: 'hackclub',
		title: 'Hack Club',
		blurb: 'For teens 13–18, anywhere. Wear it, and tell your friends.',
		link: { href: 'https://hackclub.com', label: 'hackclub.com' },
		prefixes: ['Swa/', 'Pri/Wel/'],
		featured: 'Swa/Shirt/HC'
	},
	{ key: 'more', title: 'More', blurb: '', prefixes: [] },
	{
		key: 'stickers',
		title: 'Stickers',
		blurb: 'Packs of 50 for your club, or a few for your laptop.',
		prefixes: ['Sti/'],
		featured: 'Sti/Bra/O&H/Lap'
	}
];

export type Tile = { item: Item; big: boolean };

/** The shop's sections in order, each with its tiles; empty sections are left out. */
export function sectionize(items: Item[]): { section: Section; tiles: Tile[] }[] {
	const home = (i: Item) =>
		SECTIONS.find((s) => s.prefixes.some((p) => i.sku.startsWith(p))) ?? SECTIONS.find((s) => s.key === 'more')!;
	return SECTIONS.map((section) => {
		const mine = items
			.filter((i) => home(i) === section)
			.sort((a, b) => Number(b.sku === section.featured) - Number(a.sku === section.featured) || a.sort_order - b.sort_order);
		return { section, tiles: mine.map((item) => ({ item, big: mine.length > 1 && item.sku === section.featured })) };
	}).filter((s) => s.tiles.length > 0);
}

/** The tile's one line of facts: "Free · 11×17 in", "Free · S M L XL · limit 1". */
export function tileFact(item: Item): string {
	const facts = ['Free'];
	if (item.variants.length) facts.push(item.variants.map((v) => v.label).join(' '));
	if (item.binding && item.pages.length) facts.push(`${item.pages.length} pages`);
	else if (item.width_in && item.height_in && item.packs !== 'sticker') facts.push(`${item.width_in}×${item.height_in} in`);
	if (item.packs === 'sticker') facts.push('3, 10 or packs of 50');
	else if (item.packs) facts.push('in fives');
	if (item.max_per_user != null) facts.push(`limit ${item.max_per_user}`);
	return facts.join(' · ');
}
