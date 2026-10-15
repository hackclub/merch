// WebGL flip-through for booklets (magazines): the real pages on a book with
// true size and thickness, stapled or with a square spine, that opens and
// turns its pages one leaf at a time. What ships with it (the magazine's card)
// stands beside it, then a phone for scale. Units are inches. Page bookkeeping
// is in book.ts.
import * as THREE from 'three';
import { bookState, leafAngle, leafCount, leafZ, pagesNear } from './book';
import { finishInfo } from './paper';
import { createPhone, createSheet, createStage, LEAN, loadArt, paperEdge, type Viewer, type ViewerOptions } from './paper3d';
import { GAP_IN, PHONE } from './scale';

export type BookOptions = {
	pages: string[]; // page images in reading order: front cover first, back cover last
	widthIn: number; // one page
	heightIn: number;
	thicknessIn: number; // the whole book, closed
	finish: string; // the cover's
	binding: string; // 'staple' | 'perfect'
	extras: ViewerOptions[]; // what ships with it, standing beside it
	phone?: boolean; // stand a phone beside it for scale (default true)
};

export type BookViewer = Viewer & {
	/** 0 = closed on the front cover … leaves = closed on the back cover; fractions are mid-turn. */
	position: number;
	readonly leaves: number;
};

const SEGMENTS = 36; // across a turning leaf, for its curl
const OPEN_LEAN = THREE.MathUtils.degToRad(-12); // an open book tips back, like it's on a stand

