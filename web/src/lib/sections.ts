import type { Item } from './types';

/**
 * A shelf on the request form. Items join the first section that matches them.
 * The start section holds what we most want people to hand out, in the order of
 * `skus`; the rest are extras, grouped by what they are. The featured SKU leads
 * its section as a big tile.
 */
export type Section = {
	key: string;
	title: string;
	blurb: string;
	/** SKU prefixes; the start section lists its items in this order. */
	skus?: string[];
	match?: (i: Item) => boolean;
	/** Listed under "Extras", after the start section. */
	extra?: boolean;
	/** Orders the section's items before sort order: lower first. */
	rank?: (i: Item) => number;
	featured?: string;
	/** Everything but the featured item is shown as a grid under it. */
	grid?: boolean;
};

// A single printed sheet (not a card, booklet or brochure), and whether it's bigger than letter size.
const flat = (i: Item) => i.sku.startsWith('Pri/') && !i.packs && !i.binding;
const poster = (i: Item) => Math.max(i.width_in ?? 0, i.height_in ?? 0) > 11;

export const SECTIONS: Section[] = [
	{
		key: 'start',
		title: 'Start here',
		blurb: 'The best way to spread Hack Club: hand these out at school, at your club, or to friends.',
		skus: ['Pri/Bok/Mini25/', 'Pri/Wel/4x6/', 'Pri/Hav/4x6/']
	},
	// Cards hand out better than posters, so they come first. Flyers (letter size and smaller) follow the cards.
	{ key: 'cards', title: 'More cards and flyers', blurb: '', extra: true, match: (i) => i.packs === 'postcard' || (flat(i) && !poster(i)), rank: (i) => Number(i.packs !== 'postcard') },
	{ key: 'posters', title: 'Posters', blurb: '', extra: true, match: flat },
	{ key: 'booklets', title: 'Magazine and brochure', blurb: '', extra: true, match: (i) => !!i.binding },
	{ key: 'wear', title: 'Shirts and pins', blurb: '', extra: true, skus: ['Swa/'] },
	{ key: 'more', title: 'More', blurb: '', extra: true },
	{
		key: 'stickers',
		title: 'Stickers',
		blurb: '',
		extra: true,
		match: (i) => i.packs === 'sticker' || i.sku.startsWith('Sti/'),
		featured: 'Sti/Bra/O&H/Lap',
		grid: true
	}
];

export type Tile = { item: Item; big: boolean };

// Where an item's SKU falls in its section's `skus`, or -1.
const skuIndex = (s: Section, i: Item) => s.skus?.findIndex((p) => i.sku.startsWith(p)) ?? -1;

/** The shop's sections in order, each with its tiles; empty sections are left out.
 * `rows` and `grid` split the tiles for the request form: grid sections list only the featured item as a row. */
export function sectionize(items: Item[]): { section: Section; tiles: Tile[]; rows: Tile[]; grid: Tile[] }[] {
	const home = (i: Item) =>
		SECTIONS.find((s) => skuIndex(s, i) >= 0 || !!s.match?.(i)) ?? SECTIONS.find((s) => s.key === 'more')!;
	return SECTIONS.map((section) => {
		const mine = items
			.filter((i) => home(i) === section)
			.sort(
				(a, b) =>
					Number(b.sku === section.featured) - Number(a.sku === section.featured) ||
					skuIndex(section, a) - skuIndex(section, b) ||
					(section.rank ? section.rank(a) - section.rank(b) : 0) ||
					a.sort_order - b.sort_order
			);
		const tiles = mine.map((item) => ({ item, big: mine.length > 1 && item.sku === section.featured }));
		const inGrid = (t: Tile) => !!section.grid && t.item.sku !== section.featured;
		return { section, tiles, rows: tiles.filter((t) => !inGrid(t)), grid: tiles.filter(inGrid) };
	}).filter((s) => s.tiles.length > 0);
}

/** The tile's one line of facts: "11×17 in", "S M L XL · limit 1". Everything is free, so it doesn't say so. */
export function tileFact(item: Item): string {
	const facts: string[] = [];
	if (item.variants.length) facts.push(item.variants.map((v) => v.label).join(' '));
	if (item.binding && item.pages.length) facts.push(`${item.pages.length} pages`);
	else if (item.width_in && item.height_in && item.packs !== 'sticker') facts.push(`${item.width_in}×${item.height_in} in`);
	if (item.packs === 'sticker') facts.push('3, 10 or packs of 50');
	else if (item.packs) facts.push('in fives');
	if (item.min_per_request > 1) facts.push(`at least ${item.min_per_request}`);
	if (item.max_per_user != null) facts.push(`limit ${item.max_per_user}`);
	return facts.join(' · ');
}
