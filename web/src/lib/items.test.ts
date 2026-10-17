import { describe, expect, it } from 'vitest';
import { byFilename, cardSides, formatUnitCost, isBooklet, itemIdFromParam, productPreview, previewKind, shipsWith, sizeStockLabel, unitCostLabel, itemPath, moveImage, previewCount, showsAmountIn3D, tilePreview, thumbnail } from './items';
import type { Item } from './types';

const poster: Item = {
	id: 681, sku: 'Pri/HPd/11x/1st', name: '11x17 Hackpad Poster (Double-Sided)', description: '',
	images: ['https://cdn.example/front.jpg', 'https://cdn.example/back.jpg'],
	visible: true, sort_order: 1, max_per_request: 1, min_per_request: 1, quick_picks: [], max_per_user: null, width_in: 11, height_in: 17, variants: [], finish: 'matte', thickness_in: null, weight_oz: null, photos: [], packs: '', binding: '', pages: [], ships_with: [], club_only: false, clip: ''
};

describe('item URLs', () => {
	it('puts the id first and a readable slug after it', () => {
		expect(itemPath(poster)).toBe('/items/681-11x17-hackpad-poster-double-sided');
		expect(itemPath({ ...poster, name: '¡¡¡' })).toBe('/items/681');
	});

	it('reads the id back from the slug, so renamed items keep working', () => {
		expect(itemIdFromParam('681-11x17-hackpad-poster-double-sided')).toBe(681);
		expect(itemIdFromParam('681-an-old-name')).toBe(681);
		expect(itemIdFromParam('681')).toBe(681);
		expect(itemIdFromParam('hackpad')).toBeNull();
		expect(itemIdFromParam('0-zero')).toBeNull();
	});
});

describe('thumbnail', () => {
	it('is the first image, or empty', () => {
		expect(thumbnail(poster)).toBe('https://cdn.example/front.jpg');
		expect(thumbnail({ ...poster, images: [] })).toBe('');
	});

	it('falls back to a booklet\'s cover', () => {
		expect(thumbnail({ ...poster, images: [], pages: ['https://cdn.example/cover.jpg'] })).toBe('https://cdn.example/cover.jpg');
	});
});

describe('booklets', () => {
	const mini = { ...poster, binding: 'staple', pages: ['https://cdn.example/p1.jpg', 'https://cdn.example/p2.jpg'] };

	it('flips through only when bound and given pages', () => {
		expect(isBooklet(mini)).toBe(true);
		expect(isBooklet({ ...mini, binding: '' })).toBe(false);
		expect(isBooklet({ ...mini, pages: [mini.pages[0]] })).toBe(false);
	});

	it('orders uploaded page files by their numbers', () => {
		const files = ['mag-10.jpg', 'mag-2.jpg', 'mag-1.jpg'].map((name) => ({ name }));
		expect(byFilename(files).map((f) => f.name)).toEqual(['mag-1.jpg', 'mag-2.jpg', 'mag-10.jpg']);
	});

	it('finds what an item ships with by SKU', () => {
		const card = { ...poster, id: 9, sku: 'Pri/M25/4x6/1st' };
		const mag = { ...poster, ships_with: ['Pri/M25/4x6/1st', 'Gone/1'] };
		expect(shipsWith(mag, [poster, card])).toEqual([card]);
	});
});

describe('moveImage', () => {
	it('swaps an image with its neighbor and ignores moves off either end', () => {
		const imgs = ['a', 'b', 'c'];
		expect(moveImage(imgs, 2, -1)).toEqual(['a', 'c', 'b']);
		expect(moveImage(imgs, 0, 1)).toEqual(['b', 'a', 'c']);
		expect(moveImage(imgs, 0, -1)).toEqual(['a', 'b', 'c']);
		expect(moveImage(imgs, 2, 1)).toEqual(['a', 'b', 'c']);
		expect(imgs).toEqual(['a', 'b', 'c']);
	});
});

