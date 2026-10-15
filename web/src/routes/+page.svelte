<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, formatCents, isInternational } from '$lib/api';
	import { addressComplete, addressSummary, blankAddress, prefillAddress } from '$lib/address';
	import AddressLine1 from '$lib/AddressLine1.svelte';
	import { applyPlace } from '$lib/addressSearch';
	import { bag, setCart, setPurpose, setSizes } from '$lib/bag.svelte';
	import { cartErrors, cartLines, emptyCart, pruneCart, pruneSizes, quantityOptions, setQuantity, setSizeQuantity, sizesSummary, totalQuantity } from '$lib/cart';
	import { gallery, roughFee } from '$lib/gallery';
	import { itemPath, thumbnail } from '$lib/items';
	import { itemsFor, needsReason, PURPOSES } from '$lib/purpose';
	import { sectionize, tileFact } from '$lib/sections';
	import { session } from '$lib/session.svelte';
	import SizeSteppers from '$lib/SizeSteppers.svelte';
	import Stepper from '$lib/Stepper.svelte';
	import type { Address, Catalog, Item, Purpose, SwagRequest } from '$lib/types';

	let catalog = $state<Catalog | null>(null);
	// Prefilled from the person's Hack Club Auth address when they have one.
	let address = $state<Address>(prefillAddress(blankAddress(), session.me?.user?.address));
	// A complete address folds into a "Ship to" summary. Editing shows the fields until they press "Use this address";
	// `manual` shows every field instead of just the autocomplete search.
	let editing = $state(!addressComplete(address));
	let manual = $state(!!address.line_1.trim());
	let note = $state('');
	let understood = $state(false);
	let error = $state('');
	let submitting = $state(false);

	onMount(async () => {
		try {
			catalog = await api.get<Catalog>('/api/catalog');
			const listed = itemsFor(catalog.items, bag.purpose);
			setSizes(pruneSizes(bag.sizes, listed));
			setCart(pruneCart(bag.cart, listed, bag.sizes));
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
	// One photo under the intro, like the old Fillout forms: the first real-world one when there is one.
	const photo = $derived(catalog ? gallery(catalog.items)[0] : undefined);
	const fee = $derived(catalog?.settings.international_shipping_fee_cents ?? 0);

	function choose(p: Purpose) {
		setPurpose(p);
		if (!catalog) return;
		const listed = itemsFor(catalog.items, p);
		setSizes(pruneSizes(sizes, listed));
		setCart(pruneCart(cart, listed, bag.sizes));
	}

	function pickPlace(place: Address) {
		address = applyPlace(address, place);
		if (addressComplete(address)) editing = manual = false;
		else manual = true;
	}

	function setSize(item: Item, sku: string, n: number) {
		const next = setSizeQuantity(cart, sizes, item, sku, n);
		setSizes(next.sizes);
		setCart(next.cart);
	}

	// The item whose details float beside the list while the pointer is over its row.
	let peek = $state<number | null>(null);

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

<!-- One simple column, top to bottom, like the old Fillout request forms: intro, photo, questions, shipping, submit. -->
<div class="sheet">
	<div class="intro">
		<p>Teenager and want to spread <a href="https://hackclub.com" target="_blank" rel="noreferrer">Hack Club</a> in your community?</p>
		<p>Get some awesome hand-designed swag made by your fellow Hack Clubbers!</p>
		<p>Free shipping in the US.{#if fee > 0}{' '}{roughFee(fee)} to help cover shipping globally.{/if}</p>
		<p>Shipped next day Mon–Fri. 2–5 day delivery in US. 2–4 week delivery globally.</p>
	</div>

	{#if photo}
		<a class="photo" class:product={!photo.real} href={photo.href}><img src={photo.src} alt={photo.alt} /></a>
	{/if}

	{#if error}<p class="error">{error}</p>{/if}

	{#if catalog}
		{#if !catalog.settings.requests_open}
			<p class="error">Requests are closed right now. Check back soon!</p>
		{/if}
		{#if asking}
			<fieldset class="ask" id="purpose">
				<legend>What kind of request is this?</legend>
				{#each PURPOSES as p (p.value)}
					<label class="choice" class:on={purpose === p.value}>
						<input type="radio" name="purpose" value={p.value} checked={purpose === p.value} onchange={() => choose(p.value)} />
						{p.label}
					</label>
				{/each}
			</fieldset>
		{/if}

		<h2>What would you like us to send you?</h2>
		{#each sections as { section, tiles } (section.key)}
			<section class="group" aria-labelledby="group-{section.key}">
				<h3 id="group-{section.key}">{section.title}</h3>
				{#each tiles as { item } (item.id)}
					<!-- svelte-ignore a11y_no_static_element_interactions -->
					<div class="line" class:picked={cart[item.id]} class:sized={item.variants.length > 0}
						onmouseenter={() => (peek = item.id)} onmouseleave={() => (peek = null)}>
						<a href={itemPath(item)} class="thumb" class:sticker={item.packs === 'sticker'} tabindex="-1" aria-hidden="true">
							{#if thumbnail(item)}<img src={thumbnail(item)} alt="" loading="lazy" />{/if}
						</a>
						<div class="what">
							<a href={itemPath(item)}>{item.name}</a>
							<span class="muted">{tileFact(item)}{#if sizesSummary(item, sizes)}{' · '}<strong class="in">{sizesSummary(item, sizes)}</strong>{/if}</span>
						</div>
						{#if open}
							<div class="add">
								{#if item.variants.length}
									<SizeSteppers {item} {sizes} onchange={(sku, n) => setSize(item, sku, n)} />
								{:else if cart[item.id]}
									<Stepper value={cart[item.id]} max={item.max_per_request} options={quantityOptions(item)} label="Quantity of {item.name}"
										onchange={(n) => setCart(setQuantity(cart, item, n))} />
								{:else}
									<button type="button" class="secondary" onclick={() => setCart(setQuantity(cart, item, 1))}>Add</button>
								{/if}
							</div>
						{/if}
						{#if peek === item.id}
							<div class="peek" aria-hidden="true">
								{#if thumbnail(item)}<div class="peek-img" class:sticker={item.packs === 'sticker'}><img src={thumbnail(item)} alt="" /></div>{/if}
								<strong>{item.name}</strong>
								<span class="muted">{tileFact(item)}</span>
								{#if item.description}<p>{item.description}</p>{/if}
								<span class="muted">Click for photos{item.width_in ? ', the 3D view' : ''} and more →</span>
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
			<form id="request" onsubmit={submit}>
				<h2>Shipping info</h2>
				{#if !editing}
					<div class="ship-to">
						<div>
							<span class="muted">Ship to</span>
							<address>{#each addressSummary(address) as l}<span>{l}</span>{/each}</address>
						</div>
						<button type="button" class="link" onclick={() => (editing = manual = true)}>Edit</button>
					</div>
				{:else}
					<div class="form-grid">
						<label>First name <input required bind:value={address.first_name} autocomplete="given-name" /></label>
						<label>Last name <input required bind:value={address.last_name} autocomplete="family-name" /></label>
					</div>
					<AddressLine1 bind:value={address.line_1} onpick={pickPlace} onunavailable={() => (manual = true)}
						label={manual ? 'Address line 1' : 'Address'} placeholder="Start typing your street address…"
						hint={manual ? 'Type to search for a different address, or fix any field below.' : 'Pick it from the suggestions and we’ll fill in the rest.'} />
					{#if manual}
						<div class="form-grid">
							<label>Address line 2 (optional) <input bind:value={address.line_2} autocomplete="address-line2" /></label>
							<label>City <input required bind:value={address.city} autocomplete="address-level2" /></label>
							<label>State / province <input bind:value={address.state} autocomplete="address-level1" /></label>
							<label>ZIP / postal code <input required bind:value={address.postal_code} autocomplete="postal-code" /></label>
							<label>Country code (US, CA, GB…) <input required maxlength="2" bind:value={address.country} autocomplete="country" /></label>
							<label>Phone {intl ? '(required for customs)' : '(optional)'}
								<input required={intl} bind:value={address.phone_number} autocomplete="tel" /></label>
						</div>
						{#if addressComplete(address)}
							<button type="button" class="secondary done" onclick={() => (editing = manual = false)}>Use this address</button>
						{/if}
					{:else}
						<button type="button" class="link" onclick={() => (manual = true)}>Can’t find it? Type it in yourself</button>
					{/if}
				{/if}
				{#if needsReason(purpose)}
					<label>{purposeInfo?.reason}
						<textarea bind:value={note} rows="3" required placeholder={purposeInfo?.example}></textarea></label>
				{:else}
					<label>Anything we should know? (optional) <textarea bind:value={note} rows="2"></textarea></label>
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
	{:else if !error}
		<p class="muted">Loading swag…</p>
	{/if}
</div>

<style>
	/* Grey page, one white sheet: the request form is the whole page. */
	:global(html:has(.sheet)) { background: var(--tile); }
	.sheet { max-width: 40rem; margin: 0 auto; padding: 2rem; background: white; line-height: 1.5; }
	@media (max-width: 600px) { .sheet { margin: -1.25rem -1rem -3rem; padding: 1.5rem 1.25rem 3rem; } }
	/* One font and one size throughout; the site's uppercase mono labels don't apply here. */
	.sheet :global(:is(label, h2, h3, legend, button, .button, .stepper select, .stepper input)) {
		font-family: inherit; text-transform: none; letter-spacing: 0;
	}
	.intro p { margin: 0 0 0.6rem; }
	.photo { display: flex; justify-content: center; margin: 1.25rem 0 0; }
	.photo img { display: block; max-width: 100%; max-height: 28rem; }
	.photo.product { background: var(--tile); }
	.photo.product img { padding: 2rem; box-sizing: border-box; mix-blend-mode: multiply; }
	h2 { margin: 2rem 0 1rem; padding-top: 1.5rem; border-top: 1px solid var(--smoke); font-size: 1.25rem; line-height: 1.3; }
	.ask { margin: 2rem 0 0; padding: 1.5rem 0 0; border: 0; border-top: 1px solid var(--smoke); display: grid; gap: 0.5rem; }
	.ask legend { float: left; width: 100%; margin-bottom: 0.5rem; padding: 0; font-weight: 700; font-size: 1.25rem; line-height: 1.3; }
	.group { margin-bottom: 1.5rem; }
	.group h3 { margin: 0 0 0.25rem; font-size: 1rem; line-height: 1.3; }
	/* A row per item: thumbnail, name and facts, then the add control. */
	.line { display: flex; gap: 0.9rem; align-items: center; padding: 0.6rem 0; border-bottom: 1px solid var(--smoke); }
	.thumb { flex: none; display: grid; place-items: center; width: 3.75rem; height: 3.75rem; background: var(--tile); }
	.thumb img { width: 85%; height: 85%; object-fit: contain; mix-blend-mode: multiply; }
	.thumb.sticker img { mix-blend-mode: normal; }
	.line.picked .thumb { box-shadow: inset 0 0 0 2px var(--red); }
	.what { flex: 1; min-width: 0; display: grid; }
	.what a { text-decoration: none; font-weight: 700; }
	.what a:hover { text-decoration: underline; }
	.what .muted { font-size: 0.9rem; }
	.add { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 0.4rem; align-items: center; }
	.add > button { height: 2.6rem; padding: 0 1.2rem; font-size: 1rem; }
	.what .in { color: var(--dark); }
	/* Sizes get their own line under the name. */
	.line.sized { flex-wrap: wrap; }
	.line.sized .add { flex-basis: 100%; justify-content: flex-start; padding-left: 4.65rem; }
	@media (max-width: 600px) {
		.line { flex-wrap: wrap; }
		.add { flex-basis: 100%; justify-content: flex-start; padding-left: 4.65rem; }
	}
	/* Hovering a row shows its details in the grey space beside the sheet, on screens wide enough for it. */
	.line { position: relative; }
	.peek { display: none; }
	@media (hover: hover) and (min-width: 74rem) {
		.peek {
			display: flex; flex-direction: column; gap: 0.35rem; position: absolute; z-index: 2; top: 50%; transform: translateY(-40%);
			left: calc(100% + 3.5rem); width: min(20rem, calc((100vw - 40rem) / 2 - 3.5rem)); padding: 1rem;
			background: white; box-shadow: 0 6px 24px rgba(0, 0, 0, 0.12); pointer-events: none; font-size: 0.9rem; line-height: 1.45;
		}
		.peek p { margin: 0; display: -webkit-box; -webkit-line-clamp: 6; line-clamp: 6; -webkit-box-orient: vertical; overflow: hidden; }
		.peek-img { display: flex; align-items: center; justify-content: center; width: 100%; aspect-ratio: 4 / 3; margin-bottom: 0.4rem; background: var(--tile); }
		.peek-img img { width: 85%; height: 85%; object-fit: contain; mix-blend-mode: multiply; }
		.peek-img.sticker img { mix-blend-mode: normal; }
	}
	.send { margin-top: 1.5rem; }
	form { display: grid; gap: 1rem; }
	form h2 { margin-bottom: 0; }
	.sheet :global(label:not(.choice)) { font-size: 0.95rem; font-weight: 700; color: var(--dark); }
	.sheet :global(label :is(input, select, textarea)) { font-weight: 400; }
	button:not(.secondary):not(.link), .button { justify-self: start; padding: 0.8rem 1.4rem; font-size: 1rem; }
	button.link { justify-self: start; font-size: 0.95rem; color: var(--dark); }
	button.done { justify-self: start; padding: 0.7rem 1.2rem; font-size: 1rem; }
	/* The folded address: a mailing label with an Edit link. */
	.ship-to { display: flex; justify-content: space-between; align-items: flex-start; gap: 1rem; padding: 1rem 1.25rem; background: var(--tile); }
	.ship-to address { display: grid; margin-top: 0.25rem; font-style: normal; }
	/* Square, bordered options; the picked one takes the red. */
	.choice {
		display: flex; gap: 0.75rem; align-items: flex-start; padding: 0.8rem 1rem; border: 1.5px solid var(--smoke); cursor: pointer;
		font-size: 1rem; color: var(--dark); line-height: 1.4;
	}
	.choice:hover { border-color: var(--dark); }
	.choice.on { border-color: var(--red); box-shadow: inset 0 0 0 1px var(--red); }
	.choice input { width: auto; margin: 0.2rem 0 0; accent-color: var(--red); }
	.understand .p { display: block; margin-bottom: 0.6rem; }
	.understand .p:last-child { margin-bottom: 0; font-weight: 700; }
</style>
