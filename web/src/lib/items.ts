import type { BookOptions } from './book3d';
import type { FoldOptions } from './fold3d';
import { sheetThickness } from './paper';
import type { Viewer, ViewerOptions } from './paper3d';
import { hasSize } from './scale';
import type { PostcardOptions } from './postcard3d';
import type { ShirtOptions } from './shirt3d';
import type { StickerOptions } from './sticker3d';
import { PER_BAG } from './stickers';
import { isImage } from './tile';
import type { Item, ItemStock } from './types';

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

export const thumbnail = (item: Item): string => item.images[0] ?? item.pages[0] ?? '';

/** Booklets with page images open in 3D and flip through. */
export const isBooklet = (item: Item) => item.binding !== '' && item.binding !== 'trifold' && item.pages.length >= 2;

/** A trifold brochure with its outside (images[0]) and inside (images[1]) art folds and unfolds in 3D. */
export const isTrifold = (item: Item) => item.binding === 'trifold' && item.images.length >= 2;

/** A 3D product preview: booklet flip-through, trifold, printed sheet, sticker packs, postcard bundles, or a T-shirt. */
export type Preview3D =
	| { book: BookOptions } // a booklet that flips through its pages
	| { fold: FoldOptions } // a trifold brochure that opens and folds up
	| { sheet: ViewerOptions } // a sized print, front and back
	| { stickers: StickerOptions }
	| { postcards: PostcardOptions } // a stack, or bundles of 50
	| { shirt: ShirtOptions }; // cut out of its product photos, front and back

/** What a shop tile shows instead of its photo; null = just the photo. */
export type TilePreview =
	| { clip: string; hover?: Preview3D } // a looping video, GIF or photo of the real thing; a still photo can give way to 3D on hover
	| Preview3D;

/**
 * The 3D view for an item: on the product page (phone = true, with any ships_with
 * extras standing beside a booklet) or alone on a shop tile (phone = false).
 */
export function productPreview(item: Item, includes: Item[] = [], phone = true): Preview3D | null {
	if (item.packs === 'sticker') {
		return item.images.length
			? { stickers: { kind: 'stickers', front: item.images[0], widthIn: item.width_in, heightIn: item.height_in, phone } }
			: null;
	}
	if (item.variants.length && !hasSize(item)) {
		// Sized items are shirts: shown in an M (or the middle size), front and back.
		const labels = item.variants.map((v) => v.label);
		const size = labels.includes('M') ? 'M' : labels[Math.floor((labels.length - 1) / 2)];
		return item.images.length ? { shirt: { kind: 'shirt', front: item.images[0], back: item.images[1], size, phone } } : null;
	}
	if (!hasSize(item)) return null;
	if (item.packs === 'postcard' && item.images.length) {
		return { postcards: postcardOptions(item, phone) };
	}
	if (isBooklet(item)) {
		const extras = phone
			? shipsWith(item, includes)
					.filter((i) => hasSize(i) && i.images.length)
					.map((i) => ({
						front: i.images[0], back: i.images[1], widthIn: i.width_in!, heightIn: i.height_in!,
						thicknessIn: sheetThickness(i), finish: i.finish
					}))
			: [];
		return {
			book: {
				pages: item.pages, widthIn: item.width_in!, heightIn: item.height_in!, finish: item.finish, binding: item.binding,
				// Without a measured thickness, about 0.1 mm per page.
				thicknessIn: item.thickness_in ?? item.pages.length * 0.004, extras, phone
			}
		};
	}
	if (!item.images.length) return null;
	if (isTrifold(item)) {
		return {
			fold: {
				outside: item.images[0], inside: item.images[1], widthIn: item.width_in!, heightIn: item.height_in!,
				thicknessIn: sheetThickness(item), finish: item.finish, phone
			}
		};
	}
	return {
		sheet: {
			front: item.images[0], back: item.images[1], widthIn: item.width_in!, heightIn: item.height_in!,
			thicknessIn: sheetThickness(item), finish: item.finish, phone
		}
	};
}

/**
 * A tile's preview, on its own (no phone): the item's clip if it has one, a
 * booklet's pages, a sized print's sheet, or stickers. A still photo clip shows
 * the 3D view while hovered (a shirt photographed on someone turns into the shirt).
 */