describe('formatUnitCost', () => {
	it('shows dollars with at least cents, keeping sub-cent precision', () => {
		expect(formatUnitCost('0.25')).toBe('$0.25');
		expect(formatUnitCost('3')).toBe('$3.00');
		expect(formatUnitCost('0.0832')).toBe('$0.0832');
		expect(formatUnitCost('1250.5')).toBe('$1,250.50');
	});

	it('is empty when the warehouse has no cost', () => {
		expect(formatUnitCost(null)).toBe('');
		expect(formatUnitCost('')).toBe('');
		expect(formatUnitCost('n/a')).toBe('');
	});
});

describe('unitCostLabel', () => {
	it('shows one cost, or a range when sizes cost different amounts', () => {
		expect(unitCostLabel({ in_stock: 5, unit_cost: '0.3682', unit_cost_max: null, variants: [] })).toBe('$0.3682 each');
		expect(unitCostLabel({ in_stock: 309, unit_cost: '13.8166', unit_cost_max: '14.2682', variants: [] })).toBe('$13.8166–$14.2682 each');
		expect(unitCostLabel({ in_stock: 0, unit_cost: null, unit_cost_max: null, variants: [] })).toBe('');
	});
});

describe('sizeStockLabel', () => {
	it('lists each size with its count, or nothing for one-size items', () => {
		const variants = [
			{ sku: 'Swa/Shirt/HC/S', label: 'S', in_stock: 92, unit_cost: null },
			{ sku: 'Swa/Shirt/HC/XL', label: 'XL', in_stock: 1122, unit_cost: null }
		];
		expect(sizeStockLabel({ in_stock: 1214, unit_cost: null, unit_cost_max: null, variants })).toBe('S 92 · XL 1,122');
		expect(sizeStockLabel({ in_stock: 5, unit_cost: null, unit_cost_max: null, variants: [] })).toBe('');
	});
});

describe('tilePreview', () => {
	it('shows a sized print as a sheet with its front, back, thickness and finish, without the phone', () => {
		expect(tilePreview(poster)).toEqual({
			sheet: {
				front: 'https://cdn.example/front.jpg', back: 'https://cdn.example/back.jpg',
				widthIn: 11, heightIn: 17, thicknessIn: 0.008, finish: 'matte', phone: false
			}
		});
	});

	it('flips through a booklet on its own, without the phone or what ships with it', () => {
		const mag = { ...poster, images: [], binding: 'staple', pages: ['p1', 'p2', 'p3', 'p4'], width_in: 5.5, height_in: 8.5, ships_with: ['Pri/M25/4x6/1st'] };
		expect(tilePreview(mag)).toEqual({
			book: { pages: ['p1', 'p2', 'p3', 'p4'], widthIn: 5.5, heightIn: 8.5, finish: 'matte', binding: 'staple', thicknessIn: 0.016, extras: [], phone: false }
		});
		expect(tilePreview({ ...mag, thickness_in: 0.1 })).toMatchObject({ book: { thicknessIn: 0.1 } });
	});

	it('folds a trifold brochure: its outside and inside art on one sheet, folded in thirds', () => {
		const brochure = { ...poster, binding: 'trifold', width_in: 11, height_in: 8.5, thickness_in: 0.005 };
		expect(tilePreview(brochure)).toEqual({
			fold: {
				outside: 'https://cdn.example/front.jpg', inside: 'https://cdn.example/back.jpg',
				widthIn: 11, heightIn: 8.5, thicknessIn: 0.005, finish: 'matte', phone: false
			}
		});
		expect(productPreview(brochure)).toMatchObject({ fold: { phone: true } });
		// Without its inside art, it's just a sheet.
		expect(tilePreview({ ...brochure, images: ['https://cdn.example/front.jpg'] })).toMatchObject({ sheet: {} });
	});

	it('plays the clip when there is one', () => {
		expect(tilePreview({ ...poster, clip: 'https://cdn.example/flip.mp4' })).toEqual({ clip: 'https://cdn.example/flip.mp4' });
	});

	it('shows a still photo clip, and the 3D view on hover when the item has one', () => {
		const tee = { ...poster, width_in: null, height_in: null, clip: 'https://cdn.example/wearing.jpg', variants: [{ sku: 'Swa/Shirt/CF/M', label: 'M' }] };
		expect(tilePreview(tee)).toEqual({
			clip: 'https://cdn.example/wearing.jpg',
			hover: { shirt: { kind: 'shirt', front: 'https://cdn.example/front.jpg', back: 'https://cdn.example/back.jpg', size: 'M', phone: false } }
		});
		// A video already moves on hover, and an item with no 3D view keeps just its photo.
		expect(tilePreview({ ...tee, clip: 'https://cdn.example/wearing.mp4' })).toEqual({ clip: 'https://cdn.example/wearing.mp4' });
		expect(tilePreview({ ...tee, variants: [] })).toEqual({ clip: 'https://cdn.example/wearing.jpg' });
	});

	it('shows stickers as stickers, even without a size', () => {
		expect(tilePreview({ ...poster, packs: 'sticker', width_in: null, height_in: null })).toEqual({
			stickers: { kind: 'stickers', front: 'https://cdn.example/front.jpg', widthIn: null, heightIn: null, phone: false }
		});
	});

	it('is null without a size or without art', () => {
		expect(tilePreview({ ...poster, width_in: null })).toBeNull();
		expect(tilePreview({ ...poster, images: [] })).toBeNull();
		expect(tilePreview({ ...poster, packs: 'sticker', images: [] })).toBeNull();
	});
});

