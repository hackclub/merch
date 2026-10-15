// WebGL view of a sticker request: die-cut stickers cut out of the product
// photo, fanned loose (3s and 10s) or stacked in clear zip bags of 50, lying
// on a table next to a phone for scale. setCount() follows the quantity
// stepper, so bags drop in as you ask for more. Units are inches.
import * as THREE from 'three';
import { RoundedBoxGeometry } from 'three/examples/jsm/geometries/RoundedBoxGeometry.js';
import { createPhone, createStage, PHONE_DEPTH, type Viewer } from './paper3d';
import { GAP_IN, PHONE } from './scale';
import { ACROSS, maskBounds, packLayout, PER_BAG, STICKER_THICKNESS_IN as T, stickerMask, stickerSize } from './stickers';

export type StickerOptions = {
	kind: 'stickers';
	front: string;
	widthIn: number | null;
	heightIn: number | null;
	phone?: boolean; // lay a phone beside them for scale (default true)
};

export type StickerViewer = Viewer & { setCount(n: number): void };

export const BAG_MARGIN = 0.25; // plastic around the stack on each side
export const ZIP_IN = 0.6; // extra bag length for the zip strip
export const BAG_H = PER_BAG * T + 0.04;
const PILE_GAP = 0.5;
const ELEVATION = THREE.MathUtils.degToRad(42); // camera looks down at the table
const DROP_IN = 4; // new bags fall from this high
const MASK_PX = 800; // the photo is cut out at up to this many pixels across

// Thin and clear: enough glint to read as a bag, without fogging the art inside.
const plasticMat = new THREE.MeshPhysicalMaterial({
	color: 0xffffff, transparent: true, opacity: 0.06, roughness: 0.12, clearcoat: 0.5, clearcoatRoughness: 0.08, envMapIntensity: 0.35,
	depthWrite: false
});
const zipMat = new THREE.MeshPhysicalMaterial({ color: 0xe9eef2, transparent: true, opacity: 0.55, roughness: 0.3, depthWrite: false });

export type StickerCutout = {
	art: THREE.MeshPhysicalMaterial;
	backing: THREE.MeshStandardMaterial;
	size: { w: number; h: number };
	face: THREE.PlaneGeometry;
	bagGeo: RoundedBoxGeometry;
	zipGeo: THREE.BoxGeometry;
	dispose(): void;
};

/**
 * Cuts a die-cut sticker out of its product photo (flooding in from the grey
 * backdrop and shadow) and prepares its vinyl face, white liner backing, and
 * zip-bag geometries at true size in inches.
 */
