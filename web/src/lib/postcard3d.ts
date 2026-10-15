// WebGL view of a postcard request: the cards lying on a table, a banded bundle
// for every 50 and the rest in a loose stack (postcards.ts), next to a phone for
// scale. setCount() follows the quantity, so bundles appear as you ask for more.
// Units are inches.
import * as THREE from 'three';
import { createPhone, createStage, loadArt, pageEdges, paperBand, paperEdge, paperFace, PHONE_DEPTH, type ViewerOptions } from './paper3d';
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

const GAP = 1.5; // between the cards and the phone
const ELEVATION = THREE.MathUtils.degToRad(36);

export function createPostcardViewer(canvas: HTMLCanvasElement, opts: PostcardOptions, onReady: () => void): PostcardViewer {
	const { widthIn: w, heightIn: d } = opts;
	const t = opts.thicknessIn ?? CARD_IN;
	const withPhone = opts.phone !== false;
	const stage = createStage(canvas, w * 3 + 6, d * 3);
	const { scene, renderer } = stage;
	const turntable = new THREE.Group();
	scene.add(turntable);

	// One material for every stack's top (with the card stock's finish), so the art loads once.
	const face = paperFace(opts.finish, w, d);
	loadArt(renderer, opts.front, w / d, (tex) => {
		if (tex) {
			face.mat.map = tex;
			face.mat.needsUpdate = true;
		}
		onReady();
	});
	const bottom = paperEdge();

	const phone = createPhone(true);
	if (withPhone) turntable.add(phone);

	let stacks = new THREE.Group();
	turntable.add(stacks);
	let count = -1;
	let extent = { width: w, depth: d, tallest: t };

	function layout(n: number) {
		turntable.remove(stacks);
		stacks.traverse((o) => o instanceof THREE.Mesh && o.geometry.dispose());
		stacks = new THREE.Group();
		const { stacks: piles, width, depth } = postcardStacks(n, w, d);
		// The cards on the left, the phone lying beside them (when shown).
		const sceneW = withPhone ? width + GAP + PHONE.w : width;
		const shift = -sceneW / 2 + width / 2;
		let tallest = 0;
		for (const s of piles) {
			const h = Math.max(s.cards * t, t);
			tallest = Math.max(tallest, h);
			const side = paperEdge();
			if (s.cards > 1) {
				side.map = pageEdges(false);
				side.map.repeat.set(1, s.cards / 64);
			}
			// BoxGeometry faces: +x, -x, +y (top), -y, +z, -z.
			const pile = new THREE.Mesh(new THREE.BoxGeometry(w, h, d), [side, side, face.mat, bottom, side, side]);
			pile.position.set(s.x + shift, h / 2, s.z);
			pile.castShadow = true;
			stacks.add(pile);
			if (s.band) stacks.add(paperBand(w, h, d, s.x + shift, s.z));
		}
		if (withPhone) phone.position.set(shift + width / 2 + GAP + PHONE.w / 2, PHONE_DEPTH / 2, 0);
		turntable.add(stacks);
		extent = { width: sceneW, depth: withPhone ? Math.max(depth, PHONE.h) : depth, tallest: Math.max(tallest, withPhone ? PHONE_DEPTH : t) };
		stage.fitLight(extent.width, extent.depth);
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
			stage.frameTable(extent.width, extent.depth, extent.tallest, viewer.zoom, { elevation: ELEVATION, room: 0.55, y: extent.tallest * 0.4 });
			stage.render();
		},
		dispose() {
			face.dispose();
			stacks.traverse((o) => o instanceof THREE.Mesh && o.geometry.dispose());
			stage.dispose();
		}
	};
	viewer.setCount(1);
	return viewer;
}
