// T-shirts in 3D (shirt.ts has the shapes). A shop-folded shirt for the haul: a
// soft pillow of cloth with rounded folds, sloped shoulders and a rib collar, its
// top printed with the chest of the shirt's product photo. And a whole shirt for
// its product page (createShirtViewer), cut out of its front and back photos.
// Units are inches.
import * as THREE from 'three';
import { createPhone, createStage, type Viewer } from './paper3d';
import { GAP_IN, PHONE } from './scale';
import { clothDepth, edgeDistance, fabricColor, foldedShirtTop, isProductShot, shirtCrop, shirtSize } from './shirt';
import { maskBounds, stickerMask, type Box } from './stickers';

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

export type ShirtOptions = {
	kind: 'shirt';
	front: string; // a product shot of the front, on a plain backdrop
	back?: string; // the back, if it's a product shot too (else a plain back)
	size: string; // the size shown, e.g. "M"
	phone?: boolean; // stand a phone beside it for scale (default true)
};

const PHOTO_PX = 1024; // the photos are cut out at up to this many pixels across
const GRID_IN = 0.25; // vertex spacing
const ROUND_IN = 3.5; // the cloth rounds over this far in from its outline
const DEPTH_IN = 1.6; // and stands out this far on each side, like on an invisible mannequin

type Cutout = { px: Uint8ClampedArray; w: number; h: number; mask: Uint8Array; box: Box; shot: boolean };

function loadCutout(url: string): Promise<Cutout | null> {
	return new Promise((resolve) => {
		if (!url) return resolve(null);
		new THREE.ImageLoader().setCrossOrigin('anonymous').load(
			url,
			(img) => {
				const s = Math.min(1, PHOTO_PX / Math.max(img.width, img.height));
				const w = Math.max(1, Math.round(img.width * s)), h = Math.max(1, Math.round(img.height * s));
				const c = document.createElement('canvas');
				c.width = w;
				c.height = h;
				const ctx = c.getContext('2d', { willReadFrequently: true })!;
				ctx.drawImage(img, 0, 0, w, h);
				const px = ctx.getImageData(0, 0, w, h).data;
				const shot = isProductShot(px, w, h);
				let mask = stickerMask(px, w, h);
				let box = shot ? maskBounds(mask, w, h) : null;
				if (!box) {
					mask = new Uint8Array(w * h).fill(1);
					box = { x: 0, y: 0, w, h };
				}
				resolve({ px, w, h, mask, box, shot });
			},
			undefined,
			() => resolve(null)
		);
	});
}

/** A canvas of the cutout's box with everything off the shirt painted the fabric color, so edges don't bleed backdrop. */
function cutoutCanvas(c: Cutout, fabric: [number, number, number]): HTMLCanvasElement {
	const { box } = c;
	const out = document.createElement('canvas');
	out.width = box.w;
	out.height = box.h;
	const img = new ImageData(box.w, box.h);
	for (let y = 0; y < box.h; y++)
		for (let x = 0; x < box.w; x++) {
			const i = (box.y + y) * c.w + box.x + x, o = (y * box.w + x) * 4;
			if (c.mask[i]) img.data.set(c.px.subarray(i * 4, i * 4 + 3), o);
			else img.data.set(fabric, o);
			img.data[o + 3] = 255;
		}
	out.getContext('2d')!.putImageData(img, 0, 0);
	return out;
}

/**
 * The shirt's two faces: the front photo, and the back photo stretched onto the
 * front's outline and mirrored (we see it from behind), both cut out by the
 * front's outline. The cut sits a few pixels in, where the backdrop no longer
 * bleeds into the cloth, and fades over two pixels so alphaTest draws a smooth
 * edge instead of the photo's pixel steps; pale backdrop left near the edge is
 * painted the fabric color.
 */
function shirtTextures(front: Cutout, back: Cutout | null, dist: Float32Array, fabric: [number, number, number], renderer: THREE.WebGLRenderer) {
	const { box } = front;
	const face = (draw: (ctx: CanvasRenderingContext2D) => void) => {
		const c = document.createElement('canvas');
		c.width = box.w;
		c.height = box.h;
		const ctx = c.getContext('2d', { willReadFrequently: true })!;
		ctx.fillStyle = `rgb(${fabric.join(',')})`;
		ctx.fillRect(0, 0, box.w, box.h);
		draw(ctx);
		const img = ctx.getImageData(0, 0, box.w, box.h);
		const lum = (r: number, g: number, b: number) => (r + g + b) / 3;
		const fabricLum = lum(...fabric);
		for (let y = 0; y < box.h; y++)
			for (let x = 0; x < box.w; x++) {
				const d = dist[(box.y + y) * front.w + box.x + x], o = (y * box.w + x) * 4;
				img.data[o + 3] = Math.round(Math.min(1, Math.max(0, (d - 2) / 2)) * 255);
				if (d < 6 && lum(img.data[o], img.data[o + 1], img.data[o + 2]) > fabricLum + 60) img.data.set(fabric, o);
			}
		ctx.putImageData(img, 0, 0);
		const tex = new THREE.CanvasTexture(c);
		tex.colorSpace = THREE.SRGBColorSpace;
		tex.anisotropy = renderer.capabilities.getMaxAnisotropy();
		return tex;
	};
	const frontTex = face((ctx) => ctx.drawImage(cutoutCanvas(front, fabric), 0, 0));
	const backTex = face((ctx) => {
		if (!back?.shot) return; // a plain back
		ctx.translate(box.w, 0);
		ctx.scale(-1, 1);
		ctx.drawImage(cutoutCanvas(back, fabric), 0, 0, box.w, box.h);
	});
	return { frontTex, backTex };
}