export function loadStickerCutout(
	renderer: THREE.WebGLRenderer,
	url: string,
	widthIn: number | null,
	heightIn: number | null,
	done: (cutout: StickerCutout | null) => void
) {
	new THREE.ImageLoader().setCrossOrigin('anonymous').load(
		url,
		(img) => {
			const scale = Math.min(1, MASK_PX / Math.max(img.width, img.height));
			const w = Math.max(1, Math.round(img.width * scale));
			const h = Math.max(1, Math.round(img.height * scale));
			const c = document.createElement('canvas');
			c.width = w;
			c.height = h;
			const ctx = c.getContext('2d', { willReadFrequently: true })!;
			ctx.drawImage(img, 0, 0, w, h);
			const px = ctx.getImageData(0, 0, w, h);
			const mask = stickerMask(px.data, w, h);
			let box = maskBounds(mask, w, h);
			// Nothing cut out (no backdrop to find): use the whole picture.
			if (!box || box.w * box.h > w * h * 0.98) {
				mask.fill(1);
				box = { x: 0, y: 0, w, h };
			}
			for (let i = 0; i < mask.length; i++) px.data[i * 4 + 3] = mask[i] ? 255 : 0;
			const cut = (white: boolean) => {
				const out = document.createElement('canvas');
				out.width = box.w;
				out.height = box.h;
				const o = out.getContext('2d')!;
				const crop = new ImageData(box.w, box.h);
				for (let y = 0; y < box.h; y++) {
					const row = px.data.subarray(((box.y + y) * w + box.x) * 4, ((box.y + y) * w + box.x + box.w) * 4);
					crop.data.set(row, y * box.w * 4);
				}
				if (white) for (let i = 0; i < crop.data.length; i += 4) crop.data.fill(255, i, i + 3);
				o.putImageData(crop, 0, 0);
				const tex = new THREE.CanvasTexture(out);
				tex.colorSpace = THREE.SRGBColorSpace;
				tex.anisotropy = renderer.capabilities.getMaxAnisotropy();
				return tex;
			};
			// Vinyl with a satin laminate: a soft sheen without washing out the print.
			const art = new THREE.MeshPhysicalMaterial({
				map: cut(false), roughness: 0.5, clearcoat: 0.25, clearcoatRoughness: 0.35, envMapIntensity: 0.45, alphaTest: 0.5,
				polygonOffset: true, polygonOffsetFactor: -1, polygonOffsetUnits: -2
			});
			const backing = new THREE.MeshStandardMaterial({ map: cut(true), color: 0xf3f1ec, roughness: 0.9, alphaTest: 0.5 });
			const size = stickerSize({ width_in: widthIn, height_in: heightIn }, box.w / box.h);
			const face = new THREE.PlaneGeometry(size.w, size.h).rotateX(-Math.PI / 2);
			const bagGeo = new RoundedBoxGeometry(size.w + 2 * BAG_MARGIN, BAG_H, size.h + 2 * BAG_MARGIN + ZIP_IN, 2, 0.015);
			const zipGeo = new THREE.BoxGeometry(size.w + 2 * BAG_MARGIN - 0.02, BAG_H + 0.02, 0.12);
			done({
				art,
				backing,
				size,
				face,
				bagGeo,
				zipGeo,
				dispose() {
					art.map?.dispose();
					art.dispose();
					backing.map?.dispose();
					backing.dispose();
					face.dispose();
					bagGeo.dispose();
					zipGeo.dispose();
				}
			});
		},
		undefined,
		() => done(null)
	);
}

/**
 * A stack of n die-cut stickers, bottom at y = 0, centered on x/z. Each sticker
 * is its artwork with the white backing just under it; stacked, the backings
 * make the white edge you see on a real bundle (backings stick out a hair). In
 * a tall vertical bundle (`topOnly`), only the top sticker's face is visible.
 */
function createStickerStack(cutout: StickerCutout, n: number, place: (i: number, m: THREE.Matrix4) => void, topOnly = false): THREE.Group {
	const g = new THREE.Group();
	const tops = new THREE.InstancedMesh(cutout.face, cutout.art, topOnly ? 1 : n);
	const backs = new THREE.InstancedMesh(cutout.face, cutout.backing, n * 3);
	backs.castShadow = true;
	const m = new THREE.Matrix4();
	const under = new THREE.Matrix4();
	for (let i = 0; i < n; i++) {
		place(i, m);
		if (!topOnly) tops.setMatrixAt(i, m);
		else if (i === n - 1) tops.setMatrixAt(0, m);
		for (const k of [0, 1, 2]) {
			under.makeScale(1.012, 1, 1.012).setPosition(0, -T * 0.25 * (k + 1), 0).premultiply(m);
			backs.setMatrixAt(i * 3 + k, under);
		}
	}
	g.add(backs, tops);
	return g;
}

