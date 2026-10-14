<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, formatCents, isInternational } from '$lib/api';
	import { blankAddress, prefillAddress } from '$lib/address';
	import { bag, setCart, setSizes } from '$lib/bag.svelte';
	import { cartErrors, cartLines, emptyCart, pruneCart, pruneSizes, quantityOptions, setQuantity, sizeLabel, totalQuantity } from '$lib/cart';
	import { itemPath, thumbnail } from '$lib/items';
	import { session } from '$lib/session.svelte';
	import SizePicker from '$lib/SizePicker.svelte';
	import Stepper from '$lib/Stepper.svelte';
	import type { Address, Catalog, SwagRequest } from '$lib/types';

	let catalog = $state<Catalog | null>(null);
	// Prefilled from the person's Hack Club Auth address when they have one.
	let address = $state<Address>(prefillAddress(blankAddress(), session.me?.user?.address));
	let note = $state('');
	let error = $state('');
	let submitting = $state(false);

	onMount(async () => {
		try {
			catalog = await api.get<Catalog>('/api/catalog');
			setCart(pruneCart(bag.cart, catalog.items));
			setSizes(pruneSizes(bag.sizes, catalog.items));
			// "Review request" links from product pages land here as /#request.
			if (location.hash === '#request') {
				await tick();
				document.getElementById('request')?.scrollIntoView({ behavior: 'smooth' });
			}
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not load swag.';
		}
	});

	const cart = $derived(bag.cart);
	const sizes = $derived(bag.sizes);
	const problems = $derived(catalog ? cartErrors(cart, catalog.settings.max_items_per_request, catalog.items, sizes) : []);
	const intl = $derived(isInternational(address.country));

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (!catalog) return;
		error = '';
		submitting = true;
		try {
			const r = await api.post<SwagRequest>('/api/requests', {
				address: { ...address, country: address.country.trim().toUpperCase() },
				lines: cartLines(cart, catalog.items, sizes),
				note
			});
			setCart(emptyCart());
			setSizes({});
			if (r.payment_url) window.location.href = r.payment_url;
			else goto('/requests');
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Something went wrong.';
		} finally {
			submitting = false;
		}
	}
</script>

<svelte:head><title>Hack Club Mail Room</title></svelte:head>

<h1>Hack Club swag</h1>
<p class="muted intro">
	Pick what you'd like and we'll mail it to you. Shipping inside the US is on us; outside the US we'll ask
	you to cover shipping through HCB.
</p>

{#if error}<p class="error">{error}</p>{/if}

{#if catalog}
	{#if !catalog.settings.requests_open}
		<p class="error">Requests are closed right now. Check back soon!</p>
	{/if}
	<section class="grid">
		{#each catalog.items as item (item.id)}
			<div class="item" class:picked={cart[item.id]}>
				<a href={itemPath(item)} class="item-link">
					<div class="tile" class:sticker={item.packs === 'sticker'}>
						{#if thumbnail(item)}
							<img src={thumbnail(item)} alt={item.name} loading="lazy" />
						{:else}
							<span aria-hidden="true">📦</span>
						{/if}
					</div>
					<h3>{item.name}</h3>
				</a>
				{#if item.description}<p class="muted clamp">{item.description}</p>{/if}
				{#if session.me?.user && catalog.settings.requests_open}
					{#if item.variants.length}
						<SizePicker variants={item.variants} value={sizes[item.id]} label="Size of {item.name}"
							onchange={(sku) => { setSizes({ ...sizes, [item.id]: sku }); if (!cart[item.id]) setCart(setQuantity(cart, item, 1)); }} />
					{/if}
					<Stepper value={cart[item.id] ?? 0} max={item.max_per_request} options={quantityOptions(item)} label="Quantity of {item.name}"
						onchange={(n) => setCart(setQuantity(cart, item, n))} />
				{/if}
			</div>
		{:else}
			<p class="muted">Nothing listed yet.</p>
		{/each}
	</section>

	{#if !session.me?.user}
		<p><a class="button" href="/auth/login" data-sveltekit-reload>Sign in with Hack Club to request swag</a></p>
	{:else if catalog.settings.requests_open && totalQuantity(cart) > 0}
		<form class="card" id="request" style="margin-top: 1.5rem" onsubmit={submit}>
			<h2>Where should we send it?</h2>
			<ul class="summary">
				{#each catalog.items.filter((i) => cart[i.id]) as i (i.id)}
					<li>{cart[i.id]} × <a href={itemPath(i)}>{i.name}</a>{i.variants.length ? ` (${sizeLabel(i, sizes[i.id]) || 'pick a size'})` : ''}</li>
				{/each}
			</ul>
			<div class="form-grid">
				<label>First name <input required bind:value={address.first_name} autocomplete="given-name" /></label>
				<label>Last name <input required bind:value={address.last_name} autocomplete="family-name" /></label>
				<label>Address line 1 <input required bind:value={address.line_1} autocomplete="address-line1" /></label>
				<label>Address line 2 <input bind:value={address.line_2} autocomplete="address-line2" /></label>
				<label>City <input required bind:value={address.city} autocomplete="address-level2" /></label>
				<label>State / province <input bind:value={address.state} autocomplete="address-level1" /></label>
				<label>Postal code <input required bind:value={address.postal_code} autocomplete="postal-code" /></label>
				<label>Country (2-letter code) <input required maxlength="2" bind:value={address.country} autocomplete="country" /></label>
				<label>Phone {intl ? '(required for customs)' : '(optional)'}
					<input required={intl} bind:value={address.phone_number} autocomplete="tel" /></label>
			</div>
			<label style="margin-top: 0.75rem">Anything we should know? <textarea bind:value={note} rows="2"></textarea></label>
			{#if intl && catalog.settings.international_shipping_fee_cents > 0}
				<p>
					International shipping is <strong>{formatCents(catalog.settings.international_shipping_fee_cents)}</strong>.
					After you submit we'll send you to HCB to pay it; we ship once it's received.
				</p>
			{/if}
			{#each problems as p}<p class="error">{p}</p>{/each}
			<button disabled={submitting || problems.length > 0}>
				{submitting ? 'Sending…' : `Request ${totalQuantity(cart)} item${totalQuantity(cart) === 1 ? '' : 's'}`}
			</button>
		</form>
	{/if}
{:else if !error}
	<p class="muted">Loading swag…</p>
{/if}

<style>
	.intro { max-width: 44rem; line-height: 1.5; }
	.item { display: flex; flex-direction: column; gap: 0.5rem; min-width: 0; }
	.item-link { color: inherit; text-decoration: none; }
	.tile { display: grid; place-items: center; aspect-ratio: 4 / 5; background: var(--tile); font-size: 3rem; }
	.tile img { width: 80%; height: 80%; object-fit: contain; mix-blend-mode: multiply; }
	/* Sticker photos already sit on the tile's grey; multiply would darken it into a box. */
	.tile.sticker img { mix-blend-mode: normal; }
	.item.picked .tile { box-shadow: inset 0 0 0 2px var(--red); }
	.item-link:hover h3 { text-decoration: underline; text-decoration-thickness: 2px; }
	.item h3 { margin: 0.6rem 0 0; font-size: 0.9rem; letter-spacing: 0.01em; line-height: 1.2; }
	.item p { margin: 0; font-size: 0.85rem; line-height: 1.4; }
	.clamp { display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
	.summary { margin: 0 0 1rem; padding-left: 1.1rem; }
	#request { max-width: 56rem; }
</style>
