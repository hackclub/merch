<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { bag, setCart, setSizes } from '$lib/bag.svelte';
	import { pruneCart, pruneSizes, quantityOptions, setQuantity, setSizeQuantity, sizesSummary, totalQuantity } from '$lib/cart';
	import { itemsFor, offered } from '$lib/purpose';
	import { isBooklet, itemIdFromParam, itemPath, previewCount, productPreview, shipsWith, sizeStockLabel, thumbnail, unitCostLabel } from '$lib/items';
	import { sheetThickness, finishInfo } from '$lib/paper';
	import { programFor } from '$lib/programs';
	import { sizeLabel } from '$lib/scale';
	import Product3D from '$lib/Product3D.svelte';
	import { session } from '$lib/session.svelte';
	import SizeSteppers from '$lib/SizeSteppers.svelte';
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
			const listed = itemsFor(catalog.items, bag.purpose);
			setSizes(pruneSizes(bag.sizes, listed));
			setCart(pruneCart(bag.cart, listed, bag.sizes));
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not load this item.';
		}
	});

	const id = $derived(itemIdFromParam(page.params.slug ?? ''));
	const item = $derived(catalog?.items.find((i) => i.id === id));
	const inCart = $derived(item ? (bag.cart[item.id] ?? 0) : 0);
	const total = $derived(totalQuantity(bag.cart));
	// Printed items with a size and T-shirts get a true-to-scale 3D view, shown first; stickers
	// show the stack (or bags of 50) you're requesting. Booklets open in it and
	// flip through their pages.
	const isSticker = $derived(item?.packs === 'sticker');
	const booklet = $derived(!!item && isBooklet(item));
	const comesWith = $derived(item && catalog ? shipsWith(item, catalog.includes ?? []) : []);
	const preview = $derived(item ? productPreview(item, catalog?.includes ?? []) : null);
	const has3D = $derived(!!preview);
	const book = $derived(preview && 'book' in preview ? preview.book : undefined);
	const fold = $derived(preview && 'fold' in preview ? preview.fold : undefined);
	const shown = $derived(picked ?? (has3D ? '3d' : 0));
	const tiles = $derived(item ? item.images.length + (has3D ? 1 : 0) : 0);
	const pickedSizes = $derived(item ? sizesSummary(item, bag.sizes) : '');
	const finish = $derived(item ? finishInfo(item.finish).label : '');
	const program = $derived(item ? programFor(item.sku) : null);
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

<p class="back label"><a href="/">← All swag</a></p>

