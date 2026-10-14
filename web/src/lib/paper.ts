// How printed paper looks in the 3D preview. Pure data and math; the WebGL
// side lives in paper3d.ts.

export type Finish = '' | 'matte' | 'gloss' | 'linen' | 'uncoated';

export type FinishInfo = {
	label: string; // shown under the preview
	roughness: number; // 0 = mirror, 1 = chalk
	clearcoat: number; // gloss coating on top of the ink
	weave: boolean; // linen's embossed crosshatch
	grain: number; // strength of the paper-fiber bump
};

const FINISHES: Record<Finish, FinishInfo> = {
	'': { label: '', roughness: 0.8, clearcoat: 0, weave: false, grain: 0.15 },
	matte: { label: 'Matte paper', roughness: 0.88, clearcoat: 0, weave: false, grain: 0.25 },
	gloss: { label: 'Glossy paper', roughness: 0.35, clearcoat: 0.8, weave: false, grain: 0.05 },
	linen: { label: 'Linen texture', roughness: 0.8, clearcoat: 0, weave: true, grain: 0.6 },
	uncoated: { label: 'Uncoated paper', roughness: 0.95, clearcoat: 0, weave: false, grain: 0.45 }
};

export const finishInfo = (f: string): FinishInfo => FINISHES[(f in FINISHES ? f : '') as Finish];

/** Typical sheet when no thickness was recorded: ~0.2 mm, like 100 lb text poster paper. */
export const sheetThickness = (it: { thickness_in: number | null }) => it.thickness_in ?? 0.008;

export type Crop = { repeatX: number; repeatY: number; offsetX: number; offsetY: number };

/** Texture repeat/offset that fills a sheet with art of another aspect ratio (w/h), trimming evenly. */
export function coverCrop(artAspect: number, sheetAspect: number): Crop {
	if (Math.abs(artAspect - sheetAspect) < 1e-9) return { repeatX: 1, repeatY: 1, offsetX: 0, offsetY: 0 };
	if (sheetAspect > artAspect) {
		const repeatY = artAspect / sheetAspect;
		return { repeatX: 1, repeatY, offsetX: 0, offsetY: (1 - repeatY) / 2 };
	}
	const repeatX = sheetAspect / artAspect;
	return { repeatX, repeatY: 1, offsetX: (1 - repeatX) / 2, offsetY: 0 };
}
