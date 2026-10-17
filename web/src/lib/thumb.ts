/** Where Cloudflare resizes cdn.hackclub.com images on the fly (Image Transformations on the zone). */
const CDN = 'https://cdn.hackclub.com/';

/**
 * A cdn.hackclub.com image sized for a box of w × h CSS pixels: Cloudflare scales it down
 * (never up) for 1x, 2x and 3x screens and sends AVIF or WebP to browsers that take them.
 * The originals are up to 1600px, ten times what a strip slide or list thumbnail shows.
 * fit 'cover' crops to fill the box, for images shown with object-fit: cover.
 * Images on other hosts come back as they are.
 */
export function thumb(url: string, box: { w?: number; h?: number; fit?: 'cover' }): { src: string; srcset?: string } {
	if (!url.startsWith(CDN)) return { src: url };
	const at = (x: number) => {
		const opts = [box.w && `width=${box.w * x}`, box.h && `height=${box.h * x}`, `fit=${box.fit ?? 'scale-down'}`, 'format=auto'];
		return `${CDN}cdn-cgi/image/${opts.filter(Boolean).join(',')}/${url.slice(CDN.length)}`;
	};
	return { src: at(2), srcset: [1, 2, 3].map((x) => `${at(x)} ${x}x`).join(', ') };
}