export function tilePreview(item: Item): TilePreview | null {
	const model = productPreview(item, [], false);
	if (item.clip) return model && isImage(item.clip) ? { clip: item.clip, hover: model } : { clip: item.clip };
	return model;
}

/**
 * Whether picking how many of an item also shows that amount stacked up in 3D
 * (beside its clip or flip-through): booklets, and anything measured with a clip
 * except shirts.
 */
export function showsAmountIn3D(item: Item): boolean {
	const preview = productPreview(item, [], false);
	return (isBooklet(item) || !!item.clip) && !!preview && !('shirt' in preview);
}

/** Lazy-loads the matching WebGL viewer for a Preview3D descriptor. */
export function loadPreviewViewer(canvas: HTMLCanvasElement, preview: Preview3D, count: number, onReady: () => void): Promise<Viewer> {
	if ('book' in preview) return import('./book3d').then((m) => m.createBookViewer(canvas, preview.book, onReady));
	if ('fold' in preview) return import('./fold3d').then((m) => m.createFoldViewer(canvas, preview.fold, onReady));
	if ('postcards' in preview) {
		return import('./postcard3d').then((m) => {
			const v = m.createPostcardViewer(canvas, preview.postcards, onReady);
			v.setCount(count);
			return v;
		});
	}
	if ('stickers' in preview) {
		return import('./sticker3d').then((m) => {
			const v = m.createStickerViewer(canvas, preview.stickers, onReady);
			v.setCount(count);
			return v;
		});
	}
	if ('shirt' in preview) return import('./shirt3d').then((m) => m.createShirtViewer(canvas, preview.shirt, onReady));
	return import('./paper3d').then((m) => m.createViewer(canvas, preview.sheet, onReady));
}

/** The postcard view's options for a sized postcard item, no phone (the product page adds one). */
export const postcardOptions = (item: Item, phone = false): PostcardOptions => ({
	kind: 'postcards', front: item.images[0], back: item.images[1], widthIn: item.width_in!, heightIn: item.height_in!,
	thicknessIn: item.thickness_in, finish: item.finish, phone
});

/**
 * How many the 3D views show: stickers come in sealed bags of 50, so show one
 * until you pick a quantity; postcards show the smallest pack (5).
 */
export const previewCount = (item: Item, inCart: number): number =>
	inCart > 0 ? inCart : item.packs === 'sticker' ? PER_BAG : item.packs === 'postcard' ? 5 : inCart;

/** Page files in the order people number them: page-2 before page-10. */
export const byFilename = <T extends { name: string }>(files: Iterable<T>): T[] =>
	[...files].sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }));

/** The items an item ships with (looked up by SKU), in the order it lists them. */
export const shipsWith = (item: Item, includes: Item[]): Item[] =>
	item.ships_with.flatMap((sku) => includes.filter((i) => i.sku === sku));

/** Returns a copy of images with images[i] moved one place left (-1) or right (+1). */
export function moveImage(images: string[], i: number, delta: -1 | 1): string[] {
	const j = i + delta;
	if (j < 0 || j >= images.length) return [...images];
	const next = [...images];
	[next[i], next[j]] = [next[j], next[i]];
	return next;
}

/** Warehouse unit cost ("0.0832" dollars) as "$0.0832"; at least cents, never rounded away. */
export function formatUnitCost(cost: string | null): string {
	const n = Number(cost);
	if (!cost || !Number.isFinite(n)) return '';
	const decimals = Math.max(2, cost.split('.')[1]?.length ?? 0);
	return new Intl.NumberFormat('en-US', {
		style: 'currency', currency: 'USD', minimumFractionDigits: decimals, maximumFractionDigits: decimals
	}).format(n);
}

/** "$0.37 each", or "$13.82–$14.27 each" when an item's sizes cost different amounts. */
export function unitCostLabel(stock: ItemStock): string {
	const lo = formatUnitCost(stock.unit_cost);
	if (!lo) return '';
	const hi = formatUnitCost(stock.unit_cost_max);
	return `${hi ? `${lo}–${hi}` : lo} each`;
}

/** "S 92 · M 95 · L 123", for items that come in sizes; "" otherwise. */
export const sizeStockLabel = (stock: ItemStock): string =>
	stock.variants.map((v) => `${v.label} ${v.in_stock.toLocaleString('en-US')}`).join(' · ');
