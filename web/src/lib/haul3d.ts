// Draws layoutHaul's table (haul.ts): every pile at true size — sealed clear zip
// bags of 50 die-cut stickers, fanned loose stickers, banded postcard bundles,
// booklet stacks with spines and page edges, folded T-shirts, and posters —
// next to a phone for scale, seen from above at an angle. Units are inches.
import * as THREE from 'three';
import { RoundedBoxGeometry } from 'three/examples/jsm/geometries/RoundedBoxGeometry.js';
import { bookletStack, SHIRT, tall, type HaulLayout, type Placed } from './haul';
import { finishInfo } from './paper';
import { createPhone, createStage, edgeColor, loadArt, pageEdges, paperBand, paperEdge, paperFace, PHONE_DEPTH } from './paper3d';
import { BAG_H, createStickerBag, createStickerFan, disposeStickerGroup, loadStickerCutout, type StickerCutout } from './sticker3d';
import { stickerMask } from './stickers';

export type HaulViewer = {
	/** Turn of the whole table, radians. */
	angle: number;
	/** How far above the table we look from, radians (drag up and down to change it). */
	elevation: number;
	/** 1 = everything in view; smaller is closer. */
	zoom: number;
	/** Updates what's on the table in place, reusing loaded art and sticker cutouts. */
	setTable(table: HaulLayout): void;
	/** What's under a point on the canvas (-1…1 each way): "100 × Mini-Magazine · 4 in tall", or null. */
	pick(x: number, y: number): string | null;
	resize(width: number, height: number): void;
	render(): void;
	dispose(): void;
};

export const ELEVATION = THREE.MathUtils.degToRad(34); // low enough that stack heights read
const TABLE_TOP = 1; // inches thick
// More booklets than this draw as one block (bigger piles are too far away to tell).
const MAX_BOOKLETS = 400;
const hitMat = new THREE.MeshBasicMaterial({ transparent: true, opacity: 0, depthWrite: false });