describe('productPreview', () => {
	const pin: Item = {
		...poster, sku: 'Swa/HC/Pin/1st', name: 'Hack Club enamel pin', images: ['https://cdn.example/pin.jpg'],
		width_in: 2, height_in: 1, thickness_in: 0.375, finish: 'enamel'
	};

	it('shows an enamel pin as a pin, not a sheet of paper', () => {
		expect(productPreview(pin)).toEqual({ pin: { kind: 'pin', front: 'https://cdn.example/pin.jpg', widthIn: 2, heightIn: 1, phone: true } });
		expect(tilePreview(pin)).toEqual({ pin: { kind: 'pin', front: 'https://cdn.example/pin.jpg', widthIn: 2, heightIn: 1, phone: false } });
		expect(productPreview({ ...pin, width_in: null, height_in: null })).toMatchObject({ pin: { widthIn: null, heightIn: null } });
		expect(productPreview({ ...pin, images: [] })).toBeNull();
	});

	it('builds the product-page 3D view with a phone and any ships_with extras, ignoring clips', () => {
		const card: Item = { ...poster, id: 9, sku: 'Pri/M25/4x6/1st', width_in: 6, height_in: 4, thickness_in: 0.014, weight_oz: null, finish: 'linen' };
		const mag: Item = { ...poster, images: [], binding: 'perfect', pages: ['p1', 'p2', 'p3', 'p4'], width_in: 5.5, height_in: 8.5, ships_with: ['Pri/M25/4x6/1st'], clip: 'https://cdn.example/flip.mp4' };
		expect(productPreview(mag, [card])).toEqual({
			book: {
				pages: ['p1', 'p2', 'p3', 'p4'], widthIn: 5.5, heightIn: 8.5, finish: 'matte', binding: 'perfect', thicknessIn: 0.016, phone: true,
				extras: [{ front: 'https://cdn.example/front.jpg', back: 'https://cdn.example/back.jpg', widthIn: 6, heightIn: 4, thicknessIn: 0.014, finish: 'linen' }]
			}
		});
		expect(productPreview({ ...poster, packs: 'postcard' })).toMatchObject({ postcards: { kind: 'postcards', phone: true } });
		expect(productPreview({ ...poster, packs: 'sticker', width_in: null, height_in: null })).toMatchObject({ stickers: { kind: 'stickers', phone: true } });
	});
});

