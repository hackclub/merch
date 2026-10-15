// Shop tile interaction (Tile3D.svelte): turning with the pointer and
// flipping through booklets.

const HOVER_TURN = 2; // radians across the tile's full width

/** The angle while hovering: rest, turned by how far the pointer moved since it came in. */
export const hoverTurn = (rest: number, fromX: number, x: number, width: number) =>
	rest + ((x - fromX) / Math.max(1, width)) * HOVER_TURN;

/** The next leaf to flip to while hovering a booklet; it stays closed on the back cover. */
export const flipStep = (goal: number, leaves: number) => Math.min(leaves, goal + 1);

/** GIFs play on their own and photos stay still; anything else is a video we can pause. */
export const isImage = (url: string) => /\.(gif|jpe?g|png|webp|avif)$/i.test(url.split(/[?#]/)[0]);
