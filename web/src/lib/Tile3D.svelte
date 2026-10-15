<script lang="ts">
	// A shop tile's preview (see items.ts tilePreview). It sits still until you
	// touch it: hover turns a print or stickers with the pointer and drag turns
	// them further; a booklet opens on hover and flips through its pages, and
	// dragging across turns them by hand. A clip plays while hovered (or while
	// on screen, on touch screens). A click still follows the tile's link.
	// Viewers exist only while the tile is near the screen, so a long grid never
	// runs out of WebGL contexts; the photo underneath shows until it's ready.
	import { onMount, untrack } from 'svelte';
	import { settle } from './book';
	import type { BookViewer } from './book3d';
	import { loadPreviewViewer, type Preview3D, type TilePreview } from './items';
	import type { Viewer } from './paper3d';
	import type { StickerViewer } from './sticker3d';
	import { flipStep, hoverTurn, isGif } from './tile';

	// count: stickers in the bag; rest: the angle it sits at; active: animate as if hovered
	// (the clip plays, a booklet flips, a print swings), for previews that never get the pointer.
	let { preview, count = 0, rest = -0.45, active = false }: { preview: TilePreview; count?: number; rest?: number; active?: boolean } = $props();

	const FLIP_EVERY = 1100; // ms per leaf while hovering a booklet
	let stage: HTMLDivElement;
	let canvas = $state<HTMLCanvasElement>();
	let video = $state<HTMLVideoElement>();
	let near = $state(false);
	let ready = $state(false);
	let viewer: Viewer | null = null;

	let at = untrack(() => rest); // where it settles when left alone
	let target = untrack(() => rest);
	let hovering = false;
	let fromX = 0; // pointer x where the hover turn started
	let dragging = false;
	let dragged = false;
	let lastX = 0;
	let dragFrom = { x: 0, position: 0 };
	let goal = 0; // booklets: the leaf we're heading to
	let flipAt = 0; // when the next hover flip happens
	let busyUntil = 0; // sticker bags are dropping in until then

	const book = $derived('book' in preview ? preview.book : null);
	const leaves = $derived(book ? Math.ceil(book.pages.length / 2) : 0);

	$effect(() => {
		const n = count;
		(viewer as StickerViewer | null)?.setCount?.(n);
		busyUntil = performance.now() + 1500;
	});

	onMount(() => {
		const io = new IntersectionObserver(([e]) => (near = e.isIntersecting), { rootMargin: '200px' });
		io.observe(stage);
		return () => io.disconnect();
	});

	// Clips play while hovered; touch screens have no hover, so there they play while on screen.
	$effect(() => {
		if (!video) return;
		if (active || (near && matchMedia('(hover: none)').matches)) video.play().catch(() => {});
		else if (!near) video.pause();
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
		let drawn = false;
		const ro = new ResizeObserver(() => {
			viewer?.resize(stage.clientWidth, stage.clientHeight);
			drawn = false;
		});
		loadPreviewViewer(el, preview as Preview3D, count, () => ((ready = true), (drawn = false)))
			.then((v) => {
				if (disposed) return v.dispose();
				viewer = v;
				v.angle = target;
				v.resize(stage.clientWidth, stage.clientHeight);
				ro.observe(stage);
				const bv = book ? (v as BookViewer) : null;
				const tick = (now: number) => {
					raf = requestAnimationFrame(tick);
					const dt = Math.min(0.05, (now - last) / 1000);
					last = now;
					if (document.hidden) return;
					let moving = !drawn || now < busyUntil;
					if (bv) {
						if ((hovering || active) && !dragging && !still && now > flipAt && goal < leaves) {
							goal = flipStep(goal, leaves);
							flipAt = now + FLIP_EVERY;
						}
						if (!dragging) {
							// Turn toward the goal: quickly at first, never slower than a leaf a second.
							const d = goal - bv.position;
							if (Math.abs(d) < 0.0005) bv.position = goal;
							else (bv.position += Math.sign(d) * Math.min(Math.abs(d), Math.max(Math.abs(d) * dt * 5, dt * 1.6))), (moving = true);
						}
						// Open books face you squarely; closed, they sit at rest.
						target = goal > 0 || bv.position > 0.001 ? 0 : at;
					}
					if (active && !bv && !hovering && !dragging && !still) target = at + Math.sin(now / 900) * 0.5;
					if (!dragging && Math.abs(target - v.angle) > 0.0005) {
						v.angle += (target - v.angle) * Math.min(1, dt * 6);
						moving = true;
					}
					if (moving || dragging) {
						v.render();
						drawn = true;
					}
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

	function enter(e: PointerEvent) {
		if (e.pointerType !== 'mouse') return;
		hovering = true;
		fromX = e.clientX;
		flipAt = performance.now() + 250;
		video?.play().catch(() => {});
	}
	function leave(e: PointerEvent) {
		if (e.pointerType !== 'mouse') return;
		hovering = false;
		target = at;
		goal = 0;
		if (video) {
			video.pause();
			video.currentTime = 0; // back to the cover
		}
	}
	function down(e: PointerEvent) {
		if (e.pointerType === 'mouse' && e.button !== 0) return;
		dragging = true;
		dragged = false;
		lastX = e.clientX;
		dragFrom = { x: e.clientX, position: (viewer as BookViewer | null)?.position ?? 0 };
	}
	function move(e: PointerEvent) {
		if (!dragging) {
			if (hovering && !book) target = hoverTurn(at, fromX, e.clientX, stage.clientWidth);
			return;
		}
		if (!viewer) return;
		if (!dragged && Math.abs(e.clientX - dragFrom.x) < 4) return;
		if (!dragged) (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
		dragged = true;
		if (book) {
			// Dragging across most of the tile turns one leaf.
			const bv = viewer as BookViewer;
			bv.position = Math.min(leaves, Math.max(0, dragFrom.position + (dragFrom.x - e.clientX) / (stage.clientWidth * 0.6)));
		} else {
			viewer.angle += (e.clientX - lastX) * 0.012;
		}
		lastX = e.clientX;
	}
	function up(e: PointerEvent) {
		if (!dragging) return;
		dragging = false;
		if (!dragged || !viewer) return;
		if (book) {
			const bv = viewer as BookViewer;
			goal = settle(bv.position, Math.sign(bv.position - dragFrom.position));
			flipAt = performance.now() + 3000; // let them look before flipping on
		} else {
			// It stays where you left it; hovering turns it from there.
			at = target = viewer.angle;
			fromX = e.clientX;
		}
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
<div class="spin" class:ready class:clip={'clip' in preview} bind:this={stage} aria-hidden="true"
	onpointerenter={enter} onpointerleave={leave} onpointerdown={down} onpointermove={move} onpointerup={up}
	onpointercancel={up} onclickcapture={click} ondragstart={(e) => e.preventDefault()}>
	{#if 'clip' in preview}
		{#if isGif(preview.clip)}
			{#if near}<img src={preview.clip} alt="" onload={() => (ready = true)} />{/if}
		{:else}
			<video bind:this={video} src={preview.clip} muted loop playsinline preload="metadata"
				onloadeddata={() => (ready = true)}></video>
		{/if}
	{:else if near}
		<canvas bind:this={canvas}></canvas>
	{/if}
</div>

<style>
	.spin { position: absolute; inset: 0; touch-action: pan-y; cursor: grab; }
	.spin.clip { cursor: pointer; }
	canvas, video, img { width: 100%; height: 100%; opacity: 0; transition: opacity 0.4s; display: block; }
	video, img { object-fit: cover; }
	.ready canvas, .ready video, .ready img { opacity: 1; }
</style>
