// WebGL product viewer for printed items: the real artwork on a sheet with true
// thickness and a paper surface (matte, gloss, linen…) lit by a studio
// environment, standing next to a phone at the same scale. Units are inches.
import * as THREE from 'three';
import { RoomEnvironment } from 'three/examples/jsm/environments/RoomEnvironment.js';
import { RoundedBoxGeometry } from 'three/examples/jsm/geometries/RoundedBoxGeometry.js';
import { coverCrop, finishInfo, paperHeight, SURFACE_TILE_IN, type FinishInfo } from './paper';
import { GAP_IN, PHONE } from './scale';

export type ViewerOptions = {
	front: string;
	back?: string;
	widthIn: number;
	heightIn: number;
	thicknessIn: number;
	finish: string;
	phone?: boolean; // stand a phone beside it for scale (default true)
};

export type Viewer = {
	/** Rotation of the print around its vertical axis, radians. */
	angle: number;
	/** 1 = whole scene in view; smaller is closer. */
	zoom: number;
	resize(width: number, height: number): void;
	render(): void;
	dispose(): void;
};

export const PHONE_DEPTH = 0.31;
export const LEAN = THREE.MathUtils.degToRad(-4); // prints lean back a touch, like they're propped up

export function createViewer(canvas: HTMLCanvasElement, opts: ViewerOptions, onReady: () => void): Viewer {
	const { widthIn: w, heightIn: h } = opts;
	const withPhone = opts.phone !== false;
	const sceneW = withPhone ? w + GAP_IN + PHONE.w : w;
	const sceneH = withPhone ? Math.max(h, PHONE.h) : h;
	const stage = createStage(canvas, sceneW, sceneH);
	const { scene, renderer } = stage;

	// Everything is laid out left to right, then centered on x = 0.
	const left = -sceneW / 2;

	const sheet = createSheet(opts, renderer, onReady);
	sheet.pivot.position.set(left + w / 2, 0, 0);
	scene.add(sheet.pivot);

	if (withPhone) {
		const phone = createPhone();
		phone.position.set(left + w + GAP_IN + PHONE.w / 2, PHONE.h / 2, 0);
		scene.add(phone);
	}

	const viewer: Viewer = {
		angle: 0,
		zoom: 1,
		resize: stage.resize,
		render() {
			sheet.pivot.rotation.y = viewer.angle;
			// Frame the whole scene at zoom 1; zooming moves toward the print's center.
			const zoomed = 1 - viewer.zoom; // 0 at full view
			const x = THREE.MathUtils.lerp(0, left + w / 2, Math.min(1, zoomed * 1.6));
			stage.frame(x, sceneW, sceneH, viewer.zoom);
			stage.render();
		},
		dispose() {
			sheet.dispose();
			stage.dispose();
		}
	};
	return viewer;
}

export type TableFrameOptions = {
	elevation?: number;
	room?: number;
	x?: number;
	y?: number;
	/** How far the scene reaches below the tabletop (a table's legs), inches. */
	below?: number;
};

export type Stage = {
	scene: THREE.Scene;
	renderer: THREE.WebGLRenderer;
	camera: THREE.PerspectiveCamera;
	/** The invisible shadow-catching floor, at y = 0 unless moved (e.g. under a table's legs). */
	floor: THREE.Mesh;
	resize(width: number, height: number): void;
	/** Resizes the shadow frustum and floor when a tabletop scene grows or shrinks. */
	fitLight(sceneW: number, sceneH: number): void;
	/** Points the camera at x (mid-height) so an upright sceneW × sceneH area fills the view at zoom 1. */
	frame(x: number, sceneW: number, sceneH: number, zoom: number): void;
	/** Points the camera down at a tabletop of width × depth (and stack height tallest) from elevation. */
	frameTable(width: number, depth: number, tallest: number, zoom: number, opts?: TableFrameOptions): void;
	render(): void;
	dispose(): void;
};

/**
 * A photo studio: renderer, a soft environment, a key light with a contact
 * shadow on an invisible floor, and a camera. Light and shadow are sized for
 * a scene of about sceneW × sceneH inches standing around x = 0.
 */
