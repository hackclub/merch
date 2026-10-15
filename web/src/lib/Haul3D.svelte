<script lang="ts">
	// Everything in the request, to scale, piled close together next to a phone
	// (haul.ts, haul3d.ts), on 6-foot folding tables beside a laptop for clubs and
	// events, against a dark backdrop so the white table stands out. Made to
	// explore: drag to turn and tilt, pinch (or ctrl-scroll) or +/− to zoom, hover
	// or tap a pile to see what it is and how tall. The caption puts the total in
	// familiar terms, because people tend to ask for too little.
	import { onMount, untrack } from 'svelte';
	import { haulSize, layoutHaul, lineLabel, piles, tableCover, tall, withIncludes, type HaulLine } from './haul';
	import type { HaulViewer } from './haul3d';
	import type { Item } from './types';

	// table: lay it all out on 6-foot folding tables (for clubs and events), to show how much of one it fills.
	// compact: one item's amount (beside its clip while picking how many), with a one-line caption.
	let { lines, includes = [], table: onTable = false, compact = false }: { lines: HaulLine[]; includes?: Item[]; table?: boolean; compact?: boolean } = $props();

	const allLines = $derived(withIncludes(lines, includes));
	const stack = $derived(piles(lines, includes));
	const table = $derived(layoutHaul(stack, { table: onTable }));
	const shape = $derived(JSON.stringify(table));
	const cover = $derived(Math.round(tableCover(table) * 100));

	const START_ELEVATION = (34 * Math.PI) / 180;
	const MIN_ELEVATION = (8 * Math.PI) / 180;
	const MAX_ELEVATION = (85 * Math.PI) / 180;

	let stage: HTMLDivElement;
	let canvas = $state<HTMLCanvasElement>();
	let near = $state(false);
	let ready = $state(false);
	let viewer: HaulViewer | null = null;
	// The view people steer; until they touch it, it sways a little on its own.
	let turned = 0;
	let elevation = START_ELEVATION;
	let zoom = $state(1);
	let touched = $state(false);
	let tip = $state<{ text: string; x: number; y: number } | null>(null);

	onMount(() => {
		const io = new IntersectionObserver(([e]) => (near = e.isIntersecting), { rootMargin: '200px' });
		io.observe(stage);
		return () => io.disconnect();
	});

	// Build the WebGL viewer once while near the viewport; update the table in place when items or quantities change.
	$effect(() => {
		if (!near || !canvas) return;
		const el = canvas;
		return untrack(() => build(el));
	});

	$effect(() => {
		shape;
		viewer?.setTable(untrack(() => table));
	});

	function build(el: HTMLCanvasElement) {
		const still = matchMedia('(prefers-reduced-motion: reduce)').matches;
		let disposed = false;
		let raf = 0;
		const ro = new ResizeObserver(() => viewer?.resize(stage.clientWidth, stage.clientHeight));
		ready = false;
		import('./haul3d')
			.then(({ createHaulViewer }) => {
				if (disposed) return;
				const v = createHaulViewer(el, table, () => (ready = true));
				viewer = v;
				v.resize(stage.clientWidth, stage.clientHeight);
				ro.observe(stage);
				const tick = (now: number) => {
					raf = requestAnimationFrame(tick);
					if (document.hidden) return;
					// A wide table only sways a little, so it stays in frame.
					const sway = still || touched ? 0 : Math.sin(now / 2400) * (onTable ? 0.1 : 0.35);
					v.angle = turned + sway;
					v.elevation = elevation;
					// Ease toward the zoom people asked for.
					v.zoom += (zoom - v.zoom) * 0.2;
					v.render();
				};
				raf = requestAnimationFrame(tick);
			})
			.catch(() => {});
		return () => {
			disposed = true;
			cancelAnimationFrame(raf);
			ro.disconnect();
			viewer?.dispose();
			viewer = null;
			ready = false;
		};
	}

	// Pointers down, for dragging (one) and pinching (two).
	const pointers = new Map<number, { x: number; y: number }>();
	let dragged = false;
	let pinchFrom = 0;
	let zoomFrom = 1;

	const clampZoom = (z: number) => Math.min(1.4, Math.max(0.25, z));
	const spread = () => {
		const [a, b] = [...pointers.values()];
		return Math.hypot(a.x - b.x, a.y - b.y);
	};

	function show(e: PointerEvent) {
		if (!viewer) return;
		const r = stage.getBoundingClientRect();
		const text = viewer.pick(((e.clientX - r.left) / r.width) * 2 - 1, -((e.clientY - r.top) / r.height) * 2 + 1);
		tip = text ? { text, x: e.clientX - r.left, y: e.clientY - r.top } : null;
	}

	function down(e: PointerEvent) {
		if ((e.target as HTMLElement).closest('button')) return;
		pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
		dragged = false;
		if (pointers.size === 2) {
			pinchFrom = spread();
			zoomFrom = zoom;
		}
	}
	function move(e: PointerEvent) {
		const last = pointers.get(e.pointerId);
		if (!last) {
			if (e.pointerType === 'mouse') show(e);
			return;
		}
		const dx = e.clientX - last.x;
		const dy = e.clientY - last.y;
		if (pointers.size === 2) {
			pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
			zoom = clampZoom((zoomFrom * pinchFrom) / Math.max(1, spread()));
			return;
		}
		if (!dragged && Math.abs(dx) + Math.abs(dy) < 3) return;
		pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
		dragged = touched = true;
		tip = null;
		turned += dx * 0.01;
		elevation = Math.min(MAX_ELEVATION, Math.max(MIN_ELEVATION, elevation + dy * 0.006));
	}
	function up(e: PointerEvent) {
		if (!pointers.delete(e.pointerId)) return;
		// A tap (no drag) says what's there.
		if (!dragged && e.type === 'pointerup') show(e);
	}
	// Pinch on a trackpad arrives as ctrl-scroll; plain scrolling still scrolls the page.
	function wheel(e: WheelEvent) {
		if (!e.ctrlKey) return;
		e.preventDefault();
		touched = true;
		zoom = clampZoom(zoom * Math.exp(e.deltaY * 0.01));
	}
	function reset() {
		turned = 0;
		elevation = START_ELEVATION;
		zoom = 1;
		touched = false;
		tip = null;
	}
