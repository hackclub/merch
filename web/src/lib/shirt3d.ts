// A shop-folded T-shirt mesh (shirt.ts has the shape): a soft pillow of cloth
// with rounded folds, sloped shoulders and a rib collar, its top printed with the
// chest of the shirt's product photo. Units are inches.
import * as THREE from 'three';
import { foldedShirtTop, shirtCrop } from './shirt';
import { maskBounds, stickerMask } from './stickers';

const STEP = 0.1; // grid spacing, inches: fine enough for the rib and the rounded folds

/**
 * One folded shirt, w × d and t thick, its bottom on y = 0. uv runs 0…1 across
 * the top (v = 1 at the collar), for shirtLook's crop of the photo; the color
 * attribute darkens the inside of the neck.
 */
export function foldedShirtGeometry(w: number, d: number, t: number): THREE.BufferGeometry {
	const nx = Math.ceil(w / STEP), nz = Math.ceil(d / STEP);
	const pos: number[] = [], uv: number[] = [], color: number[] = [], inside: boolean[] = [];
	for (let j = 0; j <= nz; j++)
		for (let i = 0; i <= nx; i++) {
			const x = -w / 2 + (w * i) / nx, z = -d / 2 + (d * j) / nz;
			const top = foldedShirtTop(x, z, w, d, t);
			pos.push(x, top.y, z);
			uv.push(i / nx, 1 - j / nz);
			color.push(top.shade, top.shade, top.shade);
			inside.push(top.inside);
		}
	// Only the cells on the shirt; past the shoulders there's nothing.
	const index: number[] = [];
	const at = (i: number, j: number) => j * (nx + 1) + i;
	for (let j = 0; j < nz; j++)
		for (let i = 0; i < nx; i++) {
			const a = at(i, j), b = at(i + 1, j), c = at(i, j + 1), e = at(i + 1, j + 1);
			if (inside[a] || inside[b] || inside[c]) index.push(a, c, b);
			if (inside[b] || inside[c] || inside[e]) index.push(b, c, e);
		}
	const geo = new THREE.BufferGeometry();
	geo.setAttribute('position', new THREE.Float32BufferAttribute(pos, 3));
	geo.setAttribute('uv', new THREE.Float32BufferAttribute(uv, 2));
	geo.setAttribute('color', new THREE.Float32BufferAttribute(color, 3));
	geo.setIndex(index);
	geo.computeVertexNormals();
	return geo;
}

/** Soft cotton in the shirt's color, with a little sheen at grazing angles. */
export function shirtMaterial(): THREE.MeshPhysicalMaterial {
	return new THREE.MeshPhysicalMaterial({
		color: 0x2b2c31, roughness: 0.92, sheen: 0.6, sheenRoughness: 0.7, vertexColors: true
	});
}

/**
 * From the product photo: the fabric's average color and a copy of the texture
 * cropped to the chest the folded top shows (shirtCrop), for w × d footprints.
 */
export function shirtLook(tex: THREE.Texture, w: number, d: number): { color: THREE.Color; map: THREE.Texture } {
	const img = tex.image as CanvasImageSource & { width: number; height: number };
	const N = 96;
	const c = document.createElement('canvas');
	c.width = c.height = N;
	const ctx = c.getContext('2d', { willReadFrequently: true })!;
	let color = new THREE.Color(0x2b2c31);
	let crop = shirtCrop(null, N, N, img.width / img.height, w / d);
	try {
		ctx.drawImage(img, 0, 0, N, N);
		const px = ctx.getImageData(0, 0, N, N).data;
		const mask = stickerMask(px, N, N);
		let r = 0, g = 0, b = 0, count = 0;
		for (let i = 0; i < mask.length; i++) {
			if (!mask[i]) continue;
			r += px[i * 4];
			g += px[i * 4 + 1];
			b += px[i * 4 + 2];
			count++;
		}
		if (count > 0) color = new THREE.Color().setRGB(r / count / 255, g / count / 255, b / count / 255, THREE.SRGBColorSpace);
		crop = shirtCrop(maskBounds(mask, N, N), N, N, img.width / img.height, w / d);
	} catch {
		// A cross-origin photo: keep the defaults.
	}
	// Texture v runs up from the bottom of the photo; ours runs up from the hem.
	const map = tex.clone();
	map.repeat.set(crop.w, crop.h);
	map.offset.set(crop.u0, 1 - crop.v0 - crop.h);
	map.needsUpdate = true;
	return { color, map };
}