export function createBookViewer(canvas: HTMLCanvasElement, opts: BookOptions, onReady: () => void): BookViewer {
	const { widthIn: w, heightIn: h, pages } = opts;
	const n = pages.length;
	const leaves = leafCount(n);
	const T = opts.thicknessIn;
	const tl = T / leaves; // one leaf

	// Laid out left to right with the spine at x = 0: the book (which opens to
	// the left), what ships with it, then the phone.
	let x = w + GAP_IN * 0.75;
	const extraAt = opts.extras.map((e) => {
		const at = x + e.widthIn / 2;
		x += e.widthIn + GAP_IN * 0.75;
		return at;
	});
	const withPhone = opts.phone !== false;
	const phoneX = x + GAP_IN * 0.25 + PHONE.w / 2;
	const sceneRight = withPhone ? phoneX + PHONE.w / 2 : opts.extras.length ? x - GAP_IN * 0.75 : w;
	const sceneH = Math.max(h, withPhone ? PHONE.h : 0, ...opts.extras.map((e) => e.heightIn));
	const stage = createStage(canvas, sceneRight + w, sceneH);
	const { scene, renderer } = stage;

	// ---- textures, loaded around the page you're on ----
	const cache = new Map<number, THREE.Texture | null | 'loading'>();
	let ready = false;
	const want = (i: number) => {
		if (cache.has(i)) return;
		cache.set(i, 'loading');
		loadArt(renderer, pages[i], w / h, (tex) => {
			if (cache.get(i) !== 'loading') return tex?.dispose(); // evicted meanwhile
			cache.set(i, tex);
			if (i === 0) {
				if (tex) spine.color.copy(edgeColor(tex));
				if (!ready) (ready = true), onReady();
			}
		});
	};
	const evict = (keep: number[]) => {
		const lo = Math.min(...keep.filter((i) => i > 0)) - 6;
		const hi = Math.max(...keep.filter((i) => i < n - 1)) + 6;
		for (const [i, tex] of cache) {
			if (i === 0 || i === n - 1 || (i >= lo && i <= hi)) continue;
			if (tex && tex !== 'loading') tex.dispose();
			cache.delete(i);
		}
	};

	const cover = finishInfo(opts.finish);
	// Open pages dip into the gutter, which shades them near the spine.
	const gutterAtLeft = gutter(false);
	const gutterAtRight = gutter(true);
	const page = () => new THREE.MeshPhysicalMaterial({ color: 0xffffff, roughness: 0.62, envMapIntensity: 0.6 });
	/** Shows page i (or a blank page) on a material, styled as cover or inside page. */
	const show = (m: THREE.MeshPhysicalMaterial, i: number | null, shade?: THREE.Texture) => {
		const tex = i == null ? null : cache.get(i);
		const map = tex && tex !== 'loading' ? tex : null;
		const isCover = i === 0 || i === n - 1;
		if (m.map !== map) {
			if (!m.map !== !map) m.needsUpdate = true;
			m.map = map;
		}
		const ao = isCover ? null : (shade ?? null);
		if (m.aoMap !== ao) {
			if (!m.aoMap !== !ao) m.needsUpdate = true;
			m.aoMap = ao;
		}
		m.roughness = isCover ? cover.roughness : 0.62;
		m.clearcoat = isCover ? cover.clearcoat : 0;
		// A page still loading is a touch grey, so it reads as paper, not a hole.
		m.color.setHex(map || i == null ? 0xffffff : 0xeeeeee);
	};

	// ---- the book ----
	const spine = new THREE.MeshPhysicalMaterial({
		color: 0xdddddd, roughness: cover.roughness, clearcoat: cover.clearcoat,
		polygonOffset: true, polygonOffsetFactor: 1, polygonOffsetUnits: 1
	});
	const stack = (side: 1 | -1) => {
		const fore = paperEdge();
		fore.map = pageEdges(true);
		const ends = paperEdge();
		ends.map = pageEdges(false);
		const front = page();
		const back = page();
		// BoxGeometry face order: +x, -x, +y, -y, +z (front), -z (back). The
		// right stack's spine is its -x face; the left stack's is its +x face.
		const mesh = new THREE.Mesh(
			new THREE.BoxGeometry(1, 1, 1),
			side === 1 ? [fore, spine, ends, ends, front, back] : [spine, fore, ends, ends, front, back]
		);
		mesh.castShadow = true;
		return {
			mesh,
			front,
			back,
			/** Sizes the stack to hold `count` leaves, its back on z = 0. */
			fit(count: number) {
				const t = Math.max(count * tl, 0.0001);
				mesh.visible = count > 0;
				mesh.scale.set(w, h, t);
				mesh.position.set((side * w) / 2, h / 2, t / 2);
				fore.map!.repeat.set(t / tl / 64, 1);
				ends.map!.repeat.set(1, t / tl / 64);
			}
		};
	};
	const right = stack(1);
	const left = stack(-1);

	// The turning leaf: one curled surface, with a front and a back.
	const leafFrontGeo = new THREE.PlaneGeometry(w, h, SEGMENTS, 8);
	leafFrontGeo.translate(w / 2, h / 2, 0);
	const leafBackGeo = leafFrontGeo.clone();
	const uv = leafBackGeo.attributes.uv;
	for (let i = 0; i < uv.count; i++) uv.setX(i, 1 - uv.getX(i)); // read correctly from behind
	const flat = Float32Array.from(leafFrontGeo.attributes.position.array);
	const leafFrontMat = page();
	leafFrontMat.polygonOffset = true;
	leafFrontMat.polygonOffsetFactor = -1;
	leafFrontMat.polygonOffsetUnits = -2;
	const leafBackMat = page();
	leafBackMat.side = THREE.BackSide;
	leafBackMat.polygonOffset = true;
	leafBackMat.polygonOffsetFactor = -1;
	leafBackMat.polygonOffsetUnits = -2;
	const leaf = new THREE.Group();
	const leafFront = new THREE.Mesh(leafFrontGeo, leafFrontMat);
	const leafBack = new THREE.Mesh(leafBackGeo, leafBackMat);
	leaf.add(leafFront, leafBack);

	const curl = (turn: number) => {
		const pos = leafFrontGeo.attributes.position;
		const cols = SEGMENTS + 1;
		const ds = w / SEGMENTS;
		for (let row = 0; row < pos.count / cols; row++) {
			const y = flat[row * cols * 3 + 1];
			// The top corner leads a little, so the page turns on a diagonal.
			const lead = 1 + 0.3 * (y / h - 0.5);
			let px = 0;
			let pz = 0;
			for (let c = 0; c < cols; c++) {
				pos.setXYZ(row * cols + c, px, y, pz);
				const a = leafAngle(turn, ((c + 0.5) / SEGMENTS) * lead);
				px += Math.cos(a) * ds;
				pz += Math.sin(a) * ds;
			}
		}
		pos.needsUpdate = true;
		leafFrontGeo.computeVertexNormals();
		leafBackGeo.attributes.position.array.set(pos.array as Float32Array);
		leafBackGeo.attributes.position.needsUpdate = true;
		leafBackGeo.computeVertexNormals();
		leafFrontGeo.computeBoundingSphere();
		leafBackGeo.computeBoundingSphere();
	};

	// Saddle stitching: two staples through the fold, seen on the spine when
	// closed and in the gutter of the middle spread.
	const staples = new THREE.Group();
	const steel = new THREE.MeshPhysicalMaterial({
		color: 0xc9ccd2, metalness: 1, roughness: 0.25,
		polygonOffset: true, polygonOffsetFactor: -1, polygonOffsetUnits: -2
	});
	for (const sy of [0.25, 0.75]) {
		const s = new THREE.Mesh(new THREE.BoxGeometry(0.012, Math.min(0.5, h * 0.12), 0.012), steel);
		s.position.y = h * sy;
		staples.add(s);
	}
	staples.visible = opts.binding === 'staple';

	const book = new THREE.Group();
	book.add(right.mesh, left.mesh, leaf, staples);
	book.position.set(-w / 2, 0, -T / 2); // turn around the middle of the closed book
	const lean = new THREE.Group();
	lean.add(book);
	const spin = new THREE.Group();
	spin.position.x = w / 2;
	spin.add(lean);
	scene.add(spin);

	// ---- what ships with it, and the phone ----
	const extras = opts.extras.map((e, i) => {
		const sheet = createSheet(e, renderer, () => {});
		sheet.pivot.position.x = extraAt[i];
		sheet.pivot.rotation.y = -0.2;
		scene.add(sheet.pivot);
		return sheet;
	});
	if (withPhone) {
		const phone = createPhone();
		phone.position.set(phoneX, PHONE.h / 2, 0);
		scene.add(phone);
	}

	let lastLeaf = -1;
	const viewer: BookViewer = {
		angle: 0,
		zoom: 1,
		position: 0,
		leaves,
		resize: stage.resize,
		render() {
			const st = bookState(viewer.position, leaves);
			const open = Math.min(1, viewer.position);
			if (st.leaf !== lastLeaf) {
				lastLeaf = st.leaf;
				const near = pagesNear(viewer.position, n);
				near.forEach(want);
				evict(near);
			}
			const j = st.leaf;
			const turning = st.turn > 1e-4 && st.turn < 1 - 1e-4;
			leaf.visible = turning;
			if (!turning && st.turn <= 0.5) {
				right.fit(st.right + 1);
				left.fit(st.left);
				show(right.front, 2 * j < n ? 2 * j : null, gutterAtLeft);
				show(right.back, n - 1);
				show(left.front, j > 0 ? 2 * j - 1 : null, gutterAtRight);
				show(left.back, 0);
			} else if (!turning) {
				right.fit(0);
				left.fit(st.left + 1);
				show(left.front, 2 * j + 1 < n ? 2 * j + 1 : null, gutterAtRight);
				show(left.back, 0);
			} else {
				right.fit(st.right);
				left.fit(st.left);
				show(right.front, 2 * j + 2 < n ? 2 * j + 2 : null, gutterAtLeft);
				show(right.back, n - 1);
				show(left.front, j > 0 ? 2 * j - 1 : null, gutterAtRight);
				show(left.back, 0);
				show(leafFrontMat, 2 * j, gutterAtLeft);
				show(leafBackMat, 2 * j + 1 < n ? 2 * j + 1 : null, gutterAtRight);
				curl(st.turn);
				leaf.position.z = leafZ(st, tl);
			}

			// Staples: on the spine while closed, in the gutter at the middle spread.
			const mid = Math.abs(viewer.position - leaves / 2) < 0.5;
			const closedFront = viewer.position < 0.5;
			const closedBack = viewer.position > leaves - 0.5;
			const topStack = Math.max(st.left, st.right + (turning ? 0 : 1));
			for (const s of staples.children) {
				s.position.x = closedFront ? -0.008 : closedBack ? 0.008 : 0;
				s.position.z = mid ? topStack * tl + 0.008 : T / 2;
			}
			staples.visible = opts.binding === 'staple' && (closedFront || closedBack || mid);

			spin.rotation.y = viewer.angle;
			lean.rotation.x = THREE.MathUtils.lerp(LEAN, OPEN_LEAN, open);

			// Closed, frame everything (for scale); open, frame just the book.
			const bookLeft = -w * open;
			const bookRight = w * (1 - Math.max(0, viewer.position - (leaves - 1)));
			const frameRight = THREE.MathUtils.lerp(sceneRight, bookRight, open);
			const frameH = THREE.MathUtils.lerp(sceneH, h * 1.05, open);
			const zoomed = 1 - viewer.zoom;
			const cx = THREE.MathUtils.lerp((bookLeft + frameRight) / 2, (bookLeft + bookRight) / 2, Math.min(1, zoomed * 1.6));
			stage.frame(cx, frameRight - bookLeft, frameH, viewer.zoom);
			stage.render();
		},
		dispose() {
			gutterAtLeft.dispose();
			gutterAtRight.dispose();
			for (const tex of cache.values()) if (tex && tex !== 'loading') tex.dispose();
			cache.clear();
			extras.forEach((e) => e.dispose());
			stage.dispose();
		}
	};
	want(0);
	return viewer;
}

