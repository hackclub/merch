<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError, formatCents } from '$lib/api';
	import { bag, setCart, setSizes } from '$lib/bag.svelte';
	import { pruneCart, pruneSizes, setQuantity, sizeLabel, totalQuantity } from '$lib/cart';
	import { itemIdFromParam, sizeStockLabel, unitCostLabel } from '$lib/items';
	import { sheetThickness, finishInfo } from '$lib/paper';
	import Product3D from '$lib/Product3D.svelte';
	import { hasSize } from '$lib/scale';
	import { session } from '$lib/session.svelte';
	import SizePicker from '$lib/SizePicker.svelte';
	import Stepper from '$lib/Stepper.svelte';
	import type { Catalog, ItemStock } from '$lib/types';

	let catalog = $state<Catalog | null>(null);
	let error = $state('');
	// Gallery selection: '3d' or an image index; null = the default (3D when available).
	let picked = $state<'3d' | number | null>(null);
	let stock = $state<ItemStock | null>(null);

	onMount(async () => {
		try {
			catalog = await api.get<Catalog>('/api/catalog');
			setCart(pruneCart(bag.cart, catalog.items));
			setSizes(pruneSizes(bag.sizes, catalog.items));
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not load this item.';
		}
	});

	const id = $derived(itemIdFromParam(page.params.slug ?? ''));
	const item = $derived(catalog?.items.find((i) => i.id === id));
	const inCart = $derived(item ? (bag.cart[item.id] ?? 0) : 0);
	const total = $derived(totalQuantity(bag.cart));
	// Printed items with a size get a true-to-scale 3D view, shown first.
	const has3D = $derived(!!item && hasSize(item) && item.images.length > 0);
	const shown = $derived(picked ?? (has3D ? '3d' : 0));
	const tiles = $derived(item ? item.images.length + (has3D ? 1 : 0) : 0);
	const pickedSize = $derived(item ? bag.sizes[item.id] : undefined);
	const needsSize = $derived(!!item?.variants.length && !sizeLabel(item, pickedSize));
	const finish = $derived(item ? finishInfo(item.finish).label : '');
	// Reset the gallery when moving between items.
	$effect(() => {
		id;
		picked = null;
	});
	// Live warehouse numbers. If the lookup fails, the line is just left out.
	$effect(() => {
		const itemId = item?.id;
		stock = null;
		if (!itemId) return;
		let stale = false;
		api.get<ItemStock>(`/api/items/${itemId}/stock`).then(
			(s) => { if (!stale) stock = s; },
			() => {}
		);
		return () => { stale = true; };
	});
</script>

<svelte:head><title>{item ? `${item.name} · ` : ''}Hack Club Mail Room</title></svelte:head>

<p><a href="/">← All swag</a></p>

