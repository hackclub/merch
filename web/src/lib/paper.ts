// How printed paper looks in the 3D preview. Pure data and math; the WebGL
// side lives in paper3d.ts.

export type Finish = '' | 'matte' | 'gloss' | 'linen' | 'uncoated';

export type FinishInfo = {
	label: string; // shown under the preview
	roughness: number; // 0 = mirror, 1 = chalk
	clearcoat: number; // gloss coating on top of the ink
	weave: boolean; // linen's embossed crosshatch
	grain: number; // strength of the paper-fiber bump
	sheen: number; // soft velvety glow at grazing angles, the look of uncoated and matte stock
};

const FINISHES: Record<Finish, FinishInfo> = {
	'': { label: '', roughness: 0.8, clearcoat: 0, weave: false, grain: 0.15, sheen: 0.1 },
	matte: { label: 'Matte paper', roughness: 0.88, clearcoat: 0, weave: false, grain: 0.25, sheen: 0.2 },
	gloss: { label: 'Glossy paper', roughness: 0.35, clearcoat: 0.8, weave: false, grain: 0.05, sheen: 0 },
	linen: { label: 'Linen card stock', roughness: 0.8, clearcoat: 0, weave: true, grain: 0.6, sheen: 0.15 },
	uncoated: { label: 'Uncoated paper', roughness: 0.95, clearcoat: 0, weave: false, grain: 0.45, sheen: 0.3 }
};

export const finishInfo = (f: string): FinishInfo => FINISHES[(f in FINISHES ? f : '') as Finish];

/** How deep the fiber mottle is, in texels of height per unit of grain (tuned by eye). */
const RELIEF = 2;

/** Inches of paper one tile of the surface texture covers. */
export const SURFACE_TILE_IN = 1;

/**
 * The paper's surface as an n × n tiling heightfield over SURFACE_TILE_IN,
 * scaled by the finish's grain. Fiber clumps a few millimeters to a fraction
 * of a millimeter across (mottle you can still see on a poster at arm's
 * length), plus linen's embossed crosshatch. Deterministic.
 */
export function paperHeight(n: number, info: FinishInfo): Float32Array {
	let seed = 1337;
	const rand = () => ((seed = (seed * 1664525 + 1013904223) >>> 0) / 4294967296);
	const height = new Float32Array(n * n);
	const smooth = (t: number) => t * t * (3 - 2 * t);
	// Value-noise octaves from ~3 mm down to ~0.2 mm; the coarse ones carry most of the
	// relief so the tooth survives mipmapping when the sheet is small on screen.
	for (const [mm, amp] of [[3.2, 1], [1.6, 0.7], [0.8, 0.45], [0.4, 0.3], [0.2, 0.2]]) {
		const cells = Math.max(1, Math.round((SURFACE_TILE_IN * 25.4) / mm));
		const lattice = Float32Array.from({ length: cells * cells }, () => rand() * 2 - 1);
		const at = (x: number, y: number) => lattice[(y % cells) * cells + (x % cells)];
		for (let y = 0; y < n; y++) {
			const fy = (y / n) * cells;
			const y0 = Math.floor(fy);
			const ty = smooth(fy - y0);
			for (let x = 0; x < n; x++) {
				const fx = (x / n) * cells;
				const x0 = Math.floor(fx);
				const tx = smooth(fx - x0);
				const top = at(x0, y0) + (at(x0 + 1, y0) - at(x0, y0)) * tx;
				const bottom = at(x0, y0 + 1) + (at(x0 + 1, y0 + 1) - at(x0, y0 + 1)) * tx;
				height[y * n + x] += (top + (bottom - top) * ty) * amp * info.grain * RELIEF;
			}
		}
	}
	if (info.weave) {
		// ~40 threads per inch each way, with slightly uneven thread thickness.
		const threads = Math.round(40 * SURFACE_TILE_IN);
		const rowJitter = Array.from({ length: threads }, () => 0.6 + rand() * 0.8);
		const colJitter = Array.from({ length: threads }, () => 0.6 + rand() * 0.8);
		for (let y = 0; y < n; y++) {
			for (let x = 0; x < n; x++) {
				const fy = (y / n) * threads;
				const fx = (x / n) * threads;
				const row = Math.sin(fy * Math.PI * 2) * 0.5 + 0.5;
				const col = Math.sin(fx * Math.PI * 2) * 0.5 + 0.5;
				height[y * n + x] += (row * rowJitter[Math.floor(fy)] + col * colJitter[Math.floor(fx)]) * 0.54;
			}
		}
	}
	return height;
}

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
