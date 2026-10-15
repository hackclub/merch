// WebGL view of a postcard request: the cards lying on a table, a banded bundle
// for every 50 and the rest in a loose stack (postcards.ts), next to a phone for
// scale. setCount() follows the quantity, so bundles appear as you ask for more.
// Units are inches.
import * as THREE from 'three';
import { createPhone, createStage, loadArt, paperBand, paperEdge, type ViewerOptions } from './paper3d';
import { CARD_IN, postcardStacks } from './postcards';
import { PHONE } from './scale';

export type PostcardOptions = Omit<ViewerOptions, 'thicknessIn'> & { kind: 'postcards'; thicknessIn: number | null };

export type PostcardViewer = {
	angle: number;
	zoom: number;
	setCount(n: number): void;
	resize(width: number, height: number): void;
	render(): void;
	dispose(): void;
};

const PHONE_DEPTH = 0.31;
const GAP = 1.5; // between the cards and the phone
const ELEVATION = THREE.MathUtils.degToRad(36);

export function createPostcardViewer(canvas: HTMLCanvasElement, opts: PostcardOptions, onReady: () => void): PostcardViewer {
	const { widthIn: w, heightIn: d } = opts;
	const t = opts.thicknessIn ?? CARD_IN;
	const stage = createStage(canvas, w * 3 + 6, d * 3);
	const { scene, renderer, camera } = stage;
	const turntable = new THREE.Group();
	scene.add(turntable);

	// One material for every stack's top, so the art loads once.
	const top = paperEdge();
	top.polygonOffset = false;
	top.color.set(0xffffff);
	loadArt(renderer, opts.front, w / d, (tex) => {
		if (tex) {
			top.map = tex;
			top.needsUpdate = true;
		}
		onReady();
	});
	const edge = paperEdge();

	const phone = createPhone();
	phone.rotation.set(-Math.PI / 2, 0, 0);
	turntable.add(phone);

	let stacks = new THREE.Group();
	turntable.add(stacks);
	let count = -1;
	let extent = { width: w, depth: d, tallest: t };

	function layout(n: number) {
		turntable.remove(stacks);
		stacks.traverse((o) => o instanceof THREE.Mesh && o.geometry.dispose());
		stacks = new THREE.Group();
		const { stacks: piles, width, depth } = postcardStacks(n, w, d);
		// The cards on the left, the phone lying beside them.
		const sceneW = width + GAP + PHONE.w;
		const shift = -sceneW / 2 + width / 2;
		let tallest = 0;
		for (const s of piles) {
			const h = Math.max(s.cards * t, t);
			tallest = Math.max(tallest, h);
			// BoxGeometry faces: +x, -x, +y (top), -y, +z, -z.
			const pile = new THREE.Mesh(new THREE.BoxGeometry(w, h, d), [edge, edge, top, edge, edge, edge]);
			pile.position.set(s.x + shift, h / 2, s.z);
			pile.castShadow = true;
			stacks.add(pile);
			if (s.band) stacks.add(paperBand(w, h, d, s.x + shift, s.z));
		}
		phone.position.set(shift + width / 2 + GAP + PHONE.w / 2, PHONE_DEPTH / 2, 0);
		turntable.add(stacks);
		extent = { width: sceneW, depth: Math.max(depth, PHONE.h), tallest: Math.max(tallest, PHONE_DEPTH) };
	}

	const viewer: PostcardViewer = {
		angle: 0,
		zoom: 1,
		setCount(n) {
			if (n === count) return;
			count = n;
			layout(n);
		},
		resize: stage.resize,
		render() {
			turntable.rotation.y = viewer.angle;
			// Fit the cards and phone, whichever way they're turned.
			const span = Math.hypot(extent.width, extent.depth);
			const half = THREE.MathUtils.degToRad(camera.fov) / 2;
			const fitW = (span * 0.55) / (Math.tan(half) * camera.aspect);
			const fitH = (span * Math.sin(ELEVATION) * 0.55 + extent.tallest * Math.cos(ELEVATION)) / Math.tan(half);
			const dist = Math.max(fitW, fitH) * viewer.zoom;
			camera.near = Math.max(0.1, dist * 0.05);
			camera.far = dist * 4;
			camera.updateProjectionMatrix();
			const lookY = extent.tallest * 0.4;
			camera.position.set(0, lookY + dist * Math.sin(ELEVATION), dist * Math.cos(ELEVATION));
			camera.lookAt(0, lookY, 0);
			stage.render();
		},
		dispose() {
			stacks.traverse((o) => o instanceof THREE.Mesh && o.geometry.dispose());
			stage.dispose();
		}
	};
	viewer.setCount(1);
	return viewer;
}
