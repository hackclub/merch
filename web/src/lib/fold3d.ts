// WebGL view of a trifold brochure (fold.ts has the folding): one sheet in
// three panels with real thickness and the paper's surface, hinged at its two
// creases, so it opens and folds back up like the real thing. It stands on its
// bottom edge, leaning back a touch like the sheet viewer (paper3d.ts), beside a
// phone for scale. Units are inches.
import * as THREE from 'three';
import { FOLD_STEPS, foldAngles, panelU, type Panel } from './fold';
import { finishInfo } from './paper';
import { createPhone, createStage, LEAN, loadArt, paperEdge, paperMaterial, paperSurface, type Viewer } from './paper3d';
import { GAP_IN, PHONE } from './scale';

export type FoldOptions = {
	outside: string; // the outside art, reading flap | back | cover
	inside: string; // the other side of the sheet
	widthIn: number; // the sheet, open flat
	heightIn: number;
	thicknessIn: number; // one sheet
	finish: string;
	phone?: boolean; // stand a phone beside it for scale (default true)
};

export type FoldViewer = Viewer & {
	/** 0 = folded on the cover, 1 = cover open, 2 = open flat; fractions are mid-fold. */
	position: number;
	readonly leaves: number;
};

/**
 * One panel, pw × h × t, with its third of the inside art on +z and of the
 * outside art on -z (box faces' u runs with x on +z and against it on -z).
 */
function panelGeometry(pw: number, h: number, t: number, panel: Panel): THREE.BoxGeometry {
	const geo = new THREE.BoxGeometry(pw, h, t);
	const pos = geo.attributes.position;
	const normal = geo.attributes.normal;
	const uv = geo.attributes.uv;
	const [i0, i1] = panelU(panel, 'inside');
	const [o0, o1] = panelU(panel, 'outside');
	for (let i = 0; i < uv.count; i++) {
		const nz = normal.getZ(i);
		if (Math.abs(nz) < 0.5) continue;
		const s = pos.getX(i) / pw + 0.5; // 0 at the panel's -x edge
		uv.setX(i, nz > 0 ? i0 + s * (i1 - i0) : o0 + (1 - s) * (o1 - o0));
	}
	return geo;
}

export function createFoldViewer(canvas: HTMLCanvasElement, opts: FoldOptions, onReady: () => void): FoldViewer {
	const { widthIn: w, heightIn: h } = opts;
	const t = Math.max(opts.thicknessIn, 0.003);
	const pw = w / 3;
	const withPhone = opts.phone !== false;
	// Laid out open flat, then the phone; centered on x = 0.
	const sceneW = withPhone ? w + GAP_IN + PHONE.w : w;
	const sceneH = withPhone ? Math.max(h, PHONE.h) : h;
	const stage = createStage(canvas, sceneW, sceneH);
	const { scene, renderer } = stage;
	const left = -sceneW / 2;
	const middle = left + w / 2; // the back panel, which stays put

	const info = finishInfo(opts.finish);
	const surface = paperSurface(info);
	surface.normal.repeat.set(w / surface.tileIn, h / surface.tileIn);
	let pending = 2;
	const art = (url: string) => {
		const m = paperMaterial(info, surface.normal);
		loadArt(renderer, url, w / h, (tex) => {
			if (tex) {
				m.map = tex;
				m.needsUpdate = true;
			}
			if (--pending === 0) onReady();
		});
		return m;
	};
	const inside = art(opts.inside);
	const outside = art(opts.outside);
	const edge = paperEdge();
	const panel = (which: Panel) => {
		// BoxGeometry face order: +x, -x, +y, -y, +z (inside), -z (outside).
		const mesh = new THREE.Mesh(panelGeometry(pw, h, t, which), [edge, edge, edge, edge, inside, outside]);
		mesh.castShadow = true;
		return mesh;
	};

	// Open flat, the inside faces +z: cover | back | flap. Each outer panel
	// swings in toward +z on a hinge at its crease, the flap first so it ends up
	// between the back and the cover. The hinges sit a sheet (and a hair) apart,
	// so the folded panels stack instead of passing through each other.
	const gap = t * 0.3;
	const back = panel('back');
	back.position.y = h / 2;
	const flapHinge = new THREE.Group();
	flapHinge.position.set(pw / 2, 0, t / 2 + gap / 2);
	const flap = panel('flap');
	flap.position.set(pw / 2, h / 2, -(t / 2 + gap / 2));
	flapHinge.add(flap);
	const coverHinge = new THREE.Group();
	coverHinge.position.set(-pw / 2, 0, (3 * t) / 2 + gap);
	const cover = panel('cover');
	cover.position.set(-pw / 2, h / 2, -((3 * t) / 2 + gap));
	coverHinge.add(cover);

	const lean = new THREE.Group();
	lean.rotation.x = LEAN;
	lean.add(back, flapHinge, coverHinge);
	const pivot = new THREE.Group();
	pivot.position.x = middle;
	pivot.add(lean);
	scene.add(pivot);

	const phoneRight = left + sceneW;
	if (withPhone) {
		const phone = createPhone();
		phone.position.set(phoneRight - PHONE.w / 2, PHONE.h / 2, 0);
		scene.add(phone);
	}

	const viewer: FoldViewer = {
		angle: 0,
		zoom: 1,
		position: 0,
		leaves: FOLD_STEPS,
		resize: stage.resize,
		render() {
			const a = foldAngles(viewer.position);
			flapHinge.rotation.y = -a.flap;
			coverHinge.rotation.y = a.cover;
			pivot.rotation.y = viewer.angle;

			// Frame what's there: folded, it's a third as wide, so the camera
			// comes in close and pulls back as it opens. A panel mid-swing sticks
			// out toward the camera, which makes it look wider.
			const reach = (fold: number) => pw / 2 + Math.max(0, pw * Math.cos(fold)) + 0.4 * pw * Math.sin(fold);
			const reachL = reach(a.cover); // the cover swings out to the left
			const reachR = reach(a.flap); // the flap to the right
			const c = Math.cos(viewer.angle);
			const x0 = middle - Math.max(pw / 2, (c >= 0 ? reachL : reachR) * Math.abs(c));
			const x1 = withPhone ? phoneRight : middle + Math.max(pw / 2, (c >= 0 ? reachR : reachL) * Math.abs(c));
			const zoomed = 1 - viewer.zoom; // 0 at full view; zooming moves toward the brochure
			const x = THREE.MathUtils.lerp((x0 + x1) / 2, middle, Math.min(1, zoomed * 1.6));
			stage.frame(x, x1 - x0, sceneH, viewer.zoom);
			stage.render();
		},
		dispose() {
			stage.dispose();
		}
	};
	return viewer;
}
