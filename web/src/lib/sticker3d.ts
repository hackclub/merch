// WebGL view of a sticker request: die-cut stickers cut out of the product
// photo, fanned loose (3s and 10s) or stacked in clear zip bags of 50, lying
// on a table next to a phone for scale. setCount() follows the quantity
// stepper, so bags drop in as you ask for more. Units are inches.
import * as THREE from 'three';
import { RoomEnvironment } from 'three/examples/jsm/environments/RoomEnvironment.js';
import { RoundedBoxGeometry } from 'three/examples/jsm/geometries/RoundedBoxGeometry.js';
import type { Viewer } from './paper3d';
import { GAP_IN, PHONE } from './scale';
import { ACROSS, maskBounds, packLayout, PER_BAG, STICKER_THICKNESS_IN as T, stickerMask, stickerSize } from './stickers';

export type StickerOptions = {
	kind: 'stickers';
	front: string;
	widthIn: number | null;
	heightIn: number | null;
};

export type StickerViewer = Viewer & { setCount(n: number): void };

const PHONE_DEPTH = 0.31;
const BAG_MARGIN = 0.25; // plastic around the stack on each side
const ZIP_IN = 0.6; // extra bag length for the zip strip
const BAG_H = PER_BAG * T + 0.04;
const PILE_GAP = 0.5;
const ELEVATION = THREE.MathUtils.degToRad(42); // camera looks down at the table
const DROP_IN = 4; // new bags fall from this high
const MASK_PX = 800; // the photo is cut out at up to this many pixels across