/** Shading that darkens the inner tenth of a page, toward the spine on the left (or right) edge. */
function gutter(atRight: boolean): THREE.CanvasTexture {
	const c = document.createElement('canvas');
	c.width = 64;
	c.height = 1;
	const ctx = c.getContext('2d')!;
	const g = ctx.createLinearGradient(atRight ? 64 : 0, 0, atRight ? 0 : 64, 0);
	g.addColorStop(0, '#5a5a5a');
	g.addColorStop(0.05, '#c8c8c8');
	g.addColorStop(0.14, '#ffffff');
	g.addColorStop(1, '#ffffff');
	ctx.fillStyle = g;
	ctx.fillRect(0, 0, 64, 1);
	const tex = new THREE.CanvasTexture(c);
	tex.colorSpace = THREE.NoColorSpace;
	return tex;
}

/** The cover's color along its spine edge, for the spine and fold. */
function edgeColor(tex: THREE.Texture): THREE.Color {
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

/**
 * Stacked page edges: 64 slightly uneven off-white lines per tile, one per
 * leaf. `across` runs them across the texture's u axis (the fore-edge),
 * otherwise along v (the top and bottom).
 */
function pageEdges(across: boolean): THREE.CanvasTexture {
	const N = 64;
	const c = document.createElement('canvas');
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
	const tex = new THREE.CanvasTexture(c);
	tex.wrapS = tex.wrapT = THREE.RepeatWrapping;
	tex.colorSpace = THREE.SRGBColorSpace;
	tex.anisotropy = 8;
	return tex;
}