export function createHaulViewer(canvas: HTMLCanvasElement, initial: HaulLayout, onReady: () => void): HaulViewer {
	const stage = createStage(canvas, initial.width, Math.max(initial.depth, 12));
	const { scene, renderer, camera } = stage;
	const group = new THREE.Group();
	scene.add(group);

	// Lying flat, screen up; moved to its spot on each layout update.
	const phone = createPhone(true);
	phone.traverse((o) => (o.userData.label = 'A regular phone, about 6 in long, for scale'));
	group.add(phone);

	let contents = new THREE.Group();
	group.add(contents);
	let disposers: (() => void)[] = [];

	// Per-viewer caches so changing quantities or adding items reuses already-cut stickers and textures.
	const texCache = new Map<string, THREE.Texture | null | ((tex: THREE.Texture | null) => void)[]>();
	const cutoutCache = new Map<string, StickerCutout | null | ((c: StickerCutout | null) => void)[]>();
	const shirtColorCache = new Map<string, THREE.Color>();

	let generation = 0;
	let readyFired = false;

	function getArt(url: string, aspect: number, cb: (tex: THREE.Texture | null) => void) {
		const key = `${url}@${aspect.toFixed(4)}`;
		const cached = texCache.get(key);
		if (cached !== undefined && !Array.isArray(cached)) return cb(cached);
		if (Array.isArray(cached)) {
			cached.push(cb);
			return;
		}
		const waiters = [cb];
		texCache.set(key, waiters);
		loadArt(renderer, url, aspect, (tex) => {
			texCache.set(key, tex);
			for (const w of waiters) w(tex);
		});
	}

	function getCutout(url: string, widthIn: number | null, heightIn: number | null, cb: (c: StickerCutout | null) => void) {
		const key = `${url}@${widthIn ?? ''}x${heightIn ?? ''}`;
		const cached = cutoutCache.get(key);
		if (cached !== undefined && !Array.isArray(cached)) return cb(cached);
		if (Array.isArray(cached)) {
			cached.push(cb);
			return;
		}
		const waiters = [cb];
		cutoutCache.set(key, waiters);
		loadStickerCutout(renderer, url, widthIn, heightIn, (c) => {
			cutoutCache.set(key, c);
			for (const w of waiters) w(c);
		});
	}

	function clearContents() {
		group.remove(contents);
		for (const fn of disposers) fn();
		disposers = [];
		contents = new THREE.Group();
		group.add(contents);
	}

	let table = initial;
	let tallest = 0.5;
	let shown = { width: initial.width, depth: initial.depth, tallest: 0.5, room: initial.tables.length ? 0.56 : 0.52 };
	let firstLayout = true;

	function buildPile(p: Placed, index: number, gen: number, doneOne: () => void) {
		if (p.kind === 'sticker') {
			const holder = new THREE.Group();
			holder.position.set(p.x, 0, p.z);
			const hitH = p.bag ? BAG_H : Math.max(p.height, 0.28);
			const label = p.bag ? `${p.label} · ${tall(p.height)} · a pack of 50` : `${p.label} · loose stickers`;
			const hit = new THREE.Mesh(new THREE.BoxGeometry(p.w, hitH, p.d), hitMat);
			hit.position.y = hitH / 2;
			hit.userData.label = label;
			holder.add(hit);
			contents.add(holder);
			disposers.push(() => hit.geometry.dispose());

			if (!p.art) return doneOne();
			getCutout(p.art, p.widthIn ?? null, p.heightIn ?? null, (cutout) => {
				if (gen !== generation) return;
				if (cutout) {
					const mesh = p.bag ? createStickerBag(cutout, index) : createStickerFan(cutout, p.count ?? 1, true);
					if (p.bag) mesh.rotation.y = Math.sin(index * 2.3) * 0.06;
					mesh.traverse((o) => (o.userData.label = label));
					holder.add(mesh);
					disposers.push(() => disposeStickerGroup(mesh));
				}
				doneOne();
			});
			return;
		}

		if (p.kind === 'shirt') {
			const count = Math.max(1, p.count ?? 1);
			const unitH = p.height / count;
			const geo = new RoundedBoxGeometry(p.w, Math.max(0.1, unitH - 0.04), p.d, 3, 0.25);
			const cloth = new THREE.MeshStandardMaterial({ color: 0x2b2c31, roughness: 0.9 });
			const top = new THREE.MeshStandardMaterial({ color: 0xffffff, roughness: 0.88 });
			const label = `${p.label} · ${tall(p.height)}`;
			for (let i = 0; i < count; i++) {
				const isTop = i === count - 1;
				const mesh = new THREE.Mesh(geo, isTop ? [cloth, cloth, top, cloth, cloth, cloth] : cloth);
				mesh.position.set(p.x, (i + 0.5) * unitH, p.z);
				mesh.castShadow = true;
				mesh.userData.label = label;
				contents.add(mesh);
			}
			disposers.push(() => {
				geo.dispose();
				cloth.dispose();
				top.dispose();
			});
			if (!p.art) return doneOne();
			getArt(p.art, p.w / p.d, (tex) => {
				if (gen !== generation) return;
				if (tex) {
					top.map = tex;
					top.needsUpdate = true;
					let col = shirtColorCache.get(p.art);
					if (!col) {
						col = sampleShirtColor(tex);
						shirtColorCache.set(p.art, col);
					}
					cloth.color.copy(col);
				}
				doneOne();
			});
			return;
		}

		// Printed sheets, postcards, booklets, or generic boxes.
		const face = paperFace(p.finish ?? '', p.w, p.d);
		const bottom = paperEdge();
		const side = paperEdge();
		const cards = p.count ?? 1;
		if (cards > 1 || p.kind === 'booklet') {
			side.map = pageEdges(false);
			side.map.repeat.set(1, Math.max(2, p.height / 0.012) / 64);
		}
		const info = finishInfo(p.finish ?? '');
		const spine =
			p.kind === 'booklet'
				? new THREE.MeshPhysicalMaterial({
						color: 0xdddddd, roughness: info.roughness, clearcoat: info.clearcoat,
						polygonOffset: true, polygonOffsetFactor: 1, polygonOffsetUnits: 1
					})
				: side;

		const h = Math.max(p.height, 0.014);
		// BoxGeometry faces: +x (fore-edge), -x (spine), +y (top), -y (bottom), +z, -z.
		const faces = [side, spine, face.mat, bottom, side, side];
		const booklets = p.kind === 'booklet' && cards <= MAX_BOOKLETS;
		const geo = booklets ? bookletGeometry(p.w, h / cards, p.d) : new THREE.BoxGeometry(p.w, h, p.d);
		if (booklets) side.map!.repeat.set(1, Math.max(2, h / cards / 0.012) / 64); // page edges per booklet
		let pile: THREE.Mesh;
		if (booklets) {
			// One by one, each puffed at the fold and a little askew, so the pile shows they don't lie flat.
			const many = new THREE.InstancedMesh(geo, faces, cards);
			const m = new THREE.Matrix4();
			const turn = new THREE.Quaternion();
			const up = new THREE.Vector3(0, 1, 0);
			const one = new THREE.Vector3(1, 1, 1);
			bookletStack(cards, h / cards).forEach((b, i) => {
				m.compose(new THREE.Vector3(p.x + b.dx, b.y, p.z + b.dz), turn.setFromAxisAngle(up, b.yaw), one);
				many.setMatrixAt(i, m);
			});
			many.computeBoundingSphere();
			pile = many;
		} else {
			pile = new THREE.Mesh(geo, faces);
			pile.position.set(p.x, h / 2, p.z);
		}
		pile.castShadow = true;
		pile.userData.label = `${p.label} · ${tall(p.height)}`;
		contents.add(pile);

		if (p.band) {
			const band = paperBand(p.w, h, p.d, p.x, p.z);
			band.userData.label = `${pile.userData.label} · a banded bundle`;
			contents.add(band);
			disposers.push(() => {
				band.geometry.dispose();
				(band.material as THREE.Material).dispose();
			});
		}

		disposers.push(() => {
			geo.dispose();
			face.dispose();
			face.mat.dispose();
			bottom.dispose();
			side.map?.dispose();
			side.dispose();
			if (spine !== side) spine.dispose();
		});

		if (!p.art) return doneOne();
		getArt(p.art, p.w / p.d, (tex) => {
			if (gen !== generation) return;
			if (tex) {
				face.mat.map = tex;
				face.mat.needsUpdate = true;
				if (p.kind === 'booklet' && spine !== side) spine.color.copy(edgeColor(tex));
			}
			doneOne();
		});
	}

	function applyTable(next: HaulLayout) {
		const gen = ++generation;
		clearContents();
		table = next;
		tallest = Math.max(0.5, ...table.piles.map((p) => (p.bag ? BAG_H : p.height)));
		stage.fitLight(table.width, Math.max(table.depth, tallest, 12));

		let pending = 1;
		const doneOne = () => {
			if (gen !== generation) return;
			if (--pending === 0 && !readyFired) {
				readyFired = true;
				onReady();
			}
		};

		table.piles.forEach((p, i) => {
			pending++;
			buildPile(p, i, gen, doneOne);
		});

		// Folding tables: a white plastic top with a grey rim; the piles sit on it.
		for (const t of table.tables) {
			const top = new THREE.MeshStandardMaterial({ color: 0xf6f5f1, roughness: 0.85 });
			const rim = new THREE.MeshStandardMaterial({ color: 0xb9b7b0, roughness: 0.7 });
			const geo = new THREE.BoxGeometry(t.w, TABLE_TOP, t.d);
			const slab = new THREE.Mesh(geo, [rim, rim, top, rim, rim, rim]);
			slab.position.set(t.x, -TABLE_TOP / 2 - 0.01, t.z);
			slab.receiveShadow = true;
			contents.add(slab);
			disposers.push(() => {
				geo.dispose();
				top.dispose();
				rim.dispose();
			});
		}

		phone.position.set(table.phone.x, PHONE_DEPTH / 2, table.phone.z);
		if (firstLayout) {
			shown = { width: table.width, depth: table.depth, tallest, room: table.tables.length ? 0.56 : 0.52 };
			firstLayout = false;
		}
		doneOne();
	}

	applyTable(initial);

	const ray = new THREE.Raycaster();
	let last = performance.now();

	const viewer: HaulViewer = {
		angle: 0,
		elevation: ELEVATION,
		zoom: 1,
		setTable(next) {
			applyTable(next);
		},
		pick(x, y) {
			ray.setFromCamera(new THREE.Vector2(x, y), camera);
			const hit = ray.intersectObjects(group.children, true).find((h) => h.object.userData.label);
			return hit ? hit.object.userData.label : null;
		},
		resize: stage.resize,
		render() {
			const now = performance.now();
			const dt = Math.min(0.05, (now - last) / 1000);
			last = now;
			const ease = Math.min(1, dt * 8);
			const wantRoom = table.tables.length ? 0.56 : 0.52;
			shown.width += (table.width - shown.width) * ease;
			shown.depth += (table.depth - shown.depth) * ease;
			shown.tallest += (tallest - shown.tallest) * ease;
			shown.room += (wantRoom - shown.room) * ease;

			group.rotation.y = viewer.angle;
			stage.frameTable(shown.width, shown.depth, shown.tallest, viewer.zoom, {
				elevation: viewer.elevation,
				room: shown.room,
				y: shown.tallest * 0.3
			});
			stage.render();
		},
		dispose() {
			clearContents();
			for (const v of texCache.values()) if (v && !Array.isArray(v)) v.dispose();
			texCache.clear();
			for (const v of cutoutCache.values()) if (v && !Array.isArray(v)) v.dispose();
			cutoutCache.clear();
			stage.dispose();
		}
	};
	return viewer;
}