export function createStage(canvas: HTMLCanvasElement, sceneW: number, sceneH: number): Stage {
	const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true });
	renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
	renderer.outputColorSpace = THREE.SRGBColorSpace;
	// Neutral tone mapping keeps print colors true; ACES would desaturate them.
	renderer.toneMapping = THREE.NeutralToneMapping;
	renderer.toneMappingExposure = 1;
	renderer.shadowMap.enabled = true;
	renderer.shadowMap.type = THREE.VSMShadowMap;

	const scene = new THREE.Scene();
	const pmrem = new THREE.PMREMGenerator(renderer);
	const room = new RoomEnvironment();
	scene.environment = pmrem.fromScene(room, 0.04).texture;
	room.dispose();

	// Soft studio key light from above and slightly in front, so the shadow pools
	// under things (a contact shadow) instead of stretching across the floor.
	const key = new THREE.DirectionalLight(0xffffff, 1.1);
	key.castShadow = true;
	key.shadow.mapSize.set(2048, 2048);
	key.shadow.radius = 18;
	key.shadow.blurSamples = 24;
	key.shadow.bias = -0.0005;
	scene.add(key);

	const floor = new THREE.Mesh(new THREE.PlaneGeometry(1, 1), new THREE.ShadowMaterial({ opacity: 0.12 }));
	floor.rotation.x = -Math.PI / 2;
	floor.position.y = -0.02;
	floor.receiveShadow = true;
	scene.add(floor);

	const fitLight = (w: number, h: number) => {
		const span = Math.max(w, h, 8);
		key.position.set(-span * 0.25, Math.max(h, 12) * 4, Math.max(h, 12) * 0.9);
		const sc = key.shadow.camera;
		sc.left = sc.bottom = -span * 1.5;
		sc.right = sc.top = span * 1.5;
		sc.near = 0.1;
		sc.far = Math.max(h, 12) * 8;
		sc.updateProjectionMatrix();
		floor.scale.set(span * 6, span * 6, 1);
	};
	fitLight(sceneW, sceneH);

	const camera = new THREE.PerspectiveCamera(26, 1, 0.5, 500);
	const target = new THREE.Vector3();
	let curW = 0;
	let curH = 0;
	return {
		scene,
		renderer,
		camera,
		floor,
		resize(width, height) {
			if (width <= 0 || height <= 0 || (width === curW && height === curH)) return;
			curW = width;
			curH = height;
			renderer.setSize(width, height, false);
			camera.aspect = width / height;
			camera.updateProjectionMatrix();
		},
		fitLight,
		frame(x, w, h, zoom) {
			const vfov = THREE.MathUtils.degToRad(camera.fov) / 2;
			const fitH = (h * 0.56) / Math.tan(vfov);
			const fitW = (w * 0.56) / (Math.tan(vfov) * camera.aspect);
			const dist = Math.max(fitH, fitW) * zoom;
			camera.near = Math.max(0.5, dist * 0.1);
			camera.far = dist * 4;
			camera.updateProjectionMatrix();
			target.set(x, h * 0.5, 0);
			camera.position.set(target.x + dist * 0.05, target.y + dist * 0.16, dist);
			camera.lookAt(target);
		},
		frameTable(width, depth, tallest, zoom, opts = {}) {
			const el = opts.elevation ?? THREE.MathUtils.degToRad(36);
			const room = opts.room ?? 0.55;
			const span = Math.hypot(width, depth);
			const half = THREE.MathUtils.degToRad(camera.fov) / 2;
			const fitW = (span * room) / (Math.tan(half) * camera.aspect);
			const below = opts.below ?? 0;
			const fitH = (span * Math.sin(el) * room + (tallest * 0.5 + below) * Math.cos(el)) / Math.tan(half);
			const dist = Math.max(fitW, fitH) * zoom;
			camera.near = Math.max(0.1, dist * 0.05);
			camera.far = dist * 4;
			camera.updateProjectionMatrix();
			target.set(opts.x ?? 0, (opts.y ?? tallest * 0.35) - below * 0.5, 0);
			camera.position.set(target.x, target.y + dist * Math.sin(el), dist * Math.cos(el));
			camera.lookAt(target);
		},
		render() {
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
			// Free the WebGL context now; browsers cap how many a page can hold.
			renderer.forceContextLoss();
		}
	};
}

/** Loads art onto a material, cropped to fill a w × h face like a print shop would. */
export function loadArt(
	renderer: THREE.WebGLRenderer,
	url: string,
	aspect: number,
	done: (tex: THREE.Texture | null) => void,
	mirror = false
) {
	new THREE.TextureLoader().setCrossOrigin('anonymous').load(
		url,
		(tex) => {
			tex.colorSpace = THREE.SRGBColorSpace;
			tex.anisotropy = renderer.capabilities.getMaxAnisotropy();
			const img = tex.image as { width: number; height: number };
			const c = coverCrop(img.width / img.height, aspect);
			// A mirrored copy is for the back of a two-sided plane, seen from behind.
			tex.repeat.set(mirror ? -c.repeatX : c.repeatX, c.repeatY);
			tex.offset.set(mirror ? c.offsetX + c.repeatX : c.offsetX, c.offsetY);
			done(tex);
		},
		undefined,
		() => done(null)
	);
}

