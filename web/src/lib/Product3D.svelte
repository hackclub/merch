<script lang="ts">
	// Interactive 3D view of a printed item (see paper3d.ts) or a sticker request
	// (sticker3d.ts): slowly spins like a store's 3D product view; drag or arrow
	// keys turn it, + / − zoom in to see the paper. Falls back to the flat front
	// image without WebGL.
	import { onMount } from 'svelte';
	import { finishInfo } from './paper';
	import { sizeLabel } from './scale';
	import { packLabel } from './stickers';
	import type { Viewer, ViewerOptions } from './paper3d';
	import type { StickerOptions, StickerViewer } from './sticker3d';

	// count: how many stickers are requested; the sticker view grows to match.
	let { opts, name, count = 0 }: { opts: ViewerOptions | StickerOptions; name: string; count?: number } = $props();

	let canvas = $state<HTMLCanvasElement>();
	let stage: HTMLDivElement;
	let viewer: Viewer | null = null;
	let ready = $state(false);
	let failed = $state(false);
	let spinning = $state(true);
	let dragging = $state(false);
	let showingBack = $state(false);
	let zoomLevel = $state(0); // index into ZOOMS

	const ZOOMS = [1, 0.5, 0.22];
	const SPIN = 0.45; // radians per second
	let target: number | null = null; // angle we're animating to (flip)
	let resumeAt = 0;
	let lastX = 0;

	const finish = $derived('finish' in opts ? finishInfo(opts.finish).label : '');
	const label = $derived(
		'kind' in opts ? packLabel(count) : `${sizeLabel(opts.widthIn, opts.heightIn)}${finish ? ` · ${finish}` : ''}`
	);
	$effect(() => {
		const n = count;
		(viewer as StickerViewer | null)?.setCount?.(n);
	});

	onMount(() => {
		const reduceMotion = matchMedia('(prefers-reduced-motion: reduce)').matches;
		if (reduceMotion) spinning = false;
		let raf = 0;
		let visible = true;
		let disposed = false;
		let last = performance.now();

		const io = new IntersectionObserver(([e]) => (visible = e.isIntersecting));
		io.observe(stage);
		const ro = new ResizeObserver(() => viewer?.resize(stage.clientWidth, stage.clientHeight));
		ro.observe(stage);

		const load =
			'kind' in opts
				? import('./sticker3d').then((m) => {
						const v = m.createStickerViewer(canvas!, opts as StickerOptions, () => (ready = true));
						v.setCount(count);
						return v;
					})
				: import('./paper3d').then((m) => m.createViewer(canvas!, opts as ViewerOptions, () => (ready = true)));
		load
			.then((v) => {
				if (disposed) return v.dispose();
				viewer = v;
				viewer.angle = 'kind' in opts ? -0.25 : -0.45;
				viewer.resize(stage.clientWidth, stage.clientHeight);
				const tick = (now: number) => {
					raf = requestAnimationFrame(tick);
					const dt = Math.min(0.05, (now - last) / 1000);
					last = now;
					if (!viewer || !visible || document.hidden) return;
					if (target !== null) {
						viewer.angle += (target - viewer.angle) * Math.min(1, dt * 7);
						if (Math.abs(target - viewer.angle) < 0.002) target = null;
					} else if (spinning && !dragging && now > resumeAt) {
						viewer.angle += SPIN * dt;
					}
					viewer.zoom += (ZOOMS[zoomLevel] - viewer.zoom) * Math.min(1, dt * 6);
					showingBack = Math.cos(viewer.angle) < 0;
					viewer.render();
				};
				raf = requestAnimationFrame(tick);
			})
			.catch(() => (failed = true));

		return () => {
			disposed = true;
			cancelAnimationFrame(raf);
			io.disconnect();
			ro.disconnect();
			viewer?.dispose();
		};
	});

	const pause = () => (resumeAt = performance.now() + 2500);

	function down(e: PointerEvent) {
		dragging = true;
		target = null;
		lastX = e.clientX;
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
	}
	function move(e: PointerEvent) {
		if (!dragging || !viewer) return;
		viewer.angle += (e.clientX - lastX) * 0.012;
		lastX = e.clientX;
	}
	function up() {
		dragging = false;
		pause();
	}
	function key(e: KeyboardEvent) {
		if (!viewer) return;
		if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
			target = (target ?? viewer.angle) + (e.key === 'ArrowLeft' ? -0.35 : 0.35);
		} else if (e.key === '+' || e.key === '=') zoomLevel = Math.min(ZOOMS.length - 1, zoomLevel + 1);
		else if (e.key === '-') zoomLevel = Math.max(0, zoomLevel - 1);
		else return;
		e.preventDefault();
		pause();
	}
	function flip() {
		if (!viewer) return;
		// Turn to face the other side squarely, the short way round.
		const turns = Math.round(viewer.angle / Math.PI) + 1;
		target = turns * Math.PI;
		pause();
		resumeAt = performance.now() + 6000;
	}
