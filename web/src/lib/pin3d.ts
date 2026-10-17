// Enamel pins in 3D (pin.ts has the math). The pin is cut out of its product
// photo: a die-struck metal blank with rounded edges in the outline's shape;
// on its face, the photo's gold lines and lettering stand proud of glossy
// enamel; on the back, a matte blank with a post and butterfly clutch at
// each spot postSpots picks. For its product page (createPinViewer), it turns
// in the air beside a phone; the haul lays pins face up (loadPin). Units are
// inches; the pin faces +z.
import * as THREE from 'three';
import { RoundedBoxGeometry } from 'three/examples/jsm/geometries/RoundedBoxGeometry.js';
import { createPhone, createStage, type Viewer } from './paper3d';
import { BODY_IN, CLUTCH_IN, edgeDistance, erode, faceColor, largestPiece, plating, metalMap, pinSize, POST_IN, postSpots, RELIEF_IN, reliefHeights, traceOutline } from './pin';
import { PHONE } from './scale';
import { maskBounds, stickerMask } from './stickers';

export type PinOptions = {
	kind: 'pin';
	front: string; // a product photo of the pin's face, on a plain backdrop
	widthIn: number | null;
	heightIn: number | null;
	phone?: boolean; // float a phone beside it for scale (default true)
};

export type PinModel = {
	/** The pin, centred on x/y with its face at z ≈ 0 and its posts toward -z. */
	group: THREE.Group;
	size: { w: number; h: number };
	dispose(): void;
};

const PHOTO_PX = 720; // the photo is cut out at up to this many pixels across
const BEVEL_IN = 0.008; // the blank's rounded edge
const POST_R = 0.022; // a 1.1 mm post

/**
 * Loads a pin from its photo. `cells` is how many grid cells the face's relief
 * has across: plenty for a close-up, fewer for a table of them. Lying face up
 * (`lying`), it would mirror the bright ceiling, so it reflects less.
 */
export function loadPin(
	renderer: THREE.WebGLRenderer, url: string, widthIn: number | null, heightIn: number | null, cells: number,
	done: (pin: PinModel | null) => void, lying = false
) {
	new THREE.ImageLoader().setCrossOrigin('anonymous').load(
		url,
		(img) => {
			try {
				done(buildPin(renderer, img, widthIn, heightIn, cells, lying ? 0.45 : 1));
			} catch {
				done(null); // a cross-origin photo we can't read
			}
		},
		undefined,
		() => done(null)
	);
}

