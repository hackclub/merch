// WebGL view of a 3D model of the real thing (a GLB file: the Orpheus plushie),
// standing beside a phone at true scale like the printed items (paper3d.ts) and
// turning the same way. The haul sets copies on the table (loadModel). Units are inches.
import * as THREE from 'three';
import { GLTFLoader, type GLTF } from 'three/examples/jsm/loaders/GLTFLoader.js';
import { MeshoptDecoder } from 'three/examples/jsm/libs/meshopt_decoder.module.js';
import { modelSize, type ModelOptions } from './model';
import { createPhone, createStage, type Viewer } from './paper3d';
import { GAP_IN, PHONE } from './scale';

// Parsed files, shared by every view on the page: shop tiles come and go as you
// scroll, and each would otherwise download and parse the file again.
const files = new Map<string, Promise<GLTF>>();
function loadFile(url: string): Promise<GLTF> {
	let file = files.get(url);
	if (!file) {
		const loader = new GLTFLoader().setMeshoptDecoder(MeshoptDecoder);
		loader.setCrossOrigin('anonymous');
		file = loader.loadAsync(url);
		file.catch(() => files.delete(url)); // try again next time
		files.set(url, file);
	}
	return file;
}

export type LoadedModel = {
	group: THREE.Group;
	w: number; // side to side, inches
	h: number;
	d: number; // front to back
};

/** A copy of the model heightIn tall, standing on y = 0, centered on x and z, facing +z (the camera). */
export async function loadModel(url: string, heightIn: number): Promise<LoadedModel> {
	const gltf = await loadFile(url);
	const inner = gltf.scene.clone(true);
	const box = new THREE.Box3().setFromObject(inner);
	const size = box.getSize(new THREE.Vector3());
	const center = box.getCenter(new THREE.Vector3());
	const s = heightIn / size.y;
	inner.scale.setScalar(s);
	inner.position.set(-center.x * s, -box.min.y * s, -center.z * s);
	inner.traverse((o) => {
		if (o instanceof THREE.Mesh) o.castShadow = o.receiveShadow = true;
	});
	const group = new THREE.Group();
	group.add(inner);
	return { group, w: size.x * s, h: heightIn, d: size.z * s };
}

export async function createModelViewer(canvas: HTMLCanvasElement, opts: ModelOptions, onReady: () => void): Promise<Viewer> {
	const withPhone = opts.phone !== false;
	const model = await loadModel(opts.url, modelSize({ width_in: opts.widthIn, height_in: opts.heightIn }).h);
	// It turns, so give it room for its widest side.
	const w = Math.max(model.w, model.d);
	const h = model.h;
	const sceneW = withPhone ? w + GAP_IN + PHONE.w : w;
	const sceneH = withPhone ? Math.max(h, PHONE.h) : h;
	const left = -sceneW / 2;
	const stage = createStage(canvas, sceneW, sceneH);

	const pivot = new THREE.Group();
	pivot.position.set(left + w / 2, 0, 0);
	pivot.add(model.group);
	stage.scene.add(pivot);
	if (withPhone) {
		const phone = createPhone();
		phone.position.set(left + w + GAP_IN + PHONE.w / 2, PHONE.h / 2, 0);
		stage.scene.add(phone);
	}
	onReady();

	const viewer: Viewer = {
		angle: 0,
		zoom: 1,
		resize: stage.resize,
		render() {
			pivot.rotation.y = viewer.angle;
			// Frame the whole scene at zoom 1; zooming moves toward the model.
			const x = THREE.MathUtils.lerp(0, left + w / 2, Math.min(1, (1 - viewer.zoom) * 1.6));
			stage.frame(x, sceneW, sceneH, viewer.zoom);
			stage.render();
		},
		dispose: stage.dispose
	};
	return viewer;
}
