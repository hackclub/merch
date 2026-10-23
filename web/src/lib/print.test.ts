import { describe, expect, it } from 'vitest';
import { checkArt, firstShipmentOptions, guides, printArtProps, estimateLine, arrivalLine, timelineRows, sheetFor, templateSVG, trimCrop, type PrintEstimate, type PrintProduct } from './print';

const postcard: PrintProduct = {
	key: 'postcard_4x6', name: '4x6 postcard', paper: 'Premium linen card stock', vendor: 'Vistaprint', width_in: 4, height_in: 6,
	bleed_in: 0.125, safe_in: 0.125, dpi: 300, finish: 'linen', thickness_in: 0.016, postcard: true, min_quantity: 50, max_quantity: 25000,
	step: 50, bleed_width_in: 4.25, bleed_height_in: 6.25, safe_width_in: 3.75, safe_height_in: 5.75, bleed_width_px: 1275, bleed_height_px: 1875
};

const estimate: PrintEstimate = {
	product: 'postcard_4x6', quantity: 1000, print_cents: 8259, shipping_cents: 3699, total_cents: 11958, each_cents: 11.958,
	review_days: 2, to_warehouse_days: [4, 9], to_you_days: [0, 0], min_days: 6, max_days: 11, ready_from: '2026-11-02', ready_by: '2026-11-09'
};
const delivered: PrintEstimate = { ...estimate, to_you_days: [4, 7], arrive_from: '2026-11-06', arrive_by: '2026-11-18' };

describe('checkArt (mirrors swag.CheckArt)', () => {
	it('takes full-bleed art either way round', () => {
		expect(checkArt(postcard, 1275, 1875)).toEqual({ fit: 'bleed', dpi: 300 });
		expect(checkArt(postcard, 1885, 1282)).toEqual({ fit: 'bleed', dpi: 301 });
	});
	it('warns about art without bleed', () => {
		const trim = checkArt(postcard, 3000, 2000);
		expect(trim.fit).toBe('trim');
		expect(trim.warning).toMatch(/no bleed/i);
		expect(trim.problem).toBeUndefined();
	});
	it('refuses the wrong shape, and anything under 300 DPI', () => {
		expect(checkArt(postcard, 2000, 2000).problem).toMatch(/1275 x 1875 px/);
		expect(checkArt(postcard, 850, 1250).problem).toMatch(/200 DPI.*at least 300/);
		expect(checkArt(postcard, 1274, 1874).problem).toMatch(/299 DPI/);
		expect(checkArt(postcard, 800, 1200).problem).toMatch(/200 DPI/); // trim size, too small
	});
});

describe('trimCrop', () => {
	it('cuts the bleed off for the 3D view', () => {
		expect(trimCrop(postcard, 1875, 1275)).toEqual({ sx: 37.5, sy: 37.5, sw: 1800, sh: 1200 });
	});
	it('keeps art already at the trim size whole', () => {
		expect(trimCrop(postcard, 3000, 2000)).toEqual({ sx: 0, sy: 0, sw: 3000, sh: 2000 });
	});
});

describe('guides', () => {
	it('puts the trim ⅛" in from the bleed and the safe line ⅛" further', () => {
		const g = guides(postcard, 1875, 1275);
		expect(g.trim.x).toBeCloseTo(2);
		expect(g.trim.y).toBeCloseTo(2.941, 2);
		expect(g.safe.x).toBeCloseTo(4);
		// ⅛" of the 6" x 4" trimmed card.
		expect(g.safeInTrim.x).toBeCloseTo(2.083, 2);
		expect(g.safeInTrim.y).toBeCloseTo(3.125);
	});
	it('has no bleed to cut on art at the trim size', () => {
		const g = guides(postcard, 3000, 2000);
		expect(g.trim).toEqual({ x: 0, y: 0 });
		expect(g.safe.y).toBeCloseTo(3.125);
	});
});

describe('sheetFor', () => {
	it('turns the sheet to match the art', () => {
		expect(sheetFor(postcard, 1875, 1275)).toEqual({ widthIn: 6, heightIn: 4 });
		expect(sheetFor(postcard, 1275, 1875)).toEqual({ widthIn: 4, heightIn: 6 });
	});
});

describe('templateSVG', () => {
	it('draws the bleed, trim and safe lines at full size', () => {
		const svg = templateSVG(postcard, true);
		expect(svg).toContain('width="6.25in" height="4.25in"');
		expect(svg).toContain('viewBox="0 0 1875 1275"');
		expect(svg).toMatch(/<rect x="37.5" y="37.5" width="1800" height="1200"/); // trim
		expect(svg).toMatch(/<rect x="75" y="75" width="1725" height="1125"/); // safe
		expect(svg).toContain('6.25" x 4.25"');
	});
	it("doesn't name the printer, and asks for a PNG", () => {
		const svg = templateSVG({ ...postcard, vendor: 'Vistaprint Standard Postcards' }, false);
		expect(svg).not.toMatch(/vistaprint/i);
		expect(svg).not.toMatch(/jpe?g/i);
		expect(svg).toContain('Export a PNG');
	});
});

describe('firstShipmentOptions', () => {
	it('offers postcards in the packs the shop sends, up to the run', () => {
		expect(firstShipmentOptions(postcard, 1000)).toEqual([0, 5, 10, 15, 20, 25, 50, 100, 150, 200, 250]);
		expect(firstShipmentOptions(postcard, 100)).toEqual([0, 5, 10, 15, 20, 25, 50, 100]);
	});
	it('offers posters a few at a time', () => {
		expect(firstShipmentOptions({ ...postcard, postcard: false }, 25)).toEqual([0, 1, 2, 3, 5, 10, 15, 20, 25]);
	});
});

describe('estimate text', () => {
	it('gives the total and the price of each', () => {
		expect(estimateLine(estimate)).toBe('About $119.58 for 1,000 (12¢ each)');
	});
	it('lays out each leg: review and order, Vistaprint to our warehouse, our warehouse to them', () => {
		expect(timelineRows(delivered)).toEqual([
			['Review and order from Vistaprint', 'Within 48 hours'],
			['Vistaprint prints and ships to our warehouse', '4–9 business days'],
			['Our warehouse to you', '4–7 business days']
		]);
		// No first shipment: it ends at the warehouse.
		expect(timelineRows(estimate).map((r) => r[0])).toEqual(['Review and order from Vistaprint', 'Vistaprint prints and ships to our warehouse']);
	});
	it('gives when the first ones reach them, or else when the run is in the warehouse', () => {
		expect(arrivalLine(delivered)).toBe('Your first ones should arrive about Nov 6–18.');
		expect(arrivalLine(estimate)).toBe('In our warehouse about Nov 2–9.');
	});
});

describe('printArtProps', () => {
	it('logs how a file fits, never its name', () => {
		expect(printArtProps('postcard_4x6', 'front', checkArt(postcard, 3000, 2000), 4_911_298)).toEqual({
			product: 'postcard_4x6', side: 'front', fit: 'trim', dpi: 500, problem: false, warning: true, mb: 4.9
		});
		expect(printArtProps('postcard_4x6', 'back', null, 0, 'application/pdf')).toEqual({
			product: 'postcard_4x6', side: 'back', fit: 'unreadable', problem: true, type: 'application/pdf'
		});
	});
});