{#if error}<p class="error">{error}</p>{/if}

{#if catalog && !item}
	<h1>Not found</h1>
	<p class="muted">That item isn't listed right now. <a href="/">See what is.</a></p>
{:else if item}
	<div class="product">
		<div class="gallery">
			{#if item.images.length}
				{#if shown === '3d'}
					<div class="main three">
						{#key item.id}
							<Product3D name={item.name} opts={{
								front: item.images[0], back: item.images[1], widthIn: item.width_in!, heightIn: item.height_in!,
								thicknessIn: sheetThickness(item), finish: item.finish
							}} />
						{/key}
					</div>
				{:else}
					<a class="main" href={item.images[shown]} target="_blank" rel="noreferrer" title="Open full size">
						<img src={item.images[shown]} alt="{item.name}, image {shown + 1} of {item.images.length}" />
					</a>
				{/if}
				{#if tiles > 1}
					<div class="thumbs">
						{#if has3D}
							<button type="button" class="thumb three-thumb" class:active={shown === '3d'}
								aria-label="Show it in 3D, to scale" onclick={() => (picked = '3d')}>
								<img src={item.images[0]} alt="" loading="lazy" />
								<span>3D</span>
							</button>
						{/if}
						{#each item.images as src, i (src)}
							<button type="button" class="thumb" class:active={i === shown} aria-label="Show image {i + 1}" onclick={() => (picked = i)}>
								<img {src} alt="" loading="lazy" />
							</button>
						{/each}
					</div>
				{/if}
			{:else}
				<div class="main placeholder">📦</div>
			{/if}
		</div>

		<div class="info">
			<h1>{item.name}</h1>
			{#if item.description}<p class="description">{item.description}</p>{/if}

			<ul class="facts muted">
				<li>Up to {item.max_per_request} per request{item.max_per_user != null ? `, ${item.max_per_user} per person` : ''}</li>
				<li>
					Free shipping in the US{#if catalog?.settings.international_shipping_fee_cents}; {formatCents(catalog.settings.international_shipping_fee_cents)} flat
						shipping elsewhere{/if}
				</li>
				{#if finish}
					<li>Printed on {finish.toLowerCase()}, about {(sheetThickness(item) * 25.4).toFixed(1)} mm thick</li>
				{/if}
				<li>Ships from the Hack Club warehouse</li>
				{#if stock}
					<li class="stock">
						<strong>{stock.in_stock.toLocaleString('en-US')}</strong> in stock{sizeStockLabel(stock) ? ` (${sizeStockLabel(stock)})` : ''}{#if unitCostLabel(stock)}{' · '}<span class="nowrap"
								>{unitCostLabel(stock)}</span
							>{/if}
					</li>
				{/if}
			</ul>

			{#if !catalog?.settings.requests_open}
				<p class="error">Requests are closed right now. Check back soon!</p>
			{:else if !session.me?.user}
				<p><a class="button" href="/auth/login" data-sveltekit-reload>Sign in with Hack Club to request this</a></p>
			{:else}
				{#if item.variants.length}
					<p class="size-label">Size{#if pickedSize}: <strong>{sizeLabel(item, pickedSize)}</strong>{/if}</p>
					<SizePicker variants={item.variants} value={pickedSize} onchange={(sku) => setSizes({ ...bag.sizes, [item.id]: sku })} />
				{/if}
				<div class="buy">
					<Stepper value={inCart} max={item.max_per_request} onchange={(n) => setCart(setQuantity(bag.cart, item, n))} />
					{#if inCart === 0}
						<button disabled={needsSize} onclick={() => setCart(setQuantity(bag.cart, item, 1))}>{needsSize ? 'Pick a size' : 'Add to request'}</button>
					{/if}
				</div>
				{#if total > 0}
					<p class="ok">
						{inCart > 0 ? `${inCart}${pickedSize ? ` (${sizeLabel(item, pickedSize)})` : ''} in your request.` : ''}
						<a href="/#request">Review request ({total} item{total === 1 ? '' : 's'}) →</a>
					</p>
				{/if}
			{/if}
		</div>
	</div>

	{#if item.photos.length}
		<section class="community">
			<h2>In the wild</h2>
			<p class="muted">Photos people posted after theirs arrived.</p>
			<div class="photos">
				{#each item.photos as p (p.url)}
					<figure>
						<a href={p.url} target="_blank" rel="noreferrer"><img src={p.url} alt={p.caption || `${item.name}, photo by ${p.credit}`} loading="lazy" /></a>
						<figcaption>
							{#if p.caption}<span>{p.caption}</span>{/if}
							{#if p.credit}<span class="muted">📷 {p.credit}</span>{/if}
						</figcaption>
					</figure>
				{/each}
			</div>
		</section>
	{/if}
{:else if !error}
	<p class="muted">Loading…</p>
{/if}

<style>
	.product { display: grid; gap: 2rem; grid-template-columns: minmax(0, 1.1fr) minmax(0, 1fr); align-items: start; }
	@media (max-width: 720px) {
		.product { grid-template-columns: 1fr; gap: 1.25rem; }
	}
	.main {
		display: block; background: white; border: 1px solid var(--smoke); border-radius: 12px; padding: 1rem;
		cursor: zoom-in;
	}
	/* Leave room for the header and the thumbnail strip on short laptop screens. */
	.main img { display: block; width: 100%; max-height: max(18rem, calc(100vh - 16rem)); object-fit: contain; }
	.placeholder { display: grid; place-items: center; aspect-ratio: 1; font-size: 4rem; cursor: default; }
	.thumbs { display: flex; gap: 0.5rem; margin-top: 0.75rem; flex-wrap: wrap; }
	.thumb {
		background: white; border: 2px solid var(--smoke); border-radius: 8px; padding: 0.25rem;
		width: 4.5rem; height: 4.5rem;
	}
	.thumb.active { border-color: var(--red); }
	.thumb img { width: 100%; height: 100%; object-fit: contain; }
	.main.three { cursor: default; padding: 0.5rem; }
	.community { margin-top: 2.5rem; }
	.community h2 { margin-bottom: 0.25rem; }
	.photos { display: grid; gap: 1rem; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); margin-top: 1rem; }
	.photos figure { margin: 0; background: white; border: 1px solid var(--smoke); border-radius: 12px; overflow: hidden; }
	.photos img { display: block; width: 100%; aspect-ratio: 4 / 3; object-fit: cover; }
	.photos figcaption { display: grid; gap: 0.2rem; padding: 0.6rem 0.75rem 0.75rem; font-size: 0.9rem; }
	.three-thumb { position: relative; perspective: 120px; }
	.three-thumb img { transform: rotateY(-28deg) scale(0.85); }
	.three-thumb span {
		position: absolute; right: 0.2rem; bottom: 0.2rem; background: var(--red); color: white;
		font-size: 0.65rem; font-weight: 800; padding: 0.05rem 0.3rem; border-radius: 4px;
	}
	.info h1 { margin-bottom: 0.5rem; }
	.description { white-space: pre-line; line-height: 1.5; }
	.facts { padding-left: 1.1rem; line-height: 1.6; }
	.size-label { margin: 1rem 0 0.4rem; }
	.stock strong { color: var(--dark); }
	.nowrap { white-space: nowrap; }
	.buy { display: flex; gap: 0.75rem; align-items: center; flex-wrap: wrap; margin: 1rem 0 0.5rem; }
</style>