export function createStickerViewer(canvas: HTMLCanvasElement, opts: StickerOptions, onReady: () => void): StickerViewer {
	const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true });
	renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
	renderer.outputColorSpace = THREE.SRGBColorSpace;
	renderer.toneMapping = THREE.NeutralToneMapping;
	renderer.shadowMap.enabled = true;
	renderer.shadowMap.type = THREE.VSMShadowMap;

	const scene = new THREE.Scene();
	const pmrem = new THREE.PMREMGenerator(renderer);
	const room = new RoomEnvironment();
	scene.environment = pmrem.fromScene(room, 0.04).texture;
	room.dispose();

	const key = new THREE.DirectionalLight(0xffffff, 1.1);
	key.position.set(-6, 30, 10);
	key.castShadow = true;
	key.shadow.mapSize.set(2048, 2048);
	key.shadow.radius = 12;
	key.shadow.blurSamples = 20;
	key.shadow.bias = -0.0005;
	const sc = key.shadow.camera;
	sc.left = sc.bottom = -24;
	sc.right = sc.top = 24;
	sc.near = 1;
	sc.far = 80;
	scene.add(key);

	const floor = new THREE.Mesh(new THREE.PlaneGeometry(120, 120), new THREE.ShadowMaterial({ opacity: 0.14 }));
	floor.rotation.x = -Math.PI / 2;
	floor.receiveShadow = true;
	scene.add(floor);

	// The packs turn on a turntable; the phone stays put beside them.
	const turntable = new THREE.Group();
	scene.add(turntable);

	const phone = new THREE.Group();
	const body = new THREE.Mesh(
		new RoundedBoxGeometry(PHONE.w, PHONE.h, PHONE_DEPTH, 6, 0.42),
		new THREE.MeshPhysicalMaterial({ color: 0x2b2c31, metalness: 0.7, roughness: 0.32 })
	);
	body.castShadow = true;
	const screen = new THREE.Mesh(
		new RoundedBoxGeometry(PHONE.w - 0.14, PHONE.h - 0.14, 0.02, 4, 0.34),
		new THREE.MeshPhysicalMaterial({ color: 0x07080b, roughness: 0.05, clearcoat: 1, clearcoatRoughness: 0.02 })
	);
	screen.position.z = PHONE_DEPTH / 2;
	phone.add(body, screen);
	phone.rotation.set(-Math.PI / 2, 0, THREE.MathUtils.degToRad(-10)); // lying face up
	phone.position.y = PHONE_DEPTH / 2;
	scene.add(phone);

	// Shared by every sticker; filled in once the photo is cut out.
	let size = { w: 3, h: 2 };
	let face: THREE.PlaneGeometry | null = null;
	// Vinyl with a satin laminate: a soft sheen without washing out the print.
	const art = new THREE.MeshPhysicalMaterial({ roughness: 0.5, clearcoat: 0.25, clearcoatRoughness: 0.35, envMapIntensity: 0.6, alphaTest: 0.5 });
	const backing = new THREE.MeshStandardMaterial({ color: 0xf3f1ec, roughness: 0.9, alphaTest: 0.5 });
	const plastic = new THREE.MeshPhysicalMaterial({
		color: 0xffffff, transparent: true, opacity: 0.3, roughness: 0.12, clearcoat: 1, clearcoatRoughness: 0.05, depthWrite: false
	});
	const zip = new THREE.MeshPhysicalMaterial({ color: 0xe9eef2, transparent: true, opacity: 0.55, roughness: 0.3, depthWrite: false });
	let bagGeo: RoundedBoxGeometry | null = null;
	let zipGeo: THREE.BoxGeometry | null = null;

	let count = 0;
	let ready = false;
	// Bags on the table, oldest first; each remembers when it started falling.
	let bags: { group: THREE.Group; y: number; born: number }[] = [];
	let loose: THREE.Group | null = null;
	let levels = 0; // how high the bags are piled
	let extent = 3; // half-width of what's on the turntable, for framing
	let shownExtent = 3;

	// A stack of n stickers, bottom at y = 0, centered on x/z. Each sticker is its
	// artwork with the white backing just under it; stacked, the backings make
	// the white edge you see on a real bundle (backings stick out a hair).
	function stack(n: number, place: (i: number, m: THREE.Matrix4) => void) {
		const g = new THREE.Group();
		const tops = new THREE.InstancedMesh(face!, art, n);
		const backs = new THREE.InstancedMesh(face!, backing, n * 2);
		backs.castShadow = true;
		const m = new THREE.Matrix4();
		const under = new THREE.Matrix4();
		for (let i = 0; i < n; i++) {
			place(i, m);
			tops.setMatrixAt(i, m);
			for (const k of [0, 1]) {
				under.makeScale(1.012, 1, 1.012).setPosition(0, -T * (0.3 + 0.4 * k), 0).premultiply(m);
				backs.setMatrixAt(i * 2 + k, under);
			}
		}
		g.add(backs, tops);
		return g;
	}

	function bag(seed: number) {
		const g = stack(PER_BAG, (i, m) => {
			// Die-cut stacks are nearly, not perfectly, square.
			const j = Math.sin(seed * 31 + i * 12.9898) * 43758.5453;
			const jx = (j - Math.floor(j) - 0.5) * 0.03;
			m.makeTranslation(jx, (i + 1) * T + 0.01, ZIP_IN / 2 - jx);
		});
		const shell = new THREE.Mesh(bagGeo!, plastic);
		shell.position.y = BAG_H / 2;
		const strip = new THREE.Mesh(zipGeo!, zip);
		strip.position.set(0, BAG_H / 2, -(size.h + 2 * BAG_MARGIN + ZIP_IN) / 2 + ZIP_IN * 0.45);
		g.add(shell, strip);
		return g;
	}

	function fan(n: number) {
		// Dealt like cards, five to a row, so you can count them.
		return stack(n, (i, m) => {
			const col = i % 5, row = Math.floor(i / 5);
			const turn = (col - 2) * 0.08 + row * 0.05;
			m.makeRotationY(turn).setPosition(
				(col - (Math.min(n, 5) - 1) / 2) * size.w * 0.32,
				(i + 1) * T * 1.5,
				(row - (Math.ceil(n / 5) - 1) / 2) * size.h * 0.6
			);
		});
	}

	function layout(now: number) {
		if (!ready) return;
		const plan = packLayout(count);
		const bagW = size.w + 2 * BAG_MARGIN;
		const piles = Math.min(plan.bags.length, ACROSS);
		levels = Math.ceil(plan.bags.length / ACROSS);
		const looseW = plan.loose ? size.w * (1 + 0.32 * (Math.min(plan.loose, 5) - 1)) : 0;
		const totalW = piles * bagW + Math.max(0, piles - 1) * PILE_GAP + (looseW && piles ? PILE_GAP : 0) + looseW;
		let x = -totalW / 2;

		// Keep bags that are still requested (so only new ones fall in).
		let fresh = 0;
		for (const b of bags.splice(plan.bags.length)) dispose(b.group);
		plan.bags.forEach((p, i) => {
			if (!bags[i]) {
				const group = bag(i);
				// A slight twist per bag, like they were dropped on the table.
				group.rotation.y = Math.sin(i * 2.3) * 0.07;
				turntable.add(group);
				bags[i] = { group, y: 0, born: now + fresh++ * 120 }; // one after another
			}
			bags[i].y = p.level * BAG_H;
			bags[i].group.position.x = x + p.pile * (bagW + PILE_GAP) + bagW / 2;
		});
		x += piles * (bagW + PILE_GAP);

		if (loose) dispose(loose);
		loose = plan.loose ? fan(plan.loose) : null;
		if (loose) {
			loose.position.x = x + looseW / 2;
			turntable.add(loose);
		}
		const rows = Math.ceil(plan.loose / 5);
		const depth = Math.max(piles ? size.h + 2 * BAG_MARGIN + ZIP_IN : 0, rows ? size.h * (1 + 0.6 * (rows - 1)) : 0);
		extent = Math.hypot(totalW, depth) / 2;
	}

	function dispose(g: THREE.Group) {
		turntable.remove(g);
		g.traverse((o) => o instanceof THREE.InstancedMesh && o.dispose());
	}

	// Cut the sticker out of its product photo (the CDN allows cross-origin reads).
	new THREE.ImageLoader().setCrossOrigin('anonymous').load(
		opts.front,
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
			art.map = cut(false);
			backing.map = cut(true);
			art.needsUpdate = backing.needsUpdate = true;

			size = stickerSize({ width_in: opts.widthIn, height_in: opts.heightIn }, box.w / box.h);
			face = new THREE.PlaneGeometry(size.w, size.h).rotateX(-Math.PI / 2);
			bagGeo = new RoundedBoxGeometry(size.w + 2 * BAG_MARGIN, BAG_H, size.h + 2 * BAG_MARGIN + ZIP_IN, 2, 0.015);
			zipGeo = new THREE.BoxGeometry(size.w + 2 * BAG_MARGIN - 0.02, BAG_H + 0.02, 0.12);
			ready = true;
			// The first view shouldn't animate in.
			layout(-1e9);
			shownExtent = extent;
			onReady();
		},
		undefined,
		() => onReady()
	);

	const camera = new THREE.PerspectiveCamera(26, 1, 0.05, 2000);
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
		resize(width, height) {
			renderer.setSize(width, height, false);
			camera.aspect = width / height;
			camera.updateProjectionMatrix();
		},
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
			const r = shownExtent;
			phone.position.x = r + GAP_IN + PHONE.w / 2;
			phone.position.z = 0.4;
			const sceneW = 2 * r + GAP_IN + PHONE.w;
			const sceneD = Math.max(2 * r, PHONE.h);
			const cx = (phone.position.x + PHONE.w / 2 - r) / 2;
			const vfov = THREE.MathUtils.degToRad(camera.fov) / 2;
			const fitW = (sceneW * 0.6) / (Math.tan(vfov) * camera.aspect);
			const fitD = (sceneD * Math.sin(ELEVATION) * 0.5 + levels * BAG_H) / Math.tan(vfov);
			const dist = Math.max(fitW, fitD) * viewer.zoom;
			const zoomed = 1 - viewer.zoom;
			target.set(THREE.MathUtils.lerp(cx, 0, Math.min(1, zoomed * 1.6)), 0.2, 0);
			camera.position.set(target.x, target.y + dist * Math.sin(ELEVATION), dist * Math.cos(ELEVATION));
			camera.lookAt(target);
			renderer.render(scene, camera);
		},
		dispose() {
			scene.traverse((o) => {
				if (o instanceof THREE.Mesh) {
					o.geometry.dispose();
					for (const m of [o.material].flat()) {
						for (const v of Object.values(m)) if (v instanceof THREE.Texture) v.dispose();
						m.dispose();
					}
				}
				if (o instanceof THREE.InstancedMesh) o.dispose();
			});
			scene.environment?.dispose();
			pmrem.dispose();
			renderer.dispose();
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
