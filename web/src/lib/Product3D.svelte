<script lang="ts">
	// Interactive 3D view of a printed item (see paper3d.ts), a sticker request
	// (sticker3d.ts), a T-shirt (shirt3d.ts) or an enamel pin (pin3d.ts): slowly spins like a store's 3D product view; drag or arrow
	// keys turn it, + / − zoom in to see the paper. Postcards lie in stacks, and the
	// last one turns over to show the back. Booklets (book3d.ts) also
	// open: drag, arrow keys or the slider turn their pages. A trifold brochure
	// (fold3d.ts) opens and folds itself back up on a loop until you touch it;
	// then tap, drag or the arrows fold it. Falls back to the flat front image
	// without WebGL.
	import { onMount } from 'svelte';
	import { settle, spreadLabel } from './book';
	import { FOLD_STEPS, foldCycle, foldLabel } from './fold';
	import type { BookViewer } from './book3d';
	import { loadPreviewViewer, type Preview3D } from './items';
	import { finishInfo } from './paper';
	import { sizeLabel } from './scale';
	import { packLabel } from './stickers';
	import type { Viewer } from './paper3d';
	import type { StickerViewer } from './sticker3d';
	import type { PostcardViewer } from './postcard3d';
	import { postcardLabel } from './postcards';
	import { shirtSize } from './shirt';
	import { pinFacts } from './pin';

	// count: how many stickers or postcards are requested; the view grows to match.
	let { preview, name, count = 0 }: { preview: Preview3D; name: string; count?: number } = $props();

	let canvas = $state<HTMLCanvasElement>();
	let stage: HTMLDivElement;
	let viewer: Viewer | null = null;
	let ready = $state(false);
	let failed = $state(false);
	let spinning = $state(true);
	let dragging = $state(false);
	let showingBack = $state(false);
	let zoomLevel = $state(0); // index into ZOOMS
	// Booklets: the spread we're heading to (0 = closed on the cover), and the
	// one we're nearest, for the label.
	let goal = $state(0);
	let near = $state(0);
	let turning = false; // dragging pages rather than spinning the book
	let dragFrom = { x: 0, position: 0 };
	const book = $derived('book' in preview ? preview.book : undefined);
	const fold = $derived('fold' in preview ? preview.fold : undefined);
	const opens = $derived(!!book || !!fold); // turns through positions (BookViewer.position)
	const front = $derived(
		'book' in preview
			? (preview.book.pages[0] ?? '')
			: 'fold' in preview
				? preview.fold.outside
				: 'sheet' in preview
					? preview.sheet.front
					: 'postcards' in preview
						? preview.postcards.front
						: 'shirt' in preview
							? preview.shirt.front
							: 'pin' in preview
								? preview.pin.front
								: preview.stickers.front
	);
	// Shirts always have a back to show, if only a plain one; pins, their posts and clutches.
	const back = $derived(
		'sheet' in preview ? !!preview.sheet.back : 'postcards' in preview ? !!preview.postcards.back : 'shirt' in preview || 'pin' in preview
	);
	const postcards = $derived('postcards' in preview);
	const leaves = $derived(book ? Math.ceil(book.pages.length / 2) : fold ? FOLD_STEPS : 0);
	const stepLabel = (k: number) => (book ? spreadLabel(k, book.pages.length) : foldLabel(k));
	const isOpen = $derived(goal > 0);

	const ZOOMS = [1, 0.5, 0.22];
	const SPIN = 0.45; // radians per second
	let target: number | null = null; // angle we're animating to (flip)
	let resumeAt = 0;
	let lastX = 0;
	let cycleFrom = 0; // when a trifold's open-and-fold loop started
	let cardGoal = $state(0); // postcards: 1 = the last stack turned over to its back

	const finish = $derived('sheet' in preview ? finishInfo(preview.sheet.finish).label : 'postcards' in preview ? finishInfo(preview.postcards.finish).label : '');
	const label = $derived(
		'sheet' in preview
			? `${sizeLabel(preview.sheet.widthIn, preview.sheet.heightIn)}${finish ? ` · ${finish}` : ''}`
			: 'postcards' in preview
				? `${postcardLabel(count)} · ${sizeLabel(preview.postcards.widthIn, preview.postcards.heightIn)}`
				: 'stickers' in preview
					? packLabel(count)
					: 'shirt' in preview
						? `Size ${preview.shirt.size}, about ${sizeLabel(shirtSize(preview.shirt.size).chest, shirtSize(preview.shirt.size).length)}`
						: 'pin' in preview
							? pinFacts(preview.pin.widthIn, preview.pin.heightIn)
							: ''
	);
	$effect(() => {
		const n = count;
		(viewer as StickerViewer | null)?.setCount?.(n);
	});

	onMount(() => {
		const reduceMotion = matchMedia('(prefers-reduced-motion: reduce)').matches;
		if (reduceMotion) spinning = false;
		cycleFrom = performance.now();
		let raf = 0;
		let visible = true;
		let disposed = false;
		let last = performance.now();

		const io = new IntersectionObserver(([e]) => (visible = e.isIntersecting));
		io.observe(stage);
		const ro = new ResizeObserver(() => {
			if (!viewer) return;
			viewer.resize(stage.clientWidth, stage.clientHeight);
			viewer.render();
		});
		ro.observe(stage);

		loadPreviewViewer(canvas!, preview, count, () => (ready = true))
			.then((v) => {
				if (disposed) return v.dispose();
				viewer = v;
				viewer.angle = 'stickers' in preview || 'postcards' in preview ? -0.25 : -0.45;
				viewer.resize(stage.clientWidth, stage.clientHeight);
				const tick = (now: number) => {
					raf = requestAnimationFrame(tick);
					const dt = Math.min(0.05, (now - last) / 1000);
					last = now;
					if (!viewer || !visible || document.hidden) return;
					const bv = opens ? (viewer as BookViewer) : null;
					// A trifold left alone opens and folds itself back up.
					if (fold && spinning && !dragging) goal = foldCycle(now - cycleFrom);
					if (bv && !turning) {
						// Turn toward the goal: books quickly at first, never slower than a leaf a
						// second; a trifold's panels at an even pace, like a hand folding it.
						const d = goal - bv.position;
						if (Math.abs(d) < 0.0005) bv.position = goal;
						else if (fold) bv.position += Math.sign(d) * Math.min(Math.abs(d), dt * 1.1);
						else bv.position += Math.sign(d) * Math.min(Math.abs(d), Math.max(Math.abs(d) * dt * 5, dt * 1.6));
					}
					if (bv && (goal > 0 || bv.position > 0.001)) {
						// Open books face you squarely.
						target = Math.round(viewer.angle / (2 * Math.PI)) * 2 * Math.PI;
					} else if (fold && spinning) {
						// Folded, it shows its cover at an angle.
						target = Math.round(viewer.angle / (2 * Math.PI)) * 2 * Math.PI - 0.45;
					}
					if (bv) near = Math.round(bv.position);
					if (target !== null) {
						viewer.angle += (target - viewer.angle) * Math.min(1, dt * 7);
						if (Math.abs(target - viewer.angle) < 0.002) {
							viewer.angle = target;
							target = null;
						}
					} else if (spinning && !fold && !dragging && now > resumeAt) {
						viewer.angle += SPIN * dt;
					}
					const wantZoom = ZOOMS[zoomLevel];
					viewer.zoom += (wantZoom - viewer.zoom) * Math.min(1, dt * 6);
					if (Math.abs(wantZoom - viewer.zoom) < 0.0005) viewer.zoom = wantZoom;
					if (postcards) {
						// The stack turns over at an even pace, about half a second.
						const pv = viewer as PostcardViewer;
						const d = cardGoal - pv.flipped;
						pv.flipped = Math.abs(d) < 0.001 ? cardGoal : pv.flipped + Math.sign(d) * Math.min(Math.abs(d), dt * 2);
						showingBack = cardGoal === 1;
					} else showingBack = Math.cos(viewer.angle) < 0;
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

	const pause = () => {
		resumeAt = performance.now() + 2500;
		if (fold) spinning = false; // once you take hold of it, it stays how you leave it
	};

	function down(e: PointerEvent) {
		dragging = true;
		target = null;
		lastX = e.clientX;
		// An open book's pages follow your finger; a closed one turns around.
		turning = opens && isOpen;
		if (fold) spinning = false;
		dragFrom = { x: e.clientX, position: (viewer as BookViewer | null)?.position ?? 0 };
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
	}
	function move(e: PointerEvent) {
		if (!dragging || !viewer) return;
		if (turning) {
			// Dragging across most of a page turns one leaf; dragging a trifold right
			// pulls it open (the flap opens that way), left folds it up.
			const perLeaf = stage.clientWidth * 0.4;
			const bv = viewer as BookViewer;
			const moved = (fold ? e.clientX - dragFrom.x : dragFrom.x - e.clientX) / perLeaf;
			bv.position = Math.min(leaves, Math.max(0, dragFrom.position + moved));
		} else {
			viewer.angle += (e.clientX - lastX) * 0.012;
		}
		lastX = e.clientX;
	}
	function up(e: PointerEvent) {
		dragging = false;
		if (turning && viewer) {
			const bv = viewer as BookViewer;
			goal = settle(bv.position, Math.sign(bv.position - dragFrom.position));
		} else if (opens && !isOpen && Math.abs(e.clientX - dragFrom.x) < 4) {
			goal = fold ? FOLD_STEPS : 1; // a tap on the closed book opens it; a trifold opens all the way
		}
		turning = false;
		pause();
	}
	const turnTo = (k: number) => {
		goal = Math.min(leaves, Math.max(0, k));
		pause();
	};
	function key(e: KeyboardEvent) {
		if (!viewer) return;
		if (opens && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) {
			turnTo(goal + (e.key === 'ArrowLeft' ? -1 : 1));
		} else if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
			target = (target ?? viewer.angle) + (e.key === 'ArrowLeft' ? -0.35 : 0.35);
		} else if (e.key === '+' || e.key === '=') zoomLevel = Math.min(ZOOMS.length - 1, zoomLevel + 1);
		else if (e.key === '-') zoomLevel = Math.max(0, zoomLevel - 1);
		else return;
		e.preventDefault();
		pause();
	}
	function flip() {
		if (!viewer) return;
		if (postcards) {
			cardGoal = 1 - cardGoal;
			pause();
			return;
		}
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
		aria-label={book
			? `${name} in 3D: ${sizeLabel(book.widthIn, book.heightIn)}, ${book.pages.length} pages. Left and right arrow keys turn the pages, plus and minus zoom.`
			: fold
				? `${name} in 3D: a trifold brochure, ${sizeLabel(fold.widthIn, fold.heightIn)} open. Left and right arrow keys fold and unfold it, plus and minus zoom.`
				: `${name} in 3D: ${label.toLowerCase()}, next to a phone for scale. Drag or use the arrow keys to turn it, plus and minus to zoom.`}
		aria-valuenow={opens ? near : 0}
		aria-valuemin={0}
		aria-valuemax={leaves}
		aria-valuetext={opens ? stepLabel(near) : showingBack ? 'showing the back' : 'showing the front'}
		onpointerdown={down}
		onpointermove={move}
		onpointerup={up}
		onpointercancel={up}
		onkeydown={key}
	>
		{#if failed}
			<img class="fallback" src={front} alt={name} />
		{:else}
			<canvas bind:this={canvas} class:ready></canvas>
			{#if !ready}<img class="fallback loading" src={front} alt="" />{/if}
		{/if}
		{#if !failed}
			<div class="controls">
				<button type="button" class="ctl" aria-label="Zoom in" disabled={zoomLevel === ZOOMS.length - 1}
					onpointerdown={(e) => e.stopPropagation()} onclick={() => (zoomLevel++, pause())}>+</button>
				<button type="button" class="ctl" aria-label="Zoom out" disabled={zoomLevel === 0}
					onpointerdown={(e) => e.stopPropagation()} onclick={() => (zoomLevel--, pause())}>−</button>
				{#if fold}
					<button type="button" class="ctl" aria-label={spinning ? 'Stop folding' : 'Fold and unfold on its own'}
						onpointerdown={(e) => e.stopPropagation()} onclick={() => ((spinning = !spinning), (cycleFrom = performance.now() - (goal === 2 ? 3600 : 0)))}>{spinning ? '❚❚' : '⟳'}</button>
				{:else if !isOpen}
					<button type="button" class="ctl" aria-label={spinning ? 'Stop spinning' : 'Spin'}
						onpointerdown={(e) => e.stopPropagation()} onclick={() => (spinning = !spinning)}>{spinning ? '❚❚' : '⟳'}</button>
				{/if}
			</div>
			{#if opens && ready}
				<div class="pager" role="group" aria-label={fold ? 'Fold and unfold it' : 'Turn the pages'}>
					<button type="button" class="ctl" aria-label={fold ? 'Fold it' : 'Previous page'} disabled={goal === 0}
						onpointerdown={(e) => e.stopPropagation()} onclick={() => turnTo(goal - 1)}>‹</button>
					<span class="page-label">{stepLabel(near)}</span>
					<button type="button" class="ctl" aria-label={fold ? 'Unfold it' : 'Next page'} disabled={goal === leaves}
						onpointerdown={(e) => e.stopPropagation()} onclick={() => turnTo(goal + 1)}>›</button>
				</div>
			{/if}
		{/if}
	</div>
	{#if book && !failed}
		<input class="scrub" type="range" min="0" max={leaves} step="1" value={goal} aria-label="Jump to a page"
			oninput={(e) => turnTo(+e.currentTarget.value)} />
	{/if}
	<div class="bar">
		{#if book}
			<span class="muted">{sizeLabel(book.widthIn, book.heightIn)} · {book.pages.length} pages{book.binding === 'staple' ? ', stapled' : ''} · {isOpen ? 'drag to turn the page' : 'tap to open'}</span>
			{#if !failed}
				<button type="button" class="secondary small" onclick={() => turnTo(isOpen ? 0 : 1)}>{isOpen ? 'Close it' : 'Look inside'}</button>
			{/if}
		{:else if fold}
			<span class="muted">{sizeLabel(fold.widthIn, fold.heightIn)}, folded in thirds · {isOpen ? 'drag to fold it' : 'tap to open'}</span>
			{#if !failed}
				<button type="button" class="secondary small" onclick={() => turnTo(isOpen ? 0 : FOLD_STEPS)}>{isOpen ? 'Fold it up' : 'Open it'}</button>
			{/if}
		{:else}
			<span class="muted">{label} · drag to turn</span>
			{#if back && !failed}
				<button type="button" class="secondary small" onclick={flip}>{showingBack ? 'Show front' : 'Show back'}</button>
			{/if}
		{/if}
	</div>
</div>

<style>
	.wrap { display: grid; gap: 0.5rem; width: 100%; }
	.stage {
		position: relative; aspect-ratio: 1 / 1.1; max-height: min(70vh, 34rem); min-height: 18rem; width: 100%;
		background: radial-gradient(closest-side at 50% 60%, rgba(255, 255, 255, 0.75), transparent); cursor: grab; touch-action: pan-y; user-select: none; overflow: hidden;
	}
	.stage:focus-visible { outline: 2px solid var(--red); outline-offset: 2px; }
	.stage.dragging { cursor: grabbing; }
	canvas { position: absolute; inset: 0; width: 100%; height: 100%; opacity: 0; transition: opacity 0.4s; }
	canvas.ready { opacity: 1; }
	.fallback { position: absolute; inset: 8%; width: 84%; height: 84%; object-fit: contain; }
	.loading { opacity: 0.5; filter: blur(2px); }
	.controls { position: absolute; right: 0.5rem; top: 0.5rem; display: grid; gap: 0.35rem; }
	.ctl {
		width: 2rem; height: 2rem; padding: 0; font-size: 1rem; line-height: 1;
		background: rgba(255, 255, 255, 0.9); color: var(--dark); border: 1px solid var(--smoke);
	}
	.ctl:disabled { opacity: 0.35; }
	.bar { display: flex; justify-content: space-between; align-items: center; gap: 0.5rem; flex-wrap: wrap; padding: 0 0.75rem 0.65rem; }
	.small { padding: 0.3rem 0.8rem; font-size: 0.85rem; }
	.pager {
		position: absolute; left: 50%; bottom: 0.6rem; transform: translateX(-50%); display: flex; align-items: center; gap: 0.5rem;
		background: rgba(255, 255, 255, 0.92); border: 1px solid var(--smoke); padding: 0.2rem;
	}
	.pager .ctl { border: none; font-size: 1.3rem; background: none; }
	.page-label { font-size: 0.85rem; font-weight: 600; min-width: 8.5rem; text-align: center; font-variant-numeric: tabular-nums; }
	.scrub { width: calc(100% - 1.5rem); accent-color: var(--red); margin: 0 auto; }
</style>