/** Printed paper with the finish's roughness, gloss coat, sheen and fiber/linen normal map, for a w × h face. */
export function paperMaterial(info: FinishInfo, normal: THREE.Texture) {
	return new THREE.MeshPhysicalMaterial({
		color: 0xffffff,
		roughness: info.roughness,
		clearcoat: info.clearcoat,
		clearcoatRoughness: 0.15,
		sheen: info.sheen,
		sheenRoughness: 0.8,
		sheenColor: 0xffffff,
		normalMap: normal
	});
}

/** A printed paper face material with the finish's surface. */
export function paperFace(finish: string, w: number, h: number): { mat: THREE.MeshPhysicalMaterial; dispose(): void } {
	const info = finishInfo(finish);
	const surface = paperSurface(info);
	surface.normal.repeat.set(w / surface.tileIn, h / surface.tileIn);
	return { mat: paperMaterial(info, surface.normal), dispose: () => surface.normal.dispose() };
}

/**
 * A printed sheet with real thickness: front and back art on the paper's
 * surface, standing on its bottom edge and leaning back a touch. Turn it with
 * pivot.rotation.y.
 */
export function createSheet(opts: ViewerOptions, renderer: THREE.WebGLRenderer, onReady: () => void) {
	const { widthIn: w, heightIn: h, thicknessIn: t } = opts;
	const info = finishInfo(opts.finish);
	const surface = paperSurface(info);
	surface.normal.repeat.set(w / surface.tileIn, h / surface.tileIn);
	let pending = 0;
	const face = (url: string | undefined) => {
		const m = paperMaterial(info, surface.normal);
		if (url) {
			pending++;
			loadArt(renderer, url, w / h, (tex) => {
				if (tex) {
					m.map = tex;
					m.needsUpdate = true;
				}
				if (--pending === 0) onReady();
			});
		}
		return m;
	};
	// BoxGeometry face order: +x, -x, +y, -y, +z (front), -z (back).
	const edge = paperEdge();
	const sheet = new THREE.Mesh(new THREE.BoxGeometry(w, h, t), [edge, edge, edge, edge, face(opts.front), face(opts.back)]);
	sheet.castShadow = true;
	sheet.position.y = h / 2;
	// Pivot at the bottom edge so the lean looks propped rather than floating.
	const pivot = new THREE.Group();
	const lean = new THREE.Group();
	lean.rotation.x = LEAN;
	lean.add(sheet);
	pivot.add(lean);
	if (pending === 0) queueMicrotask(onReady);
	return { pivot, dispose: () => surface.normal.dispose() };
}

/**
 * The kraft paper band a printer wraps around a bundle (of 50 postcards, say):
 * a strip around the middle of a w × h × d stack standing at (x, z) on the table.
 */
export function paperBand(w: number, h: number, d: number, x: number, z: number): THREE.Mesh {
	const across = Math.min(1.25, d / 3);
	const band = new THREE.Mesh(new THREE.BoxGeometry(w + 0.04, h + 0.02, across), new THREE.MeshStandardMaterial({ color: 0xc9a36b, roughness: 0.9 }));
	band.position.set(x, h / 2, z);
	band.castShadow = true;
	return band;
}

/** Paper's core shows on the edges: a warm off-white. */
export const paperEdge = () =>
	new THREE.MeshStandardMaterial({ color: 0xf2eee6, roughness: 0.95, polygonOffset: true, polygonOffsetFactor: 1, polygonOffsetUnits: 1 });

/** A dark phone standing at the same lean (or lying flat on a table when flat = true), for scale. */
export function createPhone(flat = false): THREE.Group {
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
	const island = new THREE.Mesh(
		new RoundedBoxGeometry(0.5, 0.14, 0.01, 3, 0.07),
		new THREE.MeshBasicMaterial({ color: 0x000000 })
	);
	island.position.set(0, PHONE.h / 2 - 0.22, PHONE_DEPTH / 2 + 0.012);
	phone.add(body, screen, island);
	if (flat) {
		phone.rotation.set(-Math.PI / 2, 0, 0);
		phone.position.y = PHONE_DEPTH / 2;
	} else {
		phone.rotation.set(LEAN, THREE.MathUtils.degToRad(-12), 0);
	}
	return phone;
}

