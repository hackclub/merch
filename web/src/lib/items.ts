import type { Item } from './types';

const slugify = (s: string) =>
	s
		.toLowerCase()
		.replace(/[^a-z0-9]+/g, '-')
		.replace(/^-|-$/g, '');

/** Product page URL: /items/<id>-<slug>. Only the id matters; the slug is for humans. */
export function itemPath(item: Item): string {
	const slug = slugify(item.name);
	return `/items/${item.id}${slug ? '-' + slug : ''}`;
}

export function itemIdFromParam(param: string): number | null {
	const id = Number(/^(\d+)(?:-|$)/.exec(param)?.[1]);
	return id > 0 ? id : null;
}

export const thumbnail = (item: Item): string => item.images[0] ?? '';

/** Returns a copy of images with images[i] moved one place left (-1) or right (+1). */
export function moveImage(images: string[], i: number, delta: -1 | 1): string[] {
	const j = i + delta;
	if (j < 0 || j >= images.length) return [...images];
	const next = [...images];
	[next[i], next[j]] = [next[j], next[i]];
	return next;
}