/** A sealed clear zip-top bag of 50 die-cut stickers, ~15 mm tall. */
export function createStickerBag(cutout: StickerCutout, seed = 0): THREE.Group {
	const { size, bagGeo, zipGeo } = cutout;
	const g = createStickerStack(
		cutout,
		PER_BAG,
		(i, m) => {
			// Die-cut stacks are nearly, not perfectly, square.
			const j = Math.sin(seed * 31 + i * 12.9898) * 43758.5453;
			const jx = (j - Math.floor(j) - 0.5) * 0.03;
			m.makeTranslation(jx, (i + 1) * T + 0.01, ZIP_IN / 2 - jx);
		},
		true
	);
	const shell = new THREE.Mesh(bagGeo, plasticMat);
	shell.position.y = BAG_H / 2;
	shell.renderOrder = 2;
	const strip = new THREE.Mesh(zipGeo, zipMat);
	strip.position.set(0, BAG_H / 2, -(size.h + 2 * BAG_MARGIN + ZIP_IN) / 2 + ZIP_IN * 0.45);
	strip.renderOrder = 1;
	g.add(shell, strip);
	return g;
}

/**
 * Loose die-cut stickers (e.g. 3 or 10), dealt out like cards so you can count
 * them. `compact` keeps the fan tight enough to sit inside a pile's spot on a
 * shared table without overlapping neighboring piles.
 */
export function createStickerFan(cutout: StickerCutout, n: number, compact = false): THREE.Group {
	const { size } = cutout;
	const stepX = compact ? 0.14 : 0.32;
	const stepZ = compact ? 0.22 : 0.6;
	return createStickerStack(cutout, n, (i, m) => {
		const col = i % 5, row = Math.floor(i / 5);
		const turn = (col - (Math.min(n, 5) - 1) / 2) * 0.08 + row * 0.05;
		m.makeRotationY(turn).setPosition(
			(col - (Math.min(n, 5) - 1) / 2) * size.w * stepX,
			(i + 1) * T * 1.5,
			(row - (Math.ceil(n / 5) - 1) / 2) * size.h * stepZ
		);
	});
}

export function disposeStickerGroup(g: THREE.Group) {
	g.traverse((o) => o instanceof THREE.InstancedMesh && o.dispose());
}

