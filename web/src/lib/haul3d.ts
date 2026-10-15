// Draws layoutHaul's table (haul.ts): every pile at true size with its art on
// top, and a phone lying beside them for scale, seen from above at an angle.
// Units are inches.
import * as THREE from 'three';
import type { layoutHaul } from './haul';
import { createPhone, createStage, loadArt, paperEdge } from './paper3d';

export type HaulViewer = {
	/** Turn of the whole table, radians. */
	angle: number;
	resize(width: number, height: number): void;
	render(): void;
	dispose(): void;
};

const PHONE_DEPTH = 0.31;
const ELEVATION = THREE.MathUtils.degToRad(48); // how far above the table we look from

export function createHaulViewer(canvas: HTMLCanvasElement, table: ReturnType<typeof layoutHaul>, onReady: () => void): HaulViewer {
	const tallest = Math.max(0.5, ...table.piles.map((p) => p.height));
	const stage = createStage(canvas, table.width, Math.max(table.depth, tallest, 12));
	const { scene, renderer, camera } = stage;
	const group = new THREE.Group();
	scene.add(group);

	let pending = 0;
	for (const p of table.piles) {
		const top = paperEdge();
		top.polygonOffset = false;
		top.color.set(0xffffff);
		if (p.art) {
			pending++;
			loadArt(renderer, p.art, p.w / p.d, (tex) => {
				if (tex) {
					top.map = tex;
					top.needsUpdate = true;
				}
				if (--pending === 0) onReady();
			});
		}
		const edge = paperEdge();
		const h = Math.max(p.height, 0.01);
		// BoxGeometry faces: +x, -x, +y (top), -y, +z, -z.
		const pile = new THREE.Mesh(new THREE.BoxGeometry(p.w, h, p.d), [edge, edge, top, edge, edge, edge]);
		pile.position.set(p.x, h / 2, p.z);
		pile.castShadow = true;
		group.add(pile);
	}
	if (pending === 0) queueMicrotask(onReady);

	// Lying flat, screen up.
	const phone = createPhone();
	phone.rotation.set(-Math.PI / 2, 0, 0);
	phone.position.set(table.phone.x, PHONE_DEPTH / 2, table.phone.z);
	group.add(phone);

	const target = new THREE.Vector3(0, tallest * 0.3, 0);
	const viewer: HaulViewer = {
		angle: 0,
		resize: stage.resize,
		render() {
			group.rotation.y = viewer.angle;
			// Fit the table, whichever way it's turned, with a little room around it.
			const span = Math.hypot(table.width, table.depth);
			const half = THREE.MathUtils.degToRad(camera.fov) / 2;
			const fitW = (span * 0.5) / (Math.tan(half) * camera.aspect);
			const fitH = (span * Math.sin(ELEVATION) * 0.5 + tallest * Math.cos(ELEVATION) * 0.5) / Math.tan(half);
			const dist = Math.max(fitW, fitH);
			camera.near = Math.max(0.5, dist * 0.1);
			camera.far = dist * 4;
			camera.updateProjectionMatrix();
			camera.position.set(0, target.y + dist * Math.sin(ELEVATION), dist * Math.cos(ELEVATION));
			camera.lookAt(target);
			stage.render();
		},
		dispose: stage.dispose
	};
	return viewer;
}