function buildPin(renderer: THREE.WebGLRenderer, img: HTMLImageElement, widthIn: number | null, heightIn: number | null, cells: number, env: number): PinModel {
	const scale = Math.min(1, PHOTO_PX / Math.max(img.width, img.height));
	const W = Math.max(1, Math.round(img.width * scale)), H = Math.max(1, Math.round(img.height * scale));
	const c = document.createElement('canvas');
	c.width = W;
	c.height = H;
	const ctx = c.getContext('2d', { willReadFrequently: true })!;
	ctx.drawImage(img, 0, 0, W, H);
	const full = ctx.getImageData(0, 0, W, H).data;
	let fullMask = largestPiece(erode(stickerMask(full, W, H), W, H), W, H);
	let box = maskBounds(fullMask, W, H);
	if (!box || box.w * box.h > W * H * 0.98) {
		fullMask = new Uint8Array(W * H).fill(1);
		box = { x: 0, y: 0, w: W, h: H };
	}
	// Crop to the pin, with a pixel of backdrop round it so the outline closes.
	const w = box.w + 2, h = box.h + 2;
	const px = new Uint8ClampedArray(w * h * 4);
	const mask = new Uint8Array(w * h);
	for (let y = 0; y < h; y++)
		for (let x = 0; x < w; x++) {
			const sx = box.x + x - 1, sy = box.y + y - 1;
			if (sx < 0 || sy < 0 || sx >= W || sy >= H) continue;
			const s = sy * W + sx, i = y * w + x;
			px.set(full.subarray(s * 4, s * 4 + 4), i * 4);
			mask[i] = fullMask[s];
		}
	const size = pinSize(widthIn, heightIn, box.w / box.h);
	const pxPerIn = box.w / size.w;
	// Pixel (x, y) of the crop → inches, the pin's box centred on 0, y up.
	const toX = (x: number) => ((x - 1) / box.w - 0.5) * size.w;
	const toY = (y: number) => (0.5 - (y - 1) / box.h) * size.h;

	const metal = metalMap(px, mask, w, h);
	const heights = reliefHeights(metal, mask, w, h, pxPerIn, Math.max(1, Math.round(0.006 * pxPerIn)));
	// The rim round the outline is metal, whatever the photo's edge pixels say.
	const near = edgeDistance(mask, w, h, 4);
	for (let i = 0; i < metal.length; i++) if (mask[i] && near[i] <= 2) metal[i] = 1;
	const photoGold = plating(px, metal) ?? [217, 180, 90];
	for (let i = 0; i < metal.length; i++) {
		if (!mask[i]) continue;
		px.set(faceColor(px[i * 4], px[i * 4 + 1], px[i * 4 + 2], metal[i] === 1, photoGold), i * 4);
	}

	// The gold, from the photo's metal: the blank's edge and back are plated the same.
	const [gr, gg, gb] = plating(px, metal) ?? photoGold;
	const gold = new THREE.Color().setRGB(gr / 255, gg / 255, gb / 255, THREE.SRGBColorSpace);
	const hsl = { h: 0, s: 0, l: 0 };
	gold.getHSL(hsl);
	gold.setHSL(hsl.h, Math.min(1, hsl.s * 1.1), Math.min(0.62, Math.max(0.5, hsl.l)));

	// The face's art (cut out) and its surface: R clearcoat (the enamel's gloss), G
	// roughness, B metalness. Off the pin and on its outermost pixels (the photo's
	// blur into its backdrop) they're the plating, so filtering bleeds gold into
	// the edge, not backdrop. Data textures, not canvases: a canvas would drop the
	// color of see-through pixels. Rows go in bottom first, as the uvs expect.
	const goldRGB = new THREE.Color();
	gold.getRGB(goldRGB, THREE.SRGBColorSpace);
	const plate = [goldRGB.r * 255, goldRGB.g * 255, goldRGB.b * 255];
	const artData = new Uint8Array(w * h * 4);
	const surfData = new Uint8Array(w * h * 4);
	for (let y = 0; y < h; y++)
		for (let x = 0; x < w; x++) {
			const i = y * w + x, o = ((h - 1 - y) * w + x) * 4;
			const edge = !mask[i] || near[i] <= 1;
			artData.set(edge ? plate : px.subarray(i * 4, i * 4 + 3), o);
			artData[o + 3] = mask[i] ? 255 : 0;
			surfData.set(metal[i] || edge ? [0, 70, 255, 255] : [255, 25, 0, 255], o);
		}
	const texture = (data: Uint8Array, color: THREE.ColorSpace) => {
		const t = new THREE.DataTexture(data, w, h, THREE.RGBAFormat);
		t.colorSpace = color;
		t.magFilter = THREE.LinearFilter;
		t.minFilter = THREE.LinearMipmapLinearFilter;
		t.generateMipmaps = true;
		t.anisotropy = renderer.capabilities.getMaxAnisotropy();
		t.needsUpdate = true;
		return t;
	};
	const map = texture(artData, THREE.SRGBColorSpace);
	const surfaceMap = texture(surfData, THREE.NoColorSpace);

	const faceMat = new THREE.MeshPhysicalMaterial({
		map, roughnessMap: surfaceMap, metalnessMap: surfaceMap, clearcoatMap: surfaceMap,
		roughness: 1, metalness: 1, clearcoat: env, clearcoatRoughness: 0.04, envMapIntensity: 1.2 * env, alphaTest: 0.5, side: THREE.DoubleSide
	});
	const edgeMat = new THREE.MeshPhysicalMaterial({ color: gold, metalness: 1, roughness: 0.2, envMapIntensity: 1.3 * Math.sqrt(env) });
	const backMat = new THREE.MeshPhysicalMaterial({ color: gold.clone().multiplyScalar(0.92), metalness: 1, roughness: 0.55, envMapIntensity: 1.1 * Math.sqrt(env) });

	const face = reliefGeometry(heights, mask, w, h, Math.max(1, Math.round(box.w / cells)), toX, toY);

	// The blank: the outline extruded, its edge rounded over, its front just under the face.
	const ring = traceOutline(mask, w, h, 0.35, 3);
	const shape = new THREE.Shape(ring.map(([x, y]) => new THREE.Vector2(toX(x), toY(y))));
	const depth = BODY_IN - 2 * BEVEL_IN;
	const blank = new THREE.ExtrudeGeometry(shape, {
		depth, bevelEnabled: true, bevelThickness: BEVEL_IN, bevelSize: BEVEL_IN * 0.6, bevelOffset: -BEVEL_IN * 0.6, bevelSegments: 3, curveSegments: 1
	});
	blank.translate(0, 0, -depth - BEVEL_IN - 0.0015);

	const group = new THREE.Group();
	const faceMesh = new THREE.Mesh(face, faceMat);
	const blankMesh = new THREE.Mesh(blank, [backMat, edgeMat]); // ExtrudeGeometry groups: caps, then sides
	for (const m of [faceMesh, blankMesh]) (m.castShadow = true), (m.receiveShadow = true);
	group.add(blankMesh, faceMesh);

	// Posts and butterfly clutches.
	const geos: THREE.BufferGeometry[] = [face, blank];
	const post = new THREE.CylinderGeometry(POST_R, POST_R, POST_IN, 14).rotateX(Math.PI / 2).translate(0, 0, -BODY_IN - POST_IN / 2);
	const solder = new THREE.SphereGeometry(POST_R * 2.2, 14, 8).scale(1, 1, 0.35).translate(0, 0, -BODY_IN);
	const clutch = clutchGeometry();
	geos.push(post, solder, ...clutch);
	const clutchMat = new THREE.MeshPhysicalMaterial({ color: gold, metalness: 1, roughness: 0.32, envMapIntensity: 1.2 * Math.sqrt(env), side: THREE.DoubleSide });
	for (const [fx, fy] of postSpots(mask, w, h, size)) {
		const spot = new THREE.Group();
		spot.position.set(toX(1 + fx * box.w), toY(1 + fy * box.h), 0);
		spot.add(new THREE.Mesh(post, edgeMat), new THREE.Mesh(solder, backMat));
		const holder = new THREE.Group();
		holder.position.z = -BODY_IN - POST_IN * 0.55;
		for (const geo of clutch) holder.add(new THREE.Mesh(geo, clutchMat));
		spot.add(holder);
		spot.traverse((o) => o instanceof THREE.Mesh && (o.castShadow = true));
		group.add(spot);
	}

	return {
		group,
		size,
		dispose() {
			for (const geo of geos) geo.dispose();
			for (const m of [faceMat, edgeMat, backMat, clutchMat]) m.dispose();
			map.dispose();
			surfaceMap.dispose();
		}
	};
}

