<script lang="ts">
	// A shop-grid tile's 3D preview (see items.ts preview3D): the print, closed
	// booklet or stickers, spinning slowly on its own. Drag to turn it; a click
	// still follows the tile's link. Viewers exist only while the tile is near
	// the screen, so a long grid never runs out of WebGL contexts. The photo
	// underneath stays until the 3D view is ready, and if WebGL fails.
	import { onMount, untrack } from 'svelte';
	import type { Viewer, ViewerOptions } from './paper3d';
	import type { StickerOptions, StickerViewer } from './sticker3d';

	// count: stickers in the bag; phase: starting angle, so tiles don't turn in step.
	let { opts, count = 0, phase = 0 }: { opts: ViewerOptions | StickerOptions; count?: number; phase?: number } = $props();

	const SPIN = 0.35; // radians per second
	let stage: HTMLDivElement;
	let canvas = $state<HTMLCanvasElement>();
	let near = $state(false);
	let ready = $state(false);
	let viewer: Viewer | null = null;
	let dragging = false;
	let dragged = false;
	let lastX = 0;
	let resumeAt = 0;

	$effect(() => {
		const n = count;
		(viewer as StickerViewer | null)?.setCount?.(n);
	});

	onMount(() => {
		const io = new IntersectionObserver(([e]) => (near = e.isIntersecting), { rootMargin: '200px' });
		io.observe(stage);
		return () => io.disconnect();
	});

	// Build the viewer when the tile comes near the screen; drop it when it leaves.
	$effect(() => {
		if (!near || !canvas) return;
		const el = canvas;
		return untrack(() => build(el));
	});

	function build(el: HTMLCanvasElement) {
		const still = matchMedia('(prefers-reduced-motion: reduce)').matches;
		let disposed = false;
		let raf = 0;
		let last = performance.now();
		const ro = new ResizeObserver(() => viewer?.resize(stage.clientWidth, stage.clientHeight));
		const load =
			'kind' in opts
				? import('./sticker3d').then((m) => {
						const v = m.createStickerViewer(el, opts as StickerOptions, () => (ready = true));
						v.setCount(count);
						return v as Viewer;
					})
				: import('./paper3d').then((m) => m.createViewer(el, opts as ViewerOptions, () => (ready = true)));
		load
			.then((v) => {
				if (disposed) return v.dispose();
				viewer = v;
				v.angle = ('kind' in opts ? -0.25 : -0.45) + phase;
				v.resize(stage.clientWidth, stage.clientHeight);
				ro.observe(stage);
				const tick = (now: number) => {
					raf = requestAnimationFrame(tick);
					const dt = Math.min(0.05, (now - last) / 1000);
					last = now;
					if (document.hidden) return;
					if (!still && !dragging && now > resumeAt) v.angle += SPIN * dt;
					v.render();
				};
				raf = requestAnimationFrame(tick);
			})
			.catch(() => {}); // the photo stays
		return () => {
			disposed = true;
			cancelAnimationFrame(raf);
			ro.disconnect();
			viewer?.dispose();
			viewer = null;
			ready = false;
		};
	}

	function down(e: PointerEvent) {
		if (e.pointerType === 'mouse' && e.button !== 0) return;
		dragging = true;
		dragged = false;
		lastX = e.clientX;
	}
	function move(e: PointerEvent) {
		if (!dragging || !viewer) return;
		const dx = e.clientX - lastX;
		if (!dragged && Math.abs(dx) < 4) return;
		if (!dragged) (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
		dragged = true;
		viewer.angle += dx * 0.012;
		lastX = e.clientX;
	}
	function up() {
		dragging = false;
		resumeAt = performance.now() + 1500;
	}
	// A drag turns the item; it shouldn't also open the product page.
	function click(e: MouseEvent) {
		if (!dragged) return;
		e.preventDefault();
		e.stopPropagation();
		dragged = false;
	}
</script>

<!-- svelte-ignore a11y_no_static_element_interactions, a11y_click_events_have_key_events -->
<div class="spin" class:ready bind:this={stage} onpointerdown={down} onpointermove={move} onpointerup={up}
	onpointercancel={up} onclickcapture={click} aria-hidden="true">
	{#if near}<canvas bind:this={canvas}></canvas>{/if}
</div>

<style>
	.spin { position: absolute; inset: 0; touch-action: pan-y; cursor: grab; }
	canvas { width: 100%; height: 100%; opacity: 0; transition: opacity 0.4s; display: block; }
	.ready canvas { opacity: 1; }
</style>
