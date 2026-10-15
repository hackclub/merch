<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, formatCents, isInternational } from '$lib/api';
	import { blankAddress, prefillAddress } from '$lib/address';
	import AddressLine1 from '$lib/AddressLine1.svelte';
	import { applyPlace } from '$lib/addressSearch';
	import { bag, setCart, setPurpose, setSizes } from '$lib/bag.svelte';
	import { cartCosts, cartErrors, cartLines, emptyCart, pruneCart, pruneSizes, quantityOptions, setQuantity, sizeLabel, totalQuantity } from '$lib/cart';
	import { gallery, roughFee } from '$lib/gallery';
	import { formatUnitCost, itemPath, previewCount, thumbnail, tilePreview } from '$lib/items';
	import { itemsFor, needsReason, PURPOSES } from '$lib/purpose';
	import { sectionize, tileFact } from '$lib/sections';
	import { session } from '$lib/session.svelte';
	import SizePicker from '$lib/SizePicker.svelte';
	import Stepper from '$lib/Stepper.svelte';
	import Tile3D from '$lib/Tile3D.svelte';
	import { bagLabel } from '$lib/stickers';
	import type { Address, Catalog, ItemStock, Purpose, SwagRequest } from '$lib/types';

	let catalog = $state<Catalog | null>(null);
	// Prefilled from the person's Hack Club Auth address when they have one.
	let address = $state<Address>(prefillAddress(blankAddress(), session.me?.user?.address));
	let note = $state('');
	let understood = $state(false);
	let error = $state('');
	let submitting = $state(false);
	// Live warehouse unit costs for what's in the bag, fetched once per item.
	let stocks = $state<Record<number, ItemStock>>({});
	const fetching = new Set<number>();

	onMount(async () => {
		try {
			catalog = await api.get<Catalog>('/api/catalog');
			setCart(pruneCart(bag.cart, itemsFor(catalog.items, bag.purpose)));
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
	const purpose = $derived(bag.purpose);
	const purposeInfo = $derived(PURPOSES.find((p) => p.value === purpose));
	// Personal requests don't offer club-only items (the full-size magazine, the T-shirt).
	const shown = $derived(catalog ? itemsFor(catalog.items, purpose) : []);
	const sections = $derived(sectionize(shown));
	const asking = $derived(!!session.me?.user && !!catalog?.settings.requests_open);
	// Anyone can fill the bag; signing in only comes at the end, when they send it.
	const open = $derived(!!catalog?.settings.requests_open);
	const count = $derived(totalQuantity(cart));
	const problems = $derived(catalog ? cartErrors(cart, catalog.settings.max_items_per_request, catalog.items, sizes) : []);
	const intl = $derived(isInternational(address.country));
	const costs = $derived(catalog ? cartCosts(cart, catalog.items, sizes, stocks) : null);
	const shippingCents = $derived(intl ? catalog?.settings.international_shipping_fee_cents ?? 0 : 0);
	const slides = $derived(catalog ? gallery(catalog.items) : []);
	const fee = $derived(catalog?.settings.international_shipping_fee_cents ?? 0);
	const dollars = (n: number) => formatCents(Math.round(n * 100));

	$effect(() => {
		if (!session.me?.user) return;
		for (const id of Object.keys(cart).map(Number)) {
			if (fetching.has(id)) continue;
			fetching.add(id);
			// No cost just leaves the line unpriced; it never blocks the request.
			api.get<ItemStock>(`/api/items/${id}/stock`).then((s) => (stocks = { ...stocks, [id]: s }), () => {});
		}
	});

	function choose(p: Purpose) {
		setPurpose(p);
		if (catalog) setCart(pruneCart(cart, itemsFor(catalog.items, p)));
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (!catalog) return;
		error = '';
		submitting = true;
		try {
			const r = await api.post<SwagRequest>('/api/requests', {
				purpose,
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

<section class="hero">
	<h1>Teenager and want to spread <span class="box">Hack Club</span> in your community?</h1>
	<p class="lede">Get some awesome hand-designed swag made by your fellow Hack Clubbers!</p>
	<ul class="facts label">
		<li>Free shipping in the US.{#if fee > 0}{' '}{roughFee(fee)} globally to help cover shipping.{/if}</li>
		<li>Shipped next day Mon–Fri. 2–5 day delivery in US. 2–4 week delivery globally.</li>
	</ul>
</section>

{#if slides.length}
	<!-- The list renders twice so the strip loops without a seam; the copy is hidden from screen readers. -->
	<div class="strip">
		<div class="track" style="--n: {slides.length}">
			{#each [0, 1] as copy}
				{#each slides as s, i (i)}
					<a href={s.href} class="slide" class:real={s.real} class:sticker={s.sticker} aria-hidden={copy === 1} tabindex={copy === 1 ? -1 : undefined}>
						<img src={s.src} alt={copy === 1 ? '' : s.alt} loading="lazy" />
					</a>
				{/each}
			{/each}
		</div>
	</div>
{/if}

{#if error}<p class="error">{error}</p>{/if}

{#if catalog}
	{#if !catalog.settings.requests_open}
		<p class="error">Requests are closed right now. Check back soon!</p>
	{/if}
	{#if asking}
		<fieldset class="ask" id="purpose">
			<legend><h2>What kind of request is this?</h2></legend>
			<p class="muted">It changes which items you can pick.</p>
			{#each PURPOSES as p (p.value)}
				<label class="choice" class:on={purpose === p.value}>
					<input type="radio" name="purpose" value={p.value} checked={purpose === p.value} onchange={() => choose(p.value)} />
					{p.label}
				</label>
			{/each}
		</fieldset>
	{/if}
	<h2 class="pick">What would you like us to send you?</h2>
	{#each sections as { section, tiles }, n (section.key)}
		<section class="shelf" aria-labelledby="shelf-{section.key}">
			<div class="shelf-head">
				<span class="label muted">{String(n + 1).padStart(2, '0')}</span>
				<h2 id="shelf-{section.key}">{section.title}</h2>
				{#if section.blurb}<p>{section.blurb}</p>{/if}
				{#if section.link}<a class="label" href={section.link.href} target="_blank" rel="noreferrer">{section.link.label} ↗</a>{/if}
			</div>
			{#each tiles as { item, big } (item.id)}
				{@const preview = tilePreview(item)}
				<div class="item" class:big class:picked={cart[item.id]}>
					<a href={itemPath(item)} class="item-link">
						<div class="tile" class:sticker={item.packs === 'sticker'} class:swap={!preview && item.images.length > 1}>
							{#if thumbnail(item)}
								<img src={thumbnail(item)} alt={item.name} loading="lazy" />
								{#if !preview && item.images.length > 1}<img class="alt" src={item.images[1]} alt="" loading="lazy" />{/if}
							{:else}
								<span aria-hidden="true">📦</span>
							{/if}
							{#if preview}<Tile3D {preview} count={previewCount(item, cart[item.id] ?? 0)} rest={'stickers' in preview ? -0.25 : -0.45} />{/if}
							{#if item.packs === 'sticker'}<span class="pack label">{bagLabel(previewCount(item, cart[item.id] ?? 0))}</span>{/if}
						</div>
						<h3>{item.name}</h3>
					</a>
					<p class="fact label">{tileFact(item)}</p>
					{#if big && item.description}<p class="muted clamp">{item.description}</p>{/if}
					{#if open}
						<div class="add">
							{#if item.variants.length}
								<SizePicker variants={item.variants} value={sizes[item.id]} label="Size of {item.name}"
									onchange={(sku) => { setSizes({ ...sizes, [item.id]: sku }); if (!cart[item.id]) setCart(setQuantity(cart, item, 1)); }} />
							{/if}
							{#if cart[item.id]}
								<Stepper value={cart[item.id]} max={item.max_per_request} options={quantityOptions(item)} label="Quantity of {item.name}"
									onchange={(n) => setCart(setQuantity(cart, item, n))} />
							{:else if !item.variants.length}
								<button type="button" class="secondary" onclick={() => setCart(setQuantity(cart, item, 1))}>Add</button>
							{/if}
						</div>
					{/if}
				</div>
			{/each}
		</section>
	{:else}
		<p class="muted">Nothing listed yet.</p>
	{/each}

	{#if !session.me?.user}
		<p id="request" class="send"><a class="button" href="/auth/login" data-sveltekit-reload
			>{count ? `Sign in with Hack Club to send ${count} item${count === 1 ? '' : 's'}` : 'Sign in with Hack Club to request swag'}</a></p>
	{:else if asking && !purpose}
		<p class="muted">Pick what kind of request this is at the top, then we'll ask where to send it.</p>
	{:else if asking && totalQuantity(cart) > 0}
		<form class="card" id="request" style="margin-top: 1.5rem" onsubmit={submit}>
			<h2>Shipping info</h2>
			{#if costs}
				<table class="summary">
					<tbody>
						{#each costs.lines as l (l.item.id)}
							<tr>
								<td>{l.quantity} × <a href={itemPath(l.item)}>{l.item.name}</a>{l.item.variants.length ? ` (${sizeLabel(l.item, sizes[l.item.id]) || 'pick a size'})` : ''}</td>
								<td class="num muted">{l.unitCost ? `${formatUnitCost(l.unitCost)} each` : ''}</td>
								<td class="num">{l.total === null ? '—' : dollars(l.total)}</td>
							</tr>
						{/each}
					</tbody>
					<tfoot>
						<tr>
							<th colspan="2">Subtotal <span class="muted">(Hack Club covers it)</span></th>
							<td class="num">{costs.subtotal === null ? '—' : dollars(costs.subtotal)}</td>
						</tr>
						<tr>
							<th colspan="2">Shipping <span class="muted">({intl ? 'outside the US, you pay' : 'inside the US'})</span></th>
							<td class="num">{shippingCents > 0 ? formatCents(shippingCents) : 'Free'}</td>
						</tr>
					</tfoot>
				</table>
			{/if}
			<div class="form-grid">
				<label>First name <input required bind:value={address.first_name} autocomplete="given-name" /></label>
				<label>Last name <input required bind:value={address.last_name} autocomplete="family-name" /></label>
				<AddressLine1 bind:value={address.line_1} onpick={(place) => (address = applyPlace(address, place))} />
				<label>Address line 2 <input bind:value={address.line_2} autocomplete="address-line2" /></label>
				<label>City <input required bind:value={address.city} autocomplete="address-level2" /></label>
				<label>State / province <input bind:value={address.state} autocomplete="address-level1" /></label>
				<label>Postal code <input required bind:value={address.postal_code} autocomplete="postal-code" /></label>
				<label>Country (2-letter code) <input required maxlength="2" bind:value={address.country} autocomplete="country" /></label>
				<label>Phone {intl ? '(required for customs)' : '(optional)'}
					<input required={intl} bind:value={address.phone_number} autocomplete="tel" /></label>
			</div>
			{#if needsReason(purpose)}
				<label style="margin-top: 0.75rem">{purposeInfo?.reason}
					<textarea bind:value={note} rows="3" required placeholder={purposeInfo?.example}></textarea></label>
			{:else}
				<label style="margin-top: 0.75rem">Anything we should know? (optional) <textarea bind:value={note} rows="2"></textarea></label>
			{/if}
			{#if intl && catalog.settings.international_shipping_fee_cents > 0}
				<p>
					International shipping is <strong>{formatCents(catalog.settings.international_shipping_fee_cents)}</strong>.
					After you submit we'll send you to HCB to pay it; we ship once it's received.
				</p>
			{/if}
			<label class="choice understand" class:on={understood}>
				<input type="checkbox" required bind:checked={understood} />
				<span>
					<span class="p">Hack Club is a charity. The absolutely most effective way we can get more teenagers into Hack Club is to help people like you spread Hack Club!</span>
					<span class="p">However - it is wasteful if we send stuff in excess. An example of this would be if you are requesting 5+ shirts and not using them. If you are requesting a bunch and wearing them - please get more!</span>
					<span class="p">Also - Please do not use this form to request t-shirts for other people. This form is for t-shirts you will wear yourself personally. If you need t-shirts for prizes or for an event, please get in touch with Zach.</span>
					<span class="p">Just please don't abuse this system. Check this box to confirm you understand :-)</span>
				</span>
			</label>
			{#each problems as p}<p class="error">{p}</p>{/each}
			<button disabled={submitting || problems.length > 0 || !understood}>
				{submitting ? 'Sending…' : `Submit request (${totalQuantity(cart)} item${totalQuantity(cart) === 1 ? '' : 's'})`}
			</button>
		</form>
	{/if}
	{#if count > 0}
		<a class="bagbar" href={!session.me?.user ? '/auth/login' : purpose ? '#request' : '#purpose'} data-sveltekit-reload={session.me?.user ? undefined : true}>
			<span class="label">{count} item{count === 1 ? '' : 's'} · free</span>
			<span>{session.me?.user ? (purpose ? 'Send it →' : 'What’s it for? →') : 'Sign in to send →'}</span>
		</a>
	{/if}
{:else if !error}
	<p class="muted">Loading swag…</p>
{/if}

<style>
	/* One row per section: a header cell, the featured item two tiles wide, then the rest. */
	.shelf { display: grid; gap: 1.75rem 0.5rem; grid-template-columns: repeat(4, minmax(0, 1fr)); margin-bottom: 3.5rem; }
	@media (max-width: 1000px) { .shelf { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
	@media (max-width: 600px) { .shelf { grid-template-columns: 1fr 1fr; gap: 1.5rem 0.4rem; margin-bottom: 2.5rem; } }
	.shelf-head { display: flex; flex-direction: column; justify-content: flex-end; gap: 0.5rem; padding: 0 1rem 0.25rem 0; border-top: 2px solid var(--dark); }
	.shelf-head h2 { margin: 0; font-size: clamp(1.4rem, 2.4vw, 2.1rem); letter-spacing: -0.01em; }
	.shelf-head p { margin: 0; line-height: 1.45; max-width: 22rem; }
	.shelf-head a { color: var(--red); text-decoration: none; }
	.shelf-head a:hover { text-decoration: underline; }
	.shelf-head > .label:first-child { margin-top: 0.6rem; margin-bottom: auto; }
	@media (max-width: 600px) {
		.shelf-head { grid-column: 1 / -1; padding: 0; }
		.shelf-head > .label:first-child { margin-bottom: 0.5rem; }
	}
	.item.big { grid-column: span 2; }
	/* As tall as the 4:5 tiles beside it. */
	.item.big .tile { aspect-ratio: 8 / 5; min-height: 0; overflow: hidden; }
	@media (max-width: 600px) { .item.big .tile { aspect-ratio: 5 / 4; } }
	.item.big h3 { font-size: 1.15rem; }
	.fact { margin: -0.2rem 0 0; color: #55555d; }
	.add { display: flex; flex-wrap: wrap; gap: 0.4rem; align-items: center; margin-top: auto; padding-top: 0.25rem; }
	.add > button { height: 2.6rem; padding: 0 1.4rem; }
	/* Items without a 3D view show their second photo on hover, like a lookbook. */
	.tile .alt { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; mix-blend-mode: normal; opacity: 0; transition: opacity 0.25s; }
	.tile.swap:hover .alt { opacity: 1; }
	.send { margin-top: 1rem; }
	.bagbar {
		position: fixed; left: 50%; bottom: 1rem; transform: translateX(-50%); z-index: 2; display: flex; gap: 1.5rem; align-items: center;
		padding: 0.85rem 1.25rem; background: var(--dark); color: white; text-decoration: none; font-weight: 700; text-transform: uppercase;
		font-size: 0.85rem; letter-spacing: 0.04em; box-shadow: 0 6px 24px rgba(0, 0, 0, 0.25); white-space: nowrap;
	}
	.bagbar:hover { background: var(--red); }
	.hero { max-width: 56rem; margin: 0.5rem 0 1.75rem; }
	.hero h1 { font-size: clamp(2rem, 5.5vw, 3.6rem); line-height: 1.02; margin-bottom: 0.9rem; }
	.hero h1 .box { background: var(--red); color: #fff; padding: 0 0.25em 0.04em; box-decoration-break: clone; -webkit-box-decoration-break: clone; }
	.lede { font-size: 1.2rem; margin: 0 0 1rem; }
	.facts { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.35rem; color: var(--dark); }
	.facts li::before { content: '→ '; color: var(--red); }
	/* Edge to edge; it scrolls on its own and pauses under the pointer. */
	.strip { margin: 0 calc(50% - 50vw) 2.5rem; overflow: hidden; }
	.track { display: flex; gap: 0.75rem; width: max-content; animation: scroll calc(var(--n) * 4s) linear infinite; }
	.strip:hover .track { animation-play-state: paused; }
	@keyframes scroll { to { transform: translateX(calc(-50% - 0.375rem)); } }
	@media (prefers-reduced-motion: reduce) {
		.strip { overflow-x: auto; }
		.track { animation: none; }
	}
	.slide { display: block; flex: none; height: clamp(11rem, 26vw, 17rem); aspect-ratio: 4 / 5; background: var(--tile); }
	.slide img { width: 100%; height: 100%; object-fit: contain; padding: 8%; box-sizing: border-box; mix-blend-mode: multiply; }
	.slide.sticker img { mix-blend-mode: normal; }
	/* Real-world photos fill their frame, wider, at full colour. */
	.slide.real { aspect-ratio: 4 / 3; }
	.slide.real img { object-fit: cover; padding: 0; mix-blend-mode: normal; }
	.item { display: flex; flex-direction: column; gap: 0.5rem; min-width: 0; }
	.item-link { color: inherit; text-decoration: none; }
	.tile { position: relative; display: grid; place-items: center; aspect-ratio: 4 / 5; background: var(--tile); font-size: 3rem; }
	.tile img { width: 80%; height: 80%; object-fit: contain; mix-blend-mode: multiply; }
	/* Sticker photos already sit on the tile's grey; multiply would darken it into a box. */
	.tile.sticker img { mix-blend-mode: normal; }
	/* Once the 3D view is up it replaces the photo. */
	.tile:has(:global(.spin.ready)) img { opacity: 0; }
	.pack { position: absolute; left: 0.5rem; bottom: 0.5rem; padding: 0.2rem 0.45rem; background: var(--dark); color: #fff; font-size: 0.68rem; pointer-events: none; }
	.item.picked .tile { box-shadow: inset 0 0 0 2px var(--red); }
	.item-link:hover h3 { text-decoration: underline; text-decoration-thickness: 2px; }
	.item h3 { margin: 0.6rem 0 0; font-size: 0.9rem; letter-spacing: 0.01em; line-height: 1.2; }
	.item p { margin: 0; font-size: 0.85rem; line-height: 1.4; }
	.clamp { display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
	.summary { width: 100%; max-width: 36rem; margin: 0 0 1rem; border-collapse: collapse; }
	.summary td, .summary th { padding: 0.3rem 0; border: 0; vertical-align: baseline; }
	.summary .num { padding-left: 1rem; text-align: right; white-space: nowrap; font-family: var(--mono); font-size: 0.8rem; }
	.summary tfoot th { text-align: left; font-family: var(--mono); font-size: 0.72rem; font-weight: 400; text-transform: uppercase; letter-spacing: 0.02em; }
	.summary tfoot tr:first-child th, .summary tfoot tr:first-child td { border-top: 1.5px solid var(--dark); padding-top: 0.5rem; }
	#request { max-width: 56rem; }
	.pick { margin: 0 0 1.5rem; }
	.ask { max-width: 56rem; margin: 0 0 2rem; padding: 0; border: 0; display: grid; gap: 0.5rem; }
	.ask legend { padding: 0; }
	.ask legend h2 { margin: 0 0 0.25rem; }
	.ask p { margin: 0 0 0.5rem; }
	/* Square, bordered options; the picked one takes the red. */
	.choice {
		display: flex; gap: 0.75rem; align-items: flex-start; padding: 0.8rem 1rem; border: 1.5px solid var(--smoke); cursor: pointer;
		font-family: inherit; font-size: 0.95rem; text-transform: none; letter-spacing: 0; color: var(--dark); line-height: 1.4;
	}
	.choice:hover { border-color: var(--dark); }
	.choice.on { border-color: var(--red); box-shadow: inset 0 0 0 1px var(--red); }
	.choice input { width: auto; margin: 0.2rem 0 0; accent-color: var(--red); }
	.understand { margin: 1rem 0; }
	.understand .p { display: block; margin-bottom: 0.6rem; }
	.understand .p:last-child { margin-bottom: 0; font-weight: 700; }
</style>
