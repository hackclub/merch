// Bookkeeping for the renderer shop tiles share (createStage in paper3d.ts):
// one WebGL canvas draws every tile's scene in its bottom-left corner, and
// each tile copies its frame onto its own 2D canvas.

export type Size = { w: number; h: number };

/** The shared canvas fits the largest view drawn on it; it only grows, so it's rarely reallocated. */
export const grow = (have: Size, need: Size): Size => ({ w: Math.max(have.w, need.w), h: Math.max(have.h, need.h) });

/** Where a w × h view drawn from GL's bottom-left corner sits in a canvas of height canvasH (top-left origin): [x, y, w, h]. */
export const sourceRect = (canvasH: number, w: number, h: number): [number, number, number, number] => [0, canvasH - h, w, h];
