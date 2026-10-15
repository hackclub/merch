// Draws layoutHaul's table (haul.ts): every pile at true size with its art on
// top, and a phone lying beside them for scale, seen from above at an angle.
// Units are inches.
import * as THREE from 'three';
import type { layoutHaul } from './haul';
import { createPhone, createStage, loadArt, paperBand, paperEdge } from './paper3d';

export type HaulViewer = {
	/** Turn of the whole table, radians. */
	angle: number;
	/** How far above the table we look from, radians (drag up and down to change it). */
	elevation: number;
	/** 1 = everything in view; smaller is closer. */
	zoom: number;
	/** What's under a point on the canvas (-1…1 each way): "100 × Mini-Magazine · 4 in tall", or null. */
	pick(x: number, y: number): string | null;
	resize(width: number, height: number): void;
	render(): void;
	dispose(): void;
};

const PHONE_DEPTH = 0.31;
export const ELEVATION = THREE.MathUtils.degToRad(34); // low enough that stack heights read
const TABLE_TOP = 1; // inches thick

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
		pile.userData.label = `${p.label} · ${tall(p.height)}`;
		group.add(pile);
		if (p.band) {
			const band = paperBand(p.w, h, p.d, p.x, p.z);
			band.userData.label = `${pile.userData.label} · a banded bundle`;
			group.add(band);
		}
	}
	if (pending === 0) queueMicrotask(onReady);

	// Folding tables: a white plastic top with a grey rim; the piles sit on it.
	for (const t of table.tables) {
		const top = new THREE.MeshStandardMaterial({ color: 0xf6f5f1, roughness: 0.85 });
		const rim = new THREE.MeshStandardMaterial({ color: 0xb9b7b0, roughness: 0.7 });
		const slab = new THREE.Mesh(new THREE.BoxGeometry(t.w, TABLE_TOP, t.d), [rim, rim, top, rim, rim, rim]);
		slab.position.set(t.x, -TABLE_TOP / 2 - 0.01, t.z);
		slab.receiveShadow = true;
		group.add(slab);
	}

	// Lying flat, screen up.
	const phone = createPhone();
	phone.rotation.set(-Math.PI / 2, 0, 0);
	phone.position.set(table.phone.x, PHONE_DEPTH / 2, table.phone.z);
	phone.traverse((o) => (o.userData.label = 'A regular phone, about 6 in long, for scale'));
	group.add(phone);

	const ray = new THREE.Raycaster();

	const target = new THREE.Vector3(0, tallest * 0.3, 0);
	const viewer: HaulViewer = {
		angle: 0,
		elevation: ELEVATION,
		zoom: 1,
		pick(x, y) {
			ray.setFromCamera(new THREE.Vector2(x, y), camera);
			const hit = ray.intersectObjects(group.children, true).find((h) => h.object.userData.label);
			return hit ? hit.object.userData.label : null;
		},
		resize: stage.resize,
		render() {
			group.rotation.y = viewer.angle;
			const el = viewer.elevation;
			// Fit the table, whichever way it's turned, with a little room around it.
			const span = Math.hypot(table.width, table.depth);
			const half = THREE.MathUtils.degToRad(camera.fov) / 2;
			// Tables get a little more room: their corners swing nearest the camera.
			const room = table.tables.length ? 0.56 : 0.52;
			const fitW = (span * room) / (Math.tan(half) * camera.aspect);
			const fitH = (span * Math.sin(el) * room + tallest * Math.cos(el) * 0.5) / Math.tan(half);
			const dist = Math.max(fitW, fitH) * viewer.zoom;
			camera.near = Math.max(0.1, dist * 0.05);
			camera.far = dist * 4;
			camera.updateProjectionMatrix();
			camera.position.set(0, target.y + dist * Math.sin(el), dist * Math.cos(el));
			camera.lookAt(target);
			stage.render();
		},
		dispose: stage.dispose
	};
	return viewer;
}

/** "4 in tall (10 cm)", "0.7 in tall (1.8 cm)". */
function tall(inches: number): string {
	const n = inches >= 1 ? inches.toFixed(1).replace(/\.0$/, '') : inches.toFixed(2).replace(/0$/, '');
	const cm = inches * 2.54;
	return `${n} in tall (${cm >= 10 ? Math.round(cm) : cm.toFixed(1)} cm)`;
}
