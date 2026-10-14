<script lang="ts">
	// A double-sided print standing next to a phone, both true to scale, in
	// perspective. Drag (or use the arrow keys) to turn the print; Flip shows the back.
	import { GAP_IN, PHONE, pixelsPerInch, sizeLabel } from './scale';

	let { front, back = '', widthIn, heightIn, name }: { front: string; back?: string; widthIn: number; heightIn: number; name: string } =
		$props();

	let stageW = $state(0);
	let stageH = $state(0);
	const ppi = $derived(pixelsPerInch({ w: widthIn, h: heightIn }, { w: stageW, h: stageH }));

	let angle = $state(-24); // degrees around the print's vertical axis
	let dragging = $state(false);
	let lastX = 0;

	function down(e: PointerEvent) {
		dragging = true;
		lastX = e.clientX;
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
	}
	function move(e: PointerEvent) {
		if (!dragging) return;
		angle += (e.clientX - lastX) * 0.6;
		lastX = e.clientX;
	}
	function key(e: KeyboardEvent) {
		if (e.key === 'ArrowLeft') angle -= 15;
		else if (e.key === 'ArrowRight') angle += 15;
		else return;
		e.preventDefault();
	}
	// Turn to the other face the short way round.
	const flip = () => (angle += Math.cos((angle * Math.PI) / 180) >= 0 ? 180 : -180);
	const showingBack = $derived(Math.cos((angle * Math.PI) / 180) < 0);
</script>

<div class="wrap">
	<div
		class="stage"
		class:dragging
		bind:clientWidth={stageW}
		bind:clientHeight={stageH}
		role="slider"
		tabindex="0"
		aria-label="{name}, {sizeLabel(widthIn, heightIn)}, next to a phone for scale. Drag or use arrow keys to turn it."
		aria-valuenow={Math.round(angle)}
		aria-valuetext={showingBack ? 'showing the back' : 'showing the front'}
		onpointerdown={down}
		onpointermove={move}
		onpointerup={() => (dragging = false)}
		onpointercancel={() => (dragging = false)}
		onkeydown={key}
	>
		{#if stageW > 0}
			<div class="scene" style:gap="{GAP_IN * ppi}px">
				<div class="object">
					<div class="sheet" style:width="{widthIn * ppi}px" style:height="{heightIn * ppi}px" style:transform="rotateY({angle}deg)">
						<div class="face front" style:background-image="url({front})"></div>
						<div class="face back" style:background-image={back ? `url(${back})` : 'none'}></div>
					</div>
					<div class="shadow" style:width="{widthIn * ppi * 1.1}px"></div>
					<div class="label">{widthIn} × {heightIn} in</div>
				</div>
				<div class="object">
					<div class="phone" style:width="{PHONE.w * ppi}px" style:height="{PHONE.h * ppi}px" style:border-radius="{0.42 * ppi}px"
						style:padding="{0.1 * ppi}px">
						<div class="screen" style:border-radius="{0.33 * ppi}px">
							<div class="island" style:width="{0.5 * ppi}px" style:height="{0.14 * ppi}px" style:top="{0.1 * ppi}px"></div>
						</div>
					</div>
					<div class="shadow" style:width="{PHONE.w * ppi * 1.2}px"></div>
					<div class="label">phone</div>
				</div>
			</div>
		{/if}
	</div>
	<div class="bar">
		<span class="muted">{sizeLabel(widthIn, heightIn)} · drag to turn it</span>
		{#if back}<button type="button" class="secondary small" onclick={flip}>{showingBack ? 'Show front' : 'Show back'}</button>{/if}
	</div>
</div>

<style>
	.wrap { display: grid; gap: 0.5rem; }
	.stage {
		position: relative; aspect-ratio: 1 / 1.1; max-height: min(70vh, 34rem); min-height: 18rem; overflow: hidden;
		perspective: 1600px; perspective-origin: 50% 35%;
		background: radial-gradient(ellipse at 50% 100%, #e9edf3 0%, #f9fafc 60%);
		border-radius: 8px; cursor: grab; touch-action: pan-y; user-select: none;
	}
	.stage:focus-visible { outline: 2px solid var(--red); outline-offset: 2px; }
	.stage.dragging { cursor: grabbing; }
	.scene {
		position: absolute; inset: 0; display: flex; align-items: flex-end; justify-content: center;
		padding-bottom: 9%; transform-style: preserve-3d; transform: rotateX(4deg) rotateY(-10deg);
	}
	.object { position: relative; display: flex; flex-direction: column; align-items: center; transform-style: preserve-3d; }
	.sheet { position: relative; transform-style: preserve-3d; transition: transform 0.6s cubic-bezier(0.2, 0.7, 0.2, 1); }
	.dragging .sheet { transition: none; }
	.face {
		position: absolute; inset: 0; background: white center / cover no-repeat; backface-visibility: hidden;
		box-shadow: 0 1px 2px rgba(0, 0, 0, 0.15), 0 8px 24px rgba(0, 0, 0, 0.12);
	}
	.back { transform: rotateY(180deg); }
	.phone { box-sizing: border-box; background: #1d1d22; box-shadow: 0 8px 24px rgba(0, 0, 0, 0.25); }
	.screen { position: relative; width: 100%; height: 100%; background: linear-gradient(160deg, #3a3f52, #15161c 70%); }
	.island { position: absolute; left: 50%; transform: translateX(-50%); border-radius: 999px; background: #050507; }
	.shadow {
		position: absolute; bottom: 1.7rem; height: 14px; border-radius: 50%; transform: translateY(50%);
		background: radial-gradient(ellipse, rgba(0, 0, 0, 0.18), transparent 70%); z-index: -1;
	}
	.label { margin-top: 1rem; font-size: 0.8rem; font-weight: 700; color: var(--muted); white-space: nowrap; }
	.bar { display: flex; justify-content: space-between; align-items: center; gap: 0.5rem; flex-wrap: wrap; }
	.small { padding: 0.3rem 0.8rem; font-size: 0.85rem; }
	@media (prefers-reduced-motion: reduce) {
		.sheet { transition: none; }
	}
</style>