describe('shirts', () => {
	const tee: Item = {
		...poster, sku: 'Swa/Shirt/CF', width_in: null, height_in: null,
		images: ['https://cdn.example/tee-front.jpg', 'https://cdn.example/tee-back.jpg', 'https://cdn.example/event.jpg'],
		variants: ['S', 'M', 'L', 'XL'].map((label) => ({ label, sku: `Swa/Shirt/CF/${label}` }))
	};

	it('stand in 3D, front and back, at size M beside a phone', () => {
		expect(productPreview(tee)).toEqual({
			shirt: { kind: 'shirt', front: 'https://cdn.example/tee-front.jpg', back: 'https://cdn.example/tee-back.jpg', size: 'M', phone: true }
		});
		expect(tilePreview(tee)).toEqual({ shirt: { kind: 'shirt', front: 'https://cdn.example/tee-front.jpg', back: 'https://cdn.example/tee-back.jpg', size: 'M', phone: false } });
		expect(tilePreview({ ...tee, clip: 'https://cdn.example/worn.jpg' })).toEqual({ clip: 'https://cdn.example/worn.jpg', hover: tilePreview(tee) });
	});

	it('show the middle size when there is no M, and need a photo', () => {
		const kids = { ...tee, variants: ['XS', 'S', 'L'].map((label) => ({ label, sku: `K/${label}` })) };
		expect(productPreview(kids)).toMatchObject({ shirt: { size: 'S' } });
		expect(productPreview({ ...tee, images: [] })).toBeNull();
		expect(productPreview({ ...tee, images: ['https://cdn.example/tee-front.jpg'] })).toMatchObject({ shirt: { back: undefined } });
	});

	it('don’t stack up in 3D in the shop beside their clip', () => {
		expect(showsAmountIn3D({ ...tee, clip: 'https://cdn.example/worn.jpg' })).toBe(false);
	});
});

describe('previewCount', () => {
	const sticker = { ...poster, packs: 'sticker' };
	it('shows a sealed bag of 50 stickers until you pick a quantity, then what you picked', () => {
		expect(previewCount(sticker, 0)).toBe(50);
		expect(previewCount(sticker, 3)).toBe(3);
		expect(previewCount(sticker, 150)).toBe(150);
	});
	it('is what you picked for everything else', () => {
		expect(previewCount(poster, 0)).toBe(0);
		expect(previewCount(poster, 2)).toBe(2);
	});
});

describe('showsAmountIn3D', () => {
	it('booklets, and items with a clip and measurements, show the amount picked stacked up in 3D', () => {
		const book: Item = { ...poster, binding: 'perfect', pages: ['1.jpg', '2.jpg'], width_in: 5.83, height_in: 8.27 };
		expect(showsAmountIn3D(book)).toBe(true);
		expect(showsAmountIn3D({ ...book, binding: 'staple', clip: 'flip.gif' })).toBe(true);
		expect(showsAmountIn3D({ ...poster, clip: 'loop.mp4', width_in: 11, height_in: 17 })).toBe(true);
		// A plain print keeps its turning tile; nothing measured can't be stacked.
		expect(showsAmountIn3D({ ...poster, width_in: 11, height_in: 17 })).toBe(false);
		expect(showsAmountIn3D({ ...poster, clip: 'loop.mp4', width_in: null, height_in: null })).toBe(false);
	});
});

describe('cardSides', () => {
	const card: Item = { ...poster, packs: 'postcard', width_in: 6, height_in: 4 };
	it('gives a card\'s front and back, for the hover preview', () => {
		expect(cardSides(card)).toEqual(['https://cdn.example/front.jpg', 'https://cdn.example/back.jpg']);
	});
	it('is null for one-sided cards, cards printed the same both sides, and anything else', () => {
		expect(cardSides({ ...card, images: ['https://cdn.example/front.jpg'] })).toBeNull();
		expect(cardSides({ ...card, images: ['https://cdn.example/a.jpg', 'https://cdn.example/a.jpg'] })).toBeNull();
		expect(cardSides(poster)).toBeNull();
	});
});

describe('previewKind', () => {
	it('names the kind of 3D view', () => {
		expect(previewKind({ shirt: {} } as never)).toBe('shirt');
		expect(previewKind({ book: {} } as never)).toBe('book');
	});
});
