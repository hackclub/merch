// WebGL view of a postcard request: the cards lying on a table, a banded bundle
// for every 50 and the rest in a loose stack (postcards.ts), next to a phone for
// scale. setCount() follows the quantity, so bundles appear as you ask for more.
// The last stack turns over as one (flipped, 0 → 1) to show the back.
// Units are inches.
import * as THREE from 'three';
import { createPhone, createStage, loadArt, pageEdges, paperBand, paperEdge, paperFace, PHONE_DEPTH, type ViewerOptions } from './paper3d';
import { cardFlip, CARD_IN, postcardStacks } from './postcards';
import { PHONE } from './scale';

export type PostcardOptions = Omit<ViewerOptions, 'thicknessIn'> & { kind: 'postcards'; thicknessIn: number | null };

export type PostcardViewer = {
	angle: number;
	zoom: number;
	flipped: number; // 0 front up, 1 the last stack turned over to its back
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
	// The last stack's underside: the back art, or plain card when there's none.
	const backFace = paperFace(opts.finish, w, d);
	if (opts.back) {
		loadArt(renderer, opts.back, w / d, (tex) => {
			if (tex) {
				backFace.mat.map = tex;
				backFace.mat.needsUpdate = true;
			}
		});
	}
	// The last stack (the loose cards, or the last bundle) turns over as one, so
	// no other card shows up from under it.
	let flipper = new THREE.Group();
	let flipRest = { x: 0, y: t / 2, z: 0 };

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
		piles.forEach((s, i) => {
			const h = Math.max(s.cards * t, t);
			tallest = Math.max(tallest, h);
			const last = i === piles.length - 1;
			const side = paperEdge();
			if (s.cards > 1) {
				side.map = pageEdges(false);
				side.map.repeat.set(1, s.cards / 64);
			}
			// BoxGeometry faces: +x, -x, +y (top), -y, +z, -z. Turned over about x, the
			// -y face comes up with its art upright.
			const pile = new THREE.Mesh(new THREE.BoxGeometry(w, h, d), [side, side, face.mat, last ? backFace.mat : bottom, side, side]);
			pile.castShadow = true;
			const group = last ? new THREE.Group() : stacks;
			pile.position.set(last ? 0 : s.x + shift, last ? 0 : h / 2, last ? 0 : s.z);
			group.add(pile);
			if (s.band) {
				const band = paperBand(w, h, d, last ? 0 : s.x + shift, last ? 0 : s.z);
				if (last) band.position.y = 0;
				group.add(band);
			}
			if (last) {
				flipper = group;
				flipRest = { x: s.x + shift, y: h / 2, z: s.z };
				stacks.add(flipper);
			}
		});
		if (withPhone) phone.position.set(shift + width / 2 + GAP + PHONE.w / 2, PHONE_DEPTH / 2, 0);
		turntable.add(stacks);
		extent = { width: sceneW, depth: withPhone ? Math.max(depth, PHONE.h) : depth, tallest: Math.max(tallest, withPhone ? PHONE_DEPTH : t) };
		stage.fitLight(extent.width, extent.depth);
	}

	const viewer: PostcardViewer = {
		angle: 0,
		zoom: 1,
		flipped: 0,
		setCount(n) {
			if (n === count) return;
			count = n;
			layout(n);
		},
		resize: stage.resize,
		render() {
			turntable.rotation.y = viewer.angle;
			const pose = cardFlip(viewer.flipped, d);
			flipper.position.set(flipRest.x, flipRest.y + pose.lift, flipRest.z);
			flipper.rotation.x = pose.rotX;
			stage.frameTable(extent.width, extent.depth, extent.tallest, viewer.zoom, { elevation: ELEVATION, room: 0.55, y: extent.tallest * 0.4 });
			stage.render();
		},
		dispose() {
			face.dispose();
			backFace.dispose();
			stacks.traverse((o) => o instanceof THREE.Mesh && o.geometry.dispose());
			stage.dispose();
		}
	};
	viewer.setCount(1);
	return viewer;
}
