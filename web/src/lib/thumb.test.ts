import { describe, expect, it } from 'vitest';
import { thumb } from './thumb';

const url = 'https://cdn.hackclub.com/01a1/poster-front.jpg';
const at = (opts: string) => `https://cdn.hackclub.com/cdn-cgi/image/${opts}/01a1/poster-front.jpg`;

describe('thumb', () => {
	it('asks Cloudflare for the box at 1x, 2x and 3x, never enlarged, in AVIF or WebP when the browser takes it', () => {
		expect(thumb(url, { h: 176 })).toEqual({
			src: at('height=352,fit=scale-down,format=auto'),
			srcset: [
				`${at('height=176,fit=scale-down,format=auto')} 1x`,
				`${at('height=352,fit=scale-down,format=auto')} 2x`,
				`${at('height=528,fit=scale-down,format=auto')} 3x`
			].join(', ')
		});
	});

	it('fits within both sides when given both', () => {
		expect(thumb(url, { w: 120, h: 154 }).srcset).toContain(`${at('width=120,height=154,fit=scale-down,format=auto')} 1x`);
	});

	it('crops to fill the box for images shown with object-fit: cover', () => {
		expect(thumb(url, { w: 72, h: 48, fit: 'cover' }).srcset).toContain(`${at('width=144,height=96,fit=cover,format=auto')} 2x`);
	});

	it('leaves images on other hosts as they are', () => {
		expect(thumb('https://example.com/a.jpg', { h: 176 })).toEqual({ src: 'https://example.com/a.jpg' });
		expect(thumb('/og.jpg', { h: 176 })).toEqual({ src: '/og.jpg' });
	});
});