</script>

<div class="wrap">
	<div
		class="stage"
		class:dragging
		bind:this={stage}
		role="slider"
		tabindex="0"
		aria-label="{name} in 3D: {label.toLowerCase()}, next to a phone for scale. Drag or use the arrow keys to turn it, plus and minus to zoom."
		aria-valuenow={0}
		aria-valuetext={showingBack ? 'showing the back' : 'showing the front'}
		onpointerdown={down}
		onpointermove={move}
		onpointerup={up}
		onpointercancel={up}
		onkeydown={key}
	>
		{#if failed}
			<img class="fallback" src={opts.front} alt={name} />
		{:else}
			<canvas bind:this={canvas} class:ready></canvas>
			{#if !ready}<img class="fallback loading" src={opts.front} alt="" />{/if}
		{/if}
		{#if !failed}
			<div class="controls">
				<button type="button" class="ctl" aria-label="Zoom in" disabled={zoomLevel === ZOOMS.length - 1}
					onpointerdown={(e) => e.stopPropagation()} onclick={() => (zoomLevel++, pause())}>+</button>
				<button type="button" class="ctl" aria-label="Zoom out" disabled={zoomLevel === 0}
					onpointerdown={(e) => e.stopPropagation()} onclick={() => (zoomLevel--, pause())}>−</button>
				<button type="button" class="ctl" aria-label={spinning ? 'Stop spinning' : 'Spin'}
					onpointerdown={(e) => e.stopPropagation()} onclick={() => (spinning = !spinning)}>{spinning ? '❚❚' : '⟳'}</button>
			</div>
		{/if}
	</div>
	<div class="bar">
		<span class="muted">{label} · drag to turn</span>
		{#if 'back' in opts && opts.back && !failed}
			<button type="button" class="secondary small" onclick={flip}>{showingBack ? 'Show front' : 'Show back'}</button>
		{/if}
	</div>
</div>

<style>
	.wrap { display: grid; gap: 0.5rem; }
	.stage {
		position: relative; aspect-ratio: 1 / 1.1; max-height: min(70vh, 34rem); min-height: 18rem; width: 100%;
		background: radial-gradient(ellipse at 50% 85%, #eef1f5 0%, #ffffff 65%);
		border-radius: 8px; cursor: grab; touch-action: pan-y; user-select: none; overflow: hidden;
	}
	.stage:focus-visible { outline: 2px solid var(--red); outline-offset: 2px; }
	.stage.dragging { cursor: grabbing; }
	canvas { position: absolute; inset: 0; width: 100%; height: 100%; opacity: 0; transition: opacity 0.4s; }
	canvas.ready { opacity: 1; }
	.fallback { position: absolute; inset: 8%; width: 84%; height: 84%; object-fit: contain; }
	.loading { opacity: 0.5; filter: blur(2px); }
	.controls { position: absolute; right: 0.5rem; top: 0.5rem; display: grid; gap: 0.35rem; }
	.ctl {
		width: 2rem; height: 2rem; padding: 0; border-radius: 999px; font-size: 1rem; line-height: 1;
		background: rgba(255, 255, 255, 0.9); color: var(--dark); border: 1px solid var(--smoke);
	}
	.ctl:disabled { opacity: 0.35; }
	.bar { display: flex; justify-content: space-between; align-items: center; gap: 0.5rem; flex-wrap: wrap; }
	.small { padding: 0.3rem 0.8rem; font-size: 0.85rem; }
</style>