/**
 * The face: a grid over the pin's box, `step` pixels a cell, raised by the
 * relief heights (each vertex the average of the four pixels it touches, so
 * the face slopes down to the outline). Cells off the pin are left out; the
 * art's alpha trims the rest to the outline.
 */
function reliefGeometry(
	heights: Float32Array, mask: Uint8Array, w: number, h: number, step: number,
	toX: (x: number) => number, toY: (y: number) => number
): THREE.BufferGeometry {
	const nx = Math.ceil(w / step), ny = Math.ceil(h / step);
	const pos: number[] = [], uv: number[] = [];
	const at = (x: number, y: number) => (x >= 0 && y >= 0 && x < w && y < h ? y * w + x : -1);
	for (let j = 0; j <= ny; j++)
		for (let i = 0; i <= nx; i++) {
			const x = Math.min(w, i * step), y = Math.min(h, j * step);
			let sum = 0, k = 0;
			for (const p of [at(x - 1, y - 1), at(x, y - 1), at(x - 1, y), at(x, y)]) if (p >= 0) (sum += heights[p]), k++;
			pos.push(toX(x), toY(y), (k ? sum / k : 0) * RELIEF_IN);
			uv.push(x / w, 1 - y / h);
		}
	const index: number[] = [];
	const v = (i: number, j: number) => j * (nx + 1) + i;
	for (let j = 0; j < ny; j++)
		for (let i = 0; i < nx; i++) {
			// Keep a cell if any pixel in it is on the pin.
			let on = false;
			for (let y = j * step; y < Math.min(h, (j + 1) * step) && !on; y++)
				for (let x = i * step; x < Math.min(w, (i + 1) * step) && !on; x++) on = mask[y * w + x] === 1;
			if (on) index.push(v(i, j), v(i, j + 1), v(i + 1, j), v(i + 1, j), v(i, j + 1), v(i + 1, j + 1));
		}
	const geo = new THREE.BufferGeometry();
	geo.setAttribute('position', new THREE.Float32BufferAttribute(pos, 3));
	geo.setAttribute('uv', new THREE.Float32BufferAttribute(uv, 2));
	geo.setIndex(index);
	geo.computeVertexNormals();
	return geo;
}

