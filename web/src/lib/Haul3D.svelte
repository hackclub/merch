<script lang="ts">
	// Everything in the request, to scale, piled close together next to a phone
	// (haul.ts, haul3d.ts), on 6-foot folding tables beside a laptop for clubs and
	// events, against a dark backdrop so the white table stands out. Made to
	// explore: drag to turn and tilt; scroll, pinch, double-tap or +/− to zoom in on a
	// spot; right- or shift-drag (two fingers on a phone) to move around; hover
	// or tap a pile to see what it is and how tall. The caption nudges people to ask
	// for more, because they tend to ask for too little.
	import { onMount, untrack } from 'svelte';
	import { layoutHaul, lineLabel, piles, tall, withIncludes, type HaulLine } from './haul';
	import type { HaulViewer } from './haul3d';
	import { clampPan, groundDelta, rotatePan, zoomToward, type Pan } from './haulview';
	import { meterView } from './track';
	import type { Item } from './types';

	// table: lay it all out on 6-foot folding tables (for clubs and events), to show how much of one it fills.
	// compact: one item's amount (beside its clip while picking how many), with a one-line caption.
	let { lines, includes = [], table: onTable = false, compact = false }: { lines: HaulLine[]; includes?: Item[]; table?: boolean; compact?: boolean } = $props();

	const allLines = $derived(withIncludes(lines, includes));
	const stack = $derived(piles(lines, includes));
	const table = $derived(layoutHaul(stack, { table: onTable }));
	const shape = $derived(JSON.stringify(table));

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
		const meter = meterView(el, 'haul', onTable ? 'table' : compact ? 'compact' : 'haul');
		import('./haul3d')
			.then(({ createHaulViewer }) => {
				if (disposed) return;
				const v = createHaulViewer(el, table, () => ((ready = true), meter.ready()));
				meter.loaded();
				v.folding = !still;
				viewer = v;
				v.resize(stage.clientWidth, stage.clientHeight);
				ro.observe(stage);
				const tick = (now: number) => {
					raf = requestAnimationFrame(tick);
					if (document.hidden) return meter.idle();
					// A wide table only sways a little, so it stays in frame.
					const sway = still || touched ? 0 : Math.sin(now / 2400) * (onTable ? 0.1 : 0.35);
					v.angle = turned + sway;
					v.elevation = elevation;
					// Ease toward the zoom and spot people asked for.
					v.zoom += (zoom - v.zoom) * 0.2;
					v.pan.x += (pan.x - v.pan.x) * 0.2;
					v.pan.z += (pan.z - v.pan.z) * 0.2;
					meter.draw(() => v.render());
				};
				raf = requestAnimationFrame(tick);
			})
			.catch((e) => meter.fail(e));
		return () => {
			disposed = true;
			cancelAnimationFrame(raf);
			ro.disconnect();
			viewer?.dispose();
			viewer = null;
			ready = false;
			meter.end();
		};
	}

	// Pointers down, for dragging (one) and pinching (two).
	const pointers = new Map<number, { x: number; y: number }>();
	let dragged = false;
	// Right, middle or shift-drag moves around the table instead of turning it.
	let panning = false;
	let pinch = { spread: 0, x: 0, y: 0 };
	let lastTap = { at: 0, x: 0, y: 0 };
	// The spot on the table in the middle of the view.
	let pan: Pan = { x: 0, z: 0 };

	const clampZoom = (z: number) => Math.min(1.6, Math.max(0.08, z));
	const twoFingers = () => {
		const [a, b] = [...pointers.values()];
		return { spread: Math.hypot(a.x - b.x, a.y - b.y), x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
	};
	// Inches on the table per pixel once the zoom settles.
	const scale = () => (viewer ? (viewer.scale() * zoom) / Math.max(0.01, viewer.zoom) : 0);
	const reach = () => viewer?.reach() ?? 0;

	// Zooms to z, keeping the spot under (x, y) on the page still.
	function zoomAt(x: number, y: number, z: number) {
		const r = stage.getBoundingClientRect();
		const off = groundDelta(x - r.left - r.width / 2, y - r.top - r.height / 2, scale(), elevation);
		const next = clampZoom(z);
		pan = clampPan(zoomToward(pan, { x: pan.x + off.x, z: pan.z + off.z }, zoom, next), reach());
		zoom = next;
		touched = true;
	}
	// Moves the table along with a drag of dx, dy pixels.
	function panBy(dx: number, dy: number) {
		const d = groundDelta(dx, dy, scale(), elevation);
		pan = clampPan({ x: pan.x - d.x, z: pan.z - d.z }, reach());
		touched = true;
	}
	// Turns the table about the middle of the view, not the middle of the table.
	function turnBy(by: number) {
		turned += by;
		pan = rotatePan(pan, by);
		if (viewer) viewer.pan = rotatePan(viewer.pan, by);
	}

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
		panning = e.button === 1 || e.button === 2 || e.shiftKey;
		if (pointers.size === 2) pinch = twoFingers();
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
			// Two fingers: pinch to zoom on the spot between them, and slide to move around.
			pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
			const now = twoFingers();
			zoomAt(now.x, now.y, (zoom * pinch.spread) / Math.max(1, now.spread));
			panBy(now.x - pinch.x, now.y - pinch.y);
			pinch = now;
			dragged = true;
			tip = null;
			return;
		}
		if (!dragged && Math.abs(dx) + Math.abs(dy) < 3) return;
		pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
		dragged = touched = true;
		tip = null;
		if (panning) return panBy(dx, dy);
		turnBy(dx * 0.01);
		elevation = Math.min(MAX_ELEVATION, Math.max(MIN_ELEVATION, elevation + dy * 0.006));
	}
	function up(e: PointerEvent) {
		if (!pointers.delete(e.pointerId)) return;
		if (dragged || e.type !== 'pointerup') return;
		// A double tap zooms in on that spot; a single tap says what's there.
		const now = performance.now();
		if (now - lastTap.at < 350 && Math.hypot(e.clientX - lastTap.x, e.clientY - lastTap.y) < 30) {
			zoomAt(e.clientX, e.clientY, zoom / 2);
			lastTap.at = 0;
			tip = null;
			return;
		}
		lastTap = { at: now, x: e.clientX, y: e.clientY };
		show(e);
	}
	// Scrolling over the view zooms toward the pointer (a trackpad pinch arrives as ctrl-scroll, in smaller steps).
	function wheel(e: WheelEvent) {
		// The small previews between rows leave plain scrolling to the page.
		if (compact && !e.ctrlKey) return;
		e.preventDefault();
		const delta = e.deltaY * (e.deltaMode === 1 ? 16 : 1);
		zoomAt(e.clientX, e.clientY, zoom * Math.exp(delta * (e.ctrlKey ? 0.01 : 0.0015)));
	}
	function reset() {
		turned = 0;
		elevation = START_ELEVATION;
		zoom = 1;
		pan = { x: 0, z: 0 };
		touched = false;
		tip = null;
	}
</script>

<figure class="haul" class:compact>
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="stage" class:ready class:table={onTable} bind:this={stage} onpointerdown={down} onpointermove={move} onpointerup={up}
		onpointercancel={up} onpointerleave={(e) => e.pointerType === 'mouse' && !pointers.size && (tip = null)} onwheel={wheel} oncontextmenu={(e) => e.preventDefault()}>
		{#if near}<canvas bind:this={canvas} data-gl={compact ? 'shared' : undefined} aria-label={onTable ? 'Your whole request in 3D, to scale, on a 6-foot folding table next to a laptop and a phone' : 'Your whole request in 3D, to scale, next to a phone'}></canvas>{/if}
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
	<figcaption>Most people find they need more than they think, so don't be shy about adding more.</figcaption>
	{/if}
</figure>

<style>
	.haul { margin: 0; display: grid; gap: 0.5rem; }
	.stage { position: relative; aspect-ratio: 16 / 10; background: var(--tile); touch-action: none; cursor: grab; overflow: hidden; }
	.stage:active { cursor: grabbing; }
	.compact .stage { aspect-ratio: 4 / 3; touch-action: pan-y; }
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