{#if error}<p class="error">{error}</p>{/if}

{#if catalog && !item}
	<h1>Not found</h1>
	<p class="muted">That item isn't listed right now. <a href="/">See what is.</a></p>
{:else if item}
	<div class="product">
		<div class="gallery">
			{#if item.images.length || has3D}
				{#if shown === '3d'}
					<div class="main three">
						{#if preview}
							{#key item.id}
								<Product3D name={item.name} {preview} count={item.packs === 'postcard' ? previewCount(item, inCart) : inCart} />
							{/key}
						{/if}
					</div>
				{:else}
					<a class="main" class:sticker={isSticker} href={item.images[shown]} target="_blank" rel="noreferrer" title="Open full size">
						<img src={item.images[shown]} alt="{item.name}, image {shown + 1} of {item.images.length}" />
					</a>
				{/if}
				{#if tiles > 1}
					<div class="thumbs">
						{#if has3D}
							<button type="button" class="thumb three-thumb" class:active={shown === '3d'}
								aria-label={booklet ? 'Flip through it in 3D' : fold ? 'Unfold it in 3D' : 'Show it in 3D, to scale'} onclick={() => (picked = '3d')}>
								<img src={thumbnail(item)} alt="" loading="lazy" />
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
			{#if program}
				<p class="program">Made for <a href={program.href} target="_blank" rel="noreferrer">{program.name} ↗</a></p>
			{/if}

			<ul class="facts">
				<li>Up to {item.max_per_request} per request{item.max_per_user != null ? `, ${item.max_per_user} per person` : ''}</li>
				<li>
					Free shipping in the US{#if catalog?.settings.international_shipping_fee_cents}; elsewhere you cover the postage{/if}
				</li>
				{#if book}
					<li>
						{item.pages.length} pages, {item.binding === 'staple' ? 'stapled' : 'with a square spine'}, about
						{(book.thicknessIn * 25.4).toFixed(0)} mm thick{finish ? `; ${finish.toLowerCase()} cover` : ''}
					</li>
				{:else if fold}
					<li>
						A trifold brochure, {sizeLabel(fold.widthIn / 3, fold.heightIn)} folded and {sizeLabel(fold.widthIn, fold.heightIn)} open{finish
							? `; printed on ${finish.toLowerCase()}`
							: ''}
					</li>
				{:else if finish}
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

			{#if comesWith.length}
				<div class="comes-with">
					{#each comesWith as extra (extra.id)}
						{#if thumbnail(extra)}<img src={thumbnail(extra)} alt="" />{/if}
						<p>
							<strong>Comes with the {extra.name}</strong><br />
							<span class="muted">One in the envelope with every copy{extra.visible ? '' : ', no need to add it'}.
								{#if extra.visible}<a href={itemPath(extra)}>See it</a>{/if}</span>
						</p>
					{/each}
				</div>
			{/if}

			{#if !catalog?.settings.requests_open}
				<p class="error">Requests are closed right now. Check back soon!</p>
			{:else if !session.me?.user}
				<p><a class="button" href="/auth/login" data-sveltekit-reload>Sign in with Hack Club to request this</a></p>
			{:else if !offered(item, bag.purpose)}
				<p class="muted">This is only for people running a Hack Club or an event. <a href="/">Change what your request is for</a>.</p>
			{:else}
				{#if item.variants.length}
					<p class="size-label">How many of each size?</p>
					<SizeSteppers {item} sizes={bag.sizes} onchange={(sku, n) => {
						const next = setSizeQuantity(bag.cart, bag.sizes, item, sku, n);
						setSizes(next.sizes);
						setCart(next.cart);
					}} />
				{:else}
					<div class="buy">
						<Stepper value={inCart} max={item.max_per_request} options={quantityOptions(item)} onchange={(n) => setCart(setQuantity(bag.cart, item, n))} />
						{#if inCart === 0}
							<button onclick={() => setCart(setQuantity(bag.cart, item, 1))}>Add to request</button>
						{/if}
					</div>
				{/if}
				{#if total > 0}
					<p class="ok">
						{inCart > 0 ? `${pickedSizes || inCart} in your request.` : ''}
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
	.back { margin: 0 0 1rem; }
	.back a { text-decoration: none; color: var(--muted); }
	.back a:hover { color: var(--dark); }
	.product { display: grid; gap: 2.5rem; grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr); align-items: start; }
	@media (max-width: 720px) {
		.product { grid-template-columns: 1fr; gap: 1.25rem; }
	}
	.main { display: grid; place-items: center; background: var(--tile); padding: 2rem; cursor: zoom-in; }
	/* Leave room for the header and the thumbnail strip on short laptop screens. */
	.main img { display: block; width: 100%; max-height: max(18rem, calc(100vh - 16rem)); object-fit: contain; mix-blend-mode: multiply; }
	.main.sticker img { mix-blend-mode: normal; }
	.placeholder { display: grid; place-items: center; aspect-ratio: 1; font-size: 4rem; cursor: default; }
	.thumbs { display: flex; gap: 0.4rem; margin-top: 0.4rem; flex-wrap: wrap; }
	.thumb { background: var(--tile); border: 0; padding: 0.3rem; width: 4.5rem; height: 4.5rem; box-shadow: inset 0 0 0 2px transparent; }
	.thumb.active { box-shadow: inset 0 0 0 2px var(--dark); }
	.thumb img { width: 100%; height: 100%; object-fit: contain; mix-blend-mode: multiply; }
	.main.three { cursor: default; padding: 0; }
	.community { margin-top: 3.5rem; }
	.community h2 { margin-bottom: 0.25rem; }
	.photos { display: grid; gap: 0.5rem; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); margin-top: 1rem; }
	.photos figure { margin: 0; }
	.photos img { display: block; width: 100%; aspect-ratio: 4 / 3; object-fit: cover; }
	.photos figcaption { display: grid; gap: 0.2rem; padding: 0.5rem 0 0.75rem; font-size: 0.9rem; }
	.three-thumb { position: relative; perspective: 120px; }
	.three-thumb img { transform: rotateY(-28deg) scale(0.85); }
	.three-thumb span {
		position: absolute; right: 0.2rem; bottom: 0.2rem; background: var(--red); color: white;
		font-family: var(--mono); font-size: 0.6rem; padding: 0.05rem 0.3rem;
	}
	.info { position: sticky; top: 5.5rem; }
	@media (max-width: 720px) {
		.info { position: static; }
	}
	.info h1 { margin-bottom: 0.75rem; font-size: 1.75rem; }
	.description { white-space: pre-line; line-height: 1.55; }
	.program { margin: 0.75rem 0; }
	.facts { list-style: none; padding: 0; margin: 1.25rem 0; border-top: 1px solid var(--smoke); }
	.facts li {
		padding: 0.65rem 0; border-bottom: 1px solid var(--smoke); font-family: var(--mono); font-size: 0.75rem;
		text-transform: uppercase; letter-spacing: 0.02em; color: #55555d; line-height: 1.45;
	}
	.size-label { margin: 1.25rem 0 0.5rem; font-family: var(--mono); font-size: 0.75rem; text-transform: uppercase; }
	.stock strong { color: var(--dark); }
	.nowrap { white-space: nowrap; }
	.comes-with {
		display: grid; grid-template-columns: auto 1fr; gap: 0.75rem; align-items: center; margin: 1rem 0;
		padding: 0.75rem; border: 1.5px dashed var(--smoke); background: white;
	}
	.comes-with img { width: 4.5rem; aspect-ratio: 3 / 2; object-fit: cover; box-shadow: 0 2px 6px rgba(0, 0, 0, 0.15); transform: rotate(-4deg); }
	.comes-with p { margin: 0; line-height: 1.4; }
	.buy { display: flex; gap: 0.5rem; align-items: center; flex-wrap: wrap; margin: 1.25rem 0 0.5rem; }
</style>