/**
 * A butterfly clutch, centred on its post at z = 0: a small plate with its
 * two sides rolled forward into tubes that grip the post.
 */
function clutchGeometry(): THREE.BufferGeometry[] {
	const s = CLUTCH_IN, t = 0.012, roll = 0.05;
	const plate = new RoundedBoxGeometry(s - 2 * roll, s * 0.85, t, 2, t / 2.5);
	// Cylinder angles run from +z (0) through +x (π/2): each tube is open toward the post.
	const wing = (side: number) =>
		new THREE.CylinderGeometry(roll, roll, s * 0.85, 16, 1, true, (side > 0 ? 0 : Math.PI) - 0.2, Math.PI + 0.4)
			.translate(side * (s / 2 - roll * 1.05), 0, roll * 0.9);
	const dimple = new THREE.TorusGeometry(POST_R * 1.8, POST_R * 0.5, 8, 18).translate(0, 0, -t / 2);
	return [plate, wing(1), wing(-1), dimple];
}

export function createPinViewer(canvas: HTMLCanvasElement, opts: PinOptions, onReady: () => void): Viewer {
	const withPhone = opts.phone !== false;
	// Until the photo says otherwise, the measured box.
	let size = pinSize(opts.widthIn, opts.heightIn, (opts.widthIn ?? 1) / (opts.heightIn ?? 1));
	// Beside a phone it floats about level with the phone's lower half, close by, so
	// the two read together; alone (a shop tile) just off the floor.
	const GAP = 1.2;
	const lift = () => (withPhone ? Math.max(0.35, PHONE.h * 0.38 - size.h / 2) : 0.35);
	const scene3 = () => {
		const sceneW = withPhone ? size.w + GAP + PHONE.w : size.w;
		const sceneH = withPhone ? Math.max(size.h + lift(), PHONE.h) : size.h + lift();
		return { sceneW, sceneH, left: -sceneW / 2 };
	};
	let layout = scene3();
	const stage = createStage(canvas, layout.sceneW, layout.sceneH);
	const { scene, renderer, camera } = stage;

	const pivot = new THREE.Group();
	scene.add(pivot);
	const phone = createPhone();
	if (withPhone) scene.add(phone);
	const place = () => {
		layout = scene3();
		pivot.position.set(layout.left + size.w / 2, lift() + size.h / 2, 0);
		phone.position.set(layout.left + size.w + GAP + PHONE.w / 2, PHONE.h / 2, 0);
		stage.fitLight(layout.sceneW, layout.sceneH);
	};
	place();

	let pin: PinModel | null = null;
	let disposed = false;
	loadPin(renderer, opts.front, opts.widthIn, opts.heightIn, 320, (loaded) => {
		if (disposed) return loaded?.dispose();
		pin = loaded;
		if (pin) {
			size = pin.size;
			// Tipped back a touch, so the light rakes across the relief.
			pin.group.rotation.x = -0.12;
			pivot.add(pin.group);
			place();
		}
		onReady();
	});

	const viewer: Viewer = {
		angle: 0,
		zoom: 1,
		resize: stage.resize,
		render() {
			pivot.rotation.y = viewer.angle;
			// Zooming in closes from the whole scene to the pin filling the view, then on into its relief.
			const { sceneW, sceneH, left } = layout;
			const k = THREE.MathUtils.smoothstep(1 - viewer.zoom, 0, 0.5);
			const closer = THREE.MathUtils.smoothstep(1 - viewer.zoom, 0.5, 0.78);
			const x = THREE.MathUtils.lerp(0, left + size.w / 2, k);
			const y = THREE.MathUtils.lerp(sceneH / 2, lift() + size.h / 2, k);
			const w = THREE.MathUtils.lerp(sceneW, size.w * 1.2, k);
			const h = THREE.MathUtils.lerp(sceneH, size.h * 1.6, k);
			// As stage.frame does, but aimed at the pin's middle, which floats.
			const half = Math.tan(THREE.MathUtils.degToRad(camera.fov) / 2);
			const dist = Math.max((h * 0.56) / half, (w * 0.56) / (half * camera.aspect)) * THREE.MathUtils.lerp(1, 0.72, closer);
			camera.near = Math.max(0.1, dist * 0.1);
			camera.far = dist * 4;
			camera.updateProjectionMatrix();
			camera.position.set(x + dist * 0.05, y + dist * 0.16, dist);
			camera.lookAt(x, y, 0);
			stage.render();
		},
		dispose() {
			disposed = true;
			pin?.dispose();
			stage.dispose();
		}
	};
	return viewer;
}