export function createStickerViewer(canvas: HTMLCanvasElement, opts: StickerOptions, onReady: () => void): StickerViewer {
	const stage = createStage(canvas, 16, 16);
	const { scene, renderer, camera } = stage;

	// The packs turn on a turntable; the phone stays put beside them.
	const turntable = new THREE.Group();
	scene.add(turntable);

	const phone = createPhone(true);
	phone.rotation.z = THREE.MathUtils.degToRad(-10);
	const withPhone = opts.phone !== false;
	if (withPhone) scene.add(phone);

	let cutout: StickerCutout | null = null;
	let count = 0;
	// Bags on the table, oldest first; each remembers when it started falling.
	let bags: { group: THREE.Group; y: number; born: number }[] = [];
	let loose: THREE.Group | null = null;
	let levels = 0; // how high the bags are piled
	let extent = 3; // half-width of what's on the turntable, for framing
	let shownExtent = 3;

	function layout(now: number) {
		if (!cutout) return;
		const { size } = cutout;
		const plan = packLayout(count);
		const bagW = size.w + 2 * BAG_MARGIN;
		const piles = Math.min(plan.bags.length, ACROSS);
		levels = Math.ceil(plan.bags.length / ACROSS);
		const looseW = plan.loose ? size.w * (1 + 0.32 * (Math.min(plan.loose, 5) - 1)) : 0;
		const totalW = piles * bagW + Math.max(0, piles - 1) * PILE_GAP + (looseW && piles ? PILE_GAP : 0) + looseW;
		let x = -totalW / 2;

		// Keep bags that are still requested (so only new ones fall in).
		let fresh = 0;
		for (const b of bags.splice(plan.bags.length)) {
			turntable.remove(b.group);
			disposeStickerGroup(b.group);
		}
		plan.bags.forEach((p, i) => {
			if (!bags[i]) {
				const group = createStickerBag(cutout!, i);
				// A slight twist per bag, like they were dropped on the table.
				group.rotation.y = Math.sin(i * 2.3) * 0.07;
				turntable.add(group);
				bags[i] = { group, y: 0, born: now + fresh++ * 120 }; // one after another
			}
			bags[i].y = p.level * BAG_H;
			bags[i].group.position.x = x + p.pile * (bagW + PILE_GAP) + bagW / 2;
		});
		x += piles * (bagW + PILE_GAP);

		if (loose) {
			turntable.remove(loose);
			disposeStickerGroup(loose);
		}
		loose = plan.loose ? createStickerFan(cutout, plan.loose) : null;
		if (loose) {
			loose.position.x = x + looseW / 2;
			turntable.add(loose);
		}
		const rows = Math.ceil(plan.loose / 5);
		const depth = Math.max(piles ? size.h + 2 * BAG_MARGIN + ZIP_IN : 0, rows ? size.h * (1 + 0.6 * (rows - 1)) : 0);
		extent = Math.hypot(totalW, depth) / 2;
		stage.fitLight(extent * 2 + (withPhone ? PHONE.w + GAP_IN : 0), extent * 2);
	}

	loadStickerCutout(renderer, opts.front, opts.widthIn, opts.heightIn, (loaded) => {
		cutout = loaded;
		if (cutout) {
			// The first view shouldn't animate in.
			layout(-1e9);
			shownExtent = extent;
		}
		onReady();
	});

	const target = new THREE.Vector3();
	let last = performance.now();
	const viewer: StickerViewer = {
		angle: 0,
		zoom: 1,
		setCount(n) {
			if (n === count && (bags.length || loose)) return;
			count = n;
			layout(performance.now());
		},
		resize: stage.resize,
		render() {
			const now = performance.now();
			const dt = Math.min(0.05, (now - last) / 1000);
			last = now;
			turntable.rotation.y = viewer.angle;
			// New bags fall onto the pile with a little bounce.
			for (const b of bags) {
				const t = Math.min(1, Math.max(0, (now - b.born) / 450));
				const fall = 1 - easeOutBounce(t);
				b.group.position.y = b.y + fall * DROP_IN;
				b.group.visible = now >= b.born || b.born < 0;
			}
			// Reframe smoothly as the request grows or shrinks.
			shownExtent += (extent - shownExtent) * Math.min(1, dt * 4);
			if (Math.abs(extent - shownExtent) < 0.0005) shownExtent = extent;
			const r = shownExtent;
			phone.position.x = r + GAP_IN + PHONE.w / 2;
			phone.position.z = 0.4;
			const sceneW = withPhone ? 2 * r + GAP_IN + PHONE.w : 2 * r;
			const sceneD = withPhone ? Math.max(2 * r, PHONE.h) : 2 * r;
			const cx = withPhone ? (phone.position.x + PHONE.w / 2 - r) / 2 : 0;
			const vfov = THREE.MathUtils.degToRad(camera.fov) / 2;
			const fitW = (sceneW * 0.6) / (Math.tan(vfov) * camera.aspect);
			const fitD = (sceneD * Math.sin(ELEVATION) * 0.5 + levels * BAG_H) / Math.tan(vfov);
			const dist = Math.max(fitW, fitD) * viewer.zoom;
			camera.near = Math.max(0.5, dist * 0.1);
			camera.far = dist * 4;
			camera.updateProjectionMatrix();
			const zoomed = 1 - viewer.zoom;
			target.set(THREE.MathUtils.lerp(cx, 0, Math.min(1, zoomed * 1.6)), 0.2, 0);
			camera.position.set(target.x, target.y + dist * Math.sin(ELEVATION), dist * Math.cos(ELEVATION));
			camera.lookAt(target);
			stage.render();
		},
		dispose() {
			cutout?.dispose();
			stage.dispose();
		}
	};
	return viewer;
}

function easeOutBounce(t: number) {
	const n = 7.5625, d = 2.75;
	if (t < 1 / d) return n * t * t;
	if (t < 2 / d) return n * (t -= 1.5 / d) * t + 0.75;
	if (t < 2.5 / d) return n * (t -= 2.25 / d) * t + 0.9375;
	return n * (t -= 2.625 / d) * t + 0.984375;
}