/** Samples a T-shirt's fabric color from the non-backdrop pixels of its product photo. */
function sampleShirtColor(tex: THREE.Texture): THREE.Color {
	const c = document.createElement('canvas');
	const N = 48;
	c.width = c.height = N;
	const ctx = c.getContext('2d', { willReadFrequently: true })!;
	const img = tex.image as CanvasImageSource & { width: number; height: number };
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
		if (count > 0) return new THREE.Color().setRGB(r / count / 255, g / count / 255, b / count / 255, THREE.SRGBColorSpace);
	} catch {
		// Cross-origin fallback
	}
	return new THREE.Color(0x2b2c31);
}

/**
 * One bound booklet, w × d, its bottom on y = 0 and its spine at -x: full
 * thickness at the fold, sagging toward the middle and springing up a little at
 * the open edge, the way a stapled booklet sits. Stacked, they leave wedges of
 * air between them.
 */
function bookletGeometry(w: number, t: number, d: number): THREE.BufferGeometry {
	const geo = new THREE.BoxGeometry(w, t, d, 12, 1, 1);
	geo.translate(0, t / 2, 0);
	const pos = geo.attributes.position;
	for (let i = 0; i < pos.count; i++) {
		if (pos.getY(i) < t / 2) continue;
		const u = (pos.getX(i) + w / 2) / w; // 0 at the spine, 1 at the open edge
		pos.setY(i, t * (0.74 + 0.26 * (1 - u) ** 2 + 0.12 * u ** 3));
	}
	geo.computeVertexNormals();
	return geo;
}