</script>

<figure class="haul" class:compact>
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="stage" class:ready class:table={onTable} bind:this={stage} onpointerdown={down} onpointermove={move} onpointerup={up}
		onpointercancel={up} onpointerleave={(e) => e.pointerType === 'mouse' && !pointers.size && (tip = null)} onwheel={wheel}>
		{#if near}<canvas bind:this={canvas} aria-label={onTable ? 'Your whole request in 3D, to scale, on a 6-foot folding table next to a laptop and a phone' : 'Your whole request in 3D, to scale, next to a phone'}></canvas>{/if}
		{#if !ready}<span class="loading muted">Loading 3D…</span>{/if}
		{#if tip}<span class="tip" style="left: {tip.x}px; top: {tip.y}px">{tip.text}</span>{/if}
		<div class="controls">
			<button type="button" aria-label="Zoom in" onclick={() => ((zoom = clampZoom(zoom / 1.35)), (touched = true))}>+</button>
			<button type="button" aria-label="Zoom out" onclick={() => ((zoom = clampZoom(zoom * 1.35)), (touched = true))}>−</button>
			{#if touched}<button type="button" aria-label="Reset the view" onclick={reset}>↺</button>{/if}
		</div>
	</div>
	{#if compact}
		<figcaption class="muted">{allLines.map(lineLabel).join(' · ')}, {tall(Math.max(...stack.map((p) => p.height)))} stacked up, next to a 6-inch phone.</figcaption>
	{:else}
	<figcaption>
		<span>
			Altogether it's {haulSize(stack)}{#if onTable}, and covers about {cover}% of {table.tables.length === 1 ? 'a 6-foot folding table' : `${table.tables.length} 6-foot folding tables`}{/if}.
			{#if onTable && cover < 35}Most people find they need more than they think, so don't be shy about adding more.{/if}
		</span>
		<span class="muted">To scale, next to {onTable ? 'a 13-inch laptop and ' : ''}a regular 6-inch phone. Drag to turn and tilt it, pinch or use + and − to zoom, and tap a pile to see what it is.</span>
		<span class="muted">{allLines.map(lineLabel).join(' · ')}</span>
	</figcaption>
	{/if}
</figure>

<style>
	.haul { margin: 0; display: grid; gap: 0.5rem; }
	.stage { position: relative; aspect-ratio: 16 / 10; background: var(--tile); touch-action: pan-y; cursor: grab; overflow: hidden; }
	.stage:active { cursor: grabbing; }
	.compact .stage { aspect-ratio: 4 / 3; }
	.stage.table { background: var(--dark); }
	.stage.table .loading { color: var(--smoke); }
	canvas { width: 100%; height: 100%; display: block; opacity: 0; transition: opacity 0.4s; }
	.ready canvas { opacity: 1; }
	.loading { position: absolute; inset: 0; display: grid; place-items: center; font-size: 0.9rem; }
	.tip {
		position: absolute; transform: translate(-50%, calc(-100% - 12px)); max-width: 16rem; padding: 0.35rem 0.6rem;
		background: var(--dark); color: white; font-size: 0.8rem; line-height: 1.3; pointer-events: none;
	}
	.controls { position: absolute; top: 0.5rem; right: 0.5rem; display: grid; gap: 0.25rem; }
	.controls button {
		width: 2.2rem; height: 2.2rem; padding: 0; font-size: 1.1rem; line-height: 1;
		background: white; color: var(--dark); border: 1px solid var(--smoke);
	}
	.controls button:hover { border-color: var(--dark); }
	figcaption { display: grid; gap: 0.2rem; font-size: 0.9rem; }
</style>