/**
 * One side of the shirt standing on its hem: a grid over the photo's box,
 * pushed out (toward +z, or −z for the back) by how far each point is inside the
 * outline. Only cells on the shirt are kept; the texture's alpha trims the rest.
 */
function shirtSide(front: Cutout, dist: Float32Array, inPerPx: number, side: 1 | -1): THREE.BufferGeometry {
	const { box } = front;
	const W = box.w * inPerPx, H = box.h * inPerPx;
	const nx = Math.ceil(W / GRID_IN), ny = Math.ceil(H / GRID_IN);
	const pos: number[] = [], uv: number[] = [], on: boolean[] = [];
	for (let j = 0; j <= ny; j++)
		for (let i = 0; i <= nx; i++) {
			const u = i / nx, v = j / ny;
			const px = Math.min(front.w - 1, box.x + Math.round(u * (box.w - 1)));
			const py = Math.min(front.h - 1, box.y + Math.round(v * (box.h - 1)));
			const d = dist[py * front.w + px];
			const x = -W / 2 + u * W, y = H - v * H;
			// A soft ripple, so the cloth doesn't look inflated.
			const ripple = 0.06 * Math.sin(x * 0.9 + y * 0.35) * Math.sin(y * 0.7 - x * 0.25) * Math.min(1, (d * inPerPx) / ROUND_IN);
			pos.push(x, y, side * (clothDepth(d * inPerPx, ROUND_IN, DEPTH_IN) + ripple));
			uv.push(u, 1 - v);
			on.push(d > 0);
		}
	const index: number[] = [];
	const at = (i: number, j: number) => j * (nx + 1) + i;
	for (let j = 0; j < ny; j++)
		for (let i = 0; i < nx; i++) {
			const a = at(i, j), b = at(i + 1, j), c = at(i, j + 1), e = at(i + 1, j + 1);
			if (!(on[a] || on[b] || on[c] || on[e])) continue;
			// Wound to face out: +z for the front, −z for the back.
			if (side === 1) index.push(a, c, b, b, c, e);
			else index.push(a, b, c, b, e, c);
		}
	const geo = new THREE.BufferGeometry();
	geo.setAttribute('position', new THREE.Float32BufferAttribute(pos, 3));
	geo.setAttribute('uv', new THREE.Float32BufferAttribute(uv, 2));
	geo.setIndex(index);
	geo.computeVertexNormals();
	return geo;
}

/**
 * A T-shirt in 3D: cut out of its front product photo, with the cloth standing
 * out as if on an invisible mannequin, the back photo on its back, at the true
 * size of opts.size next to a phone. Turn it with angle, like a print.
 */
export function createShirtViewer(canvas: HTMLCanvasElement, opts: ShirtOptions, onReady: () => void): Viewer {
	const size = shirtSize(opts.size);
	const withPhone = opts.phone !== false;
	const H = size.length;
	let W = H * 1.1; // until the photo says how wide it is with its sleeves
	const scene = () => ({ w: withPhone ? W + GAP_IN + PHONE.w : W, h: Math.max(H, withPhone ? PHONE.h : 0) });
	const stage = createStage(canvas, scene().w, scene().h);
	const pivot = new THREE.Group();
	stage.scene.add(pivot);
	const phone = withPhone ? createPhone() : null;
	if (phone) stage.scene.add(phone);
	const place = () => {
		const left = -scene().w / 2;
		pivot.position.set(left + W / 2, 0, 0);
		phone?.position.set(left + W + GAP_IN + PHONE.w / 2, PHONE.h / 2, 0);
		stage.fitLight(scene().w, scene().h);
	};
	place();

	let disposed = false;
	Promise.all([loadCutout(opts.front), loadCutout(opts.back ?? '')]).then(([front, back]) => {
		if (disposed || !front) return onReady();
		const dist = edgeDistance(front.mask, front.w, front.h);
		const fabric = fabricColor(front.px, front.mask, dist, front.w);
		const inPerPx = H / front.box.h;
		W = front.box.w * inPerPx;
		const { frontTex, backTex } = shirtTextures(front, back, dist, fabric, stage.renderer);
		const cloth = (map: THREE.Texture) =>
			new THREE.MeshPhysicalMaterial({
				map, alphaTest: 0.5, roughness: 0.9, sheen: 0.5, sheenRoughness: 0.6,
				sheenColor: new THREE.Color().setRGB(...(fabric.map((c) => Math.min(1, c / 255 + 0.25)) as [number, number, number]), THREE.SRGBColorSpace)
			});
		for (const [side, tex] of [[1, frontTex], [-1, backTex]] as const) {
			const geo = shirtSide(front, dist, inPerPx, side);
			const mat = cloth(tex);
			const mesh = new THREE.Mesh(geo, mat);
			mesh.castShadow = true;
			pivot.add(mesh); // the stage disposes it
		}
		place();
		onReady();
	});

	const viewer: Viewer = {
		angle: 0,
		zoom: 1,
		resize: stage.resize,
		render() {
			pivot.rotation.y = viewer.angle;
			const { w, h } = scene();
			// Frame the whole scene at zoom 1; zooming moves toward the chest.
			const x = THREE.MathUtils.lerp(0, pivot.position.x, Math.min(1, (1 - viewer.zoom) * 1.6));
			stage.frame(x, w, h, viewer.zoom);
			stage.render();
		},
		dispose() {
			disposed = true;
			stage.dispose();
		}
	};
	return viewer;
}
