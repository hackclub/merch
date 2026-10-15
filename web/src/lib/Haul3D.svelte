<script lang="ts">
	// Everything in the request, to scale, on a table next to a phone (haul.ts,
	// haul3d.ts). It sways gently; drag to turn it. Rebuilt when the bag changes.
	import { onMount, untrack } from 'svelte';
	import { layoutHaul, piles, type HaulLine } from './haul';
	import type { HaulViewer } from './haul3d';

	// table: lay it all out on 6-foot folding tables (for clubs and events), to show how much of one it fills.
	let { lines, table: onTable = false }: { lines: HaulLine[]; table?: boolean } = $props();

	const stack = $derived(piles(lines));
	const table = $derived(layoutHaul(stack, { table: onTable }));
	// Rebuild only when what's on the table changes, not on every re-render.
	const shape = $derived(JSON.stringify(table));

	let stage: HTMLDivElement;
	let canvas = $state<HTMLCanvasElement>();
	let near = $state(false);
	let ready = $state(false);
	let turned = 0; // where dragging left it
	let dragging = false;
	let lastX = 0;

	onMount(() => {
		const io = new IntersectionObserver(([e]) => (near = e.isIntersecting), { rootMargin: '200px' });
		io.observe(stage);
		return () => io.disconnect();
	});

	$effect(() => {
		shape;
		if (!near || !canvas) return;
		const el = canvas;
		return untrack(() => build(el));
	});

	function build(el: HTMLCanvasElement) {
		const still = matchMedia('(prefers-reduced-motion: reduce)').matches;
		let viewer: HaulViewer | null = null;
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
					v.angle = turned + (still || dragging ? 0 : Math.sin(now / 2400) * (onTable ? 0.1 : 0.35));
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
		};
	}

	function down(e: PointerEvent) {
		dragging = true;
		lastX = e.clientX;
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
	}
	function move(e: PointerEvent) {
		if (!dragging) return;
		turned += (e.clientX - lastX) * 0.01;
		lastX = e.clientX;
	}
	const up = () => (dragging = false);
</script>

<figure class="haul">
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="stage" class:ready bind:this={stage} onpointerdown={down} onpointermove={move} onpointerup={up} onpointercancel={up}>
		<!-- A fresh canvas per rebuild: disposing a viewer kills its canvas's WebGL context for good. -->
		{#if near}{#key shape}<canvas bind:this={canvas} aria-label="Your whole request in 3D, to scale, next to a phone"></canvas>{/key}{/if}
		{#if !ready}<span class="loading muted">Loading 3D…</span>{/if}
	</div>
	<figcaption>
		<span class="muted">
			{#if onTable}Everything you're getting, to scale, on {table.tables.length === 1 ? 'a 6-foot folding table' : `${table.tables.length} 6-foot folding tables`} with a phone for size.
			{:else}Everything you're getting, to scale, next to a regular 6-inch phone.{/if}
			Drag to turn it.
		</span>
		<span>{stack.map((p) => p.label).join(' · ')}</span>
	</figcaption>
</figure>

<style>
	.haul { margin: 0; display: grid; gap: 0.5rem; }
	.stage { position: relative; aspect-ratio: 16 / 10; background: var(--tile); touch-action: pan-y; cursor: grab; }
	canvas { width: 100%; height: 100%; display: block; opacity: 0; transition: opacity 0.4s; }
	.ready canvas { opacity: 1; }
	.loading { position: absolute; inset: 0; display: grid; place-items: center; font-size: 0.9rem; }
	figcaption { display: grid; gap: 0.15rem; font-size: 0.9rem; }
</style>