const surfaceCache = new Map<string, HTMLCanvasElement>();

/**
 * A tiling normal map for the paper surface (paperHeight), drawn on a canvas.
 * Each tile covers SURFACE_TILE_IN. The canvas is cached per finish so
 * building sheets and piles never recomputes the heightfield.
 */
export function paperSurface(info: FinishInfo) {
	const key = `${info.weave ? 1 : 0}:${info.grain}`;
	let canvas = surfaceCache.get(key);
	if (!canvas) {
		const N = 512;
		const height = paperHeight(N, info);
		// Height → tangent-space normals (central differences, wrapping).
		canvas = document.createElement('canvas');
		canvas.width = canvas.height = N;
		const ctx = canvas.getContext('2d')!;
		const img = ctx.createImageData(N, N);
		const at = (x: number, y: number) => height[((y + N) % N) * N + ((x + N) % N)];
		for (let y = 0; y < N; y++) {
			for (let x = 0; x < N; x++) {
				const dx = at(x + 1, y) - at(x - 1, y);
				const dy = at(x, y + 1) - at(x, y - 1);
				const len = Math.hypot(dx, dy, 1);
				const i = (y * N + x) * 4;
				img.data[i] = ((-dx / len) * 0.5 + 0.5) * 255;
				img.data[i + 1] = ((dy / len) * 0.5 + 0.5) * 255;
				img.data[i + 2] = ((1 / len) * 0.5 + 0.5) * 255;
				img.data[i + 3] = 255;
			}
		}
		ctx.putImageData(img, 0, 0);
		surfaceCache.set(key, canvas);
	}
	const normal = new THREE.CanvasTexture(canvas);
	normal.wrapS = normal.wrapT = THREE.RepeatWrapping;
	normal.colorSpace = THREE.NoColorSpace;
	normal.anisotropy = 8;
	return { normal, tileIn: SURFACE_TILE_IN };
}

/** The cover's color along its spine edge, for a booklet's spine and fold. */
export function edgeColor(tex: THREE.Texture): THREE.Color {
	const c = document.createElement('canvas');
	c.width = 1;
	c.height = 16;
	const ctx = c.getContext('2d', { willReadFrequently: true })!;
	const img = tex.image as CanvasImageSource & { width: number; height: number };
	try {
		ctx.drawImage(img, 0, 0, Math.max(1, img.width * 0.03), img.height, 0, 0, 1, 16);
		const d = ctx.getImageData(0, 0, 1, 16).data;
		let r = 0, g = 0, b = 0;
		for (let i = 0; i < d.length; i += 4) (r += d[i]), (g += d[i + 1]), (b += d[i + 2]);
		return new THREE.Color().setRGB(r / 16 / 255, g / 16 / 255, b / 16 / 255, THREE.SRGBColorSpace);
	} catch {
		return new THREE.Color(0xdddddd); // a cross-origin image we can't read
	}
}

const edgeStripCache = new Map<boolean, HTMLCanvasElement>();

/**
 * Stacked page or card edges: 64 slightly uneven off-white lines per tile, one
 * per leaf. `across` runs them across the texture's u axis (an upright book's
 * fore-edge), otherwise along v (horizontal stack sides and upright top/bottom).
 */
export function pageEdges(across: boolean): THREE.CanvasTexture {
	let c = edgeStripCache.get(across);
	if (!c) {
		const N = 64;
		c = document.createElement('canvas');
		c.width = across ? N : 1;
		c.height = across ? 1 : N;
		const ctx = c.getContext('2d')!;
		let seed = 7;
		const rand = () => ((seed = (seed * 1664525 + 1013904223) >>> 0) / 4294967296);
		for (let i = 0; i < N; i++) {
			const l = 88 + rand() * 8;
			ctx.fillStyle = `hsl(40 20% ${l}%)`;
			if (across) ctx.fillRect(i, 0, 1, 1);
			else ctx.fillRect(0, i, 1, 1);
		}
		edgeStripCache.set(across, c);
	}
	const tex = new THREE.CanvasTexture(c);
	tex.wrapS = tex.wrapT = THREE.RepeatWrapping;
	tex.colorSpace = THREE.SRGBColorSpace;
	tex.anisotropy = 8;
	return tex;
}
