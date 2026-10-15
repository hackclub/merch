<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, formatCents, isInternational } from '$lib/api';
	import { addressComplete, addressSummary, blankAddress, prefillAddress } from '$lib/address';
	import AddressLine1 from '$lib/AddressLine1.svelte';
	import { applyPlace } from '$lib/addressSearch';
	import { bag, setCart, setPurpose, setSizes } from '$lib/bag.svelte';
	import { cartErrors, cartLines, emptyCart, pruneCart, pruneSizes, setQuantity, setSizeQuantity, sizeLabel, sizesSummary, totalQuantity } from '$lib/cart';
	import { galleryRows, roughFee } from '$lib/gallery';
	import { itemPath, previewCount, thumbnail, tilePreview } from '$lib/items';
	import { itemsFor, needsReason, PURPOSES } from '$lib/purpose';
	import { sectionize, tileFact } from '$lib/sections';
	import { session } from '$lib/session.svelte';
	import SizeSteppers from '$lib/SizeSteppers.svelte';
	import Haul3D from '$lib/Haul3D.svelte';
	import QuantityPicker from '$lib/QuantityPicker.svelte';
	import Tile3D from '$lib/Tile3D.svelte';
	import type { Address, Catalog, Item, Purpose, SwagRequest } from '$lib/types';

	let catalog = $state<Catalog | null>(null);
	// Prefilled from the person's Hack Club Auth address when they have one.
	const prefilled = prefillAddress(blankAddress(), session.me?.user?.address);
	let address = $state<Address>(prefilled);
	// A complete address folds into a "Ship to" summary. Editing shows the fields until they press "Use this address";
	// `manual` shows every field instead of just the autocomplete search.
	let editing = $state(!addressComplete(prefilled));
	let manual = $state(!!prefilled.line_1.trim());
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
	// Two strips of everything under the intro: photos and products on top, magazine pages and backs below.
	const rows = $derived(catalog ? galleryRows(catalog.items) : null);
	// What's coming, line by line (one per size), for the 3D preview above Submit.
	const haul = $derived(
		cartLines(cart, shown, sizes).map((l) => {
			const item = shown.find((i) => i.id === l.item_id)!;
			return { item, quantity: l.quantity, size: l.sku ? sizeLabel(item, l.sku) : undefined };
		})
	);
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
	// Items whose amounts are showing: "Add" opens them; removing the item closes them.
	let picking = $state<Record<number, boolean>>({});
	const expanded = (item: Item) => item.variants.length > 0 || !!picking[item.id] || !!cart[item.id];

	function setAmount(item: Item, n: number) {
		setCart(setQuantity(cart, item, n));
		if (n === 0) picking[item.id] = false;
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

<!-- One simple column, top to bottom, like the old Fillout request forms: intro, gallery, questions, shipping, submit. -->
<div class="sheet">
	<div class="intro">
		<p>Teenager and want to spread <a href="https://hackclub.com" target="_blank" rel="noreferrer">Hack Club</a> in your community?</p>
		<p>Get some awesome hand-designed swag made by your fellow Hack Clubbers!</p>
		<p>Free shipping in the US.{#if fee > 0}{' '}{roughFee(fee)} to help cover shipping globally.{/if}</p>
		<p>Shipped next day Mon–Fri. 2–5 day delivery in US. 2–4 week delivery globally.</p>
	</div>

	{#if rows && rows[0].length}
		<!-- Each row renders twice so it loops without a seam; the copy is hidden from screen readers. -->
		<div class="strips">
			{#each rows as row, r (r)}
				<div class="strip" class:reverse={r === 1}>
					<div class="track" style="--n: {row.length}">
						{#each [0, 1] as copy}
							{#each row as s, i (i)}
								<a href={s.href} class="slide" class:real={s.real} class:page={s.page} class:sticker={s.sticker}
									aria-hidden={copy === 1} tabindex={copy === 1 ? -1 : undefined}>
									<img src={s.src} alt={copy === 1 ? '' : s.alt} decoding="async" />
								</a>
							{/each}
						{/each}
					</div>
				</div>
			{/each}
		</div>
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
					<div class="line" class:picked={cart[item.id]} class:sized={open && expanded(item)}
						onmouseenter={() => (peek = item.id)} onmouseleave={() => (peek = null)}>
						<a href={itemPath(item)} class="thumb" class:sticker={item.packs === 'sticker'} tabindex="-1" aria-hidden="true">
							{#if thumbnail(item)}<img src={thumbnail(item)} alt="" loading="lazy" />{/if}
						</a>
						<div class="what">
							<a href={itemPath(item)}>{item.name}</a>
							<span class="muted">{tileFact(item)}{#if sizesSummary(item, sizes)}{tileFact(item) ? ' · ' : ''}<strong class="in">{sizesSummary(item, sizes)}</strong>{/if}</span>
						</div>
						{#if open}
							<div class="add">
								{#if item.variants.length}
									<SizeSteppers {item} {sizes} onchange={(sku, n) => setSize(item, sku, n)} />
								{:else if expanded(item)}
									<QuantityPicker {item} value={cart[item.id] ?? 0} onchange={(n) => setAmount(item, n)} />
								{:else}
									<button type="button" class="secondary" onclick={() => (picking[item.id] = true)}>Add</button>
								{/if}
							</div>
						{/if}
						{#if peek === item.id}
							{@const preview = tilePreview(item)}
							<div class="peek" aria-hidden="true">
								{#if thumbnail(item) || preview}
									<div class="peek-img" class:sticker={item.packs === 'sticker'}>
										{#if thumbnail(item)}<img src={thumbnail(item)} alt="" />{/if}
										{#if preview}<Tile3D {preview} active count={previewCount(item, cart[item.id] ?? 0)} rest={'stickers' in preview ? -0.25 : -0.45} />{/if}
									</div>
								{/if}
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
			{#if haul.length}<div class="send"><Haul3D lines={haul} table={purpose === 'club' || purpose === 'event'} /></div>{/if}
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
				<div class="understand">
					<p>Hack Club is a charity. The most effective way we can get more teenagers into Hack Club is to help people like you spread it!</p>
					<p>But it's wasteful when we send more than gets used. Ask for what you'll actually hand out, put up or wear. If you run out, come back and get more!</p>
					{#if shown.some((i) => i.variants.length && cart[i.id])}
						<p>T-shirts are for you to wear yourself, not to give away. If you need shirts for prizes or an event, get in touch with Zach.</p>
					{/if}
					<label class="choice" class:on={understood}>
						<input type="checkbox" required bind:checked={understood} />
						<span>I understand, and I'll only request what I'll use.</span>
					</label>
				</div>
				{#if haul.length}<Haul3D lines={haul} table={purpose === 'club' || purpose === 'event'} />{/if}
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
	/* Two strips edge to edge across the sheet, scrolling opposite ways; they pause under the pointer. */
	.strips { display: grid; gap: 0.5rem; margin: 1.5rem -2rem 0; overflow: hidden; }
	@media (max-width: 600px) { .strips { margin: 1.25rem -1.25rem 0; } }
	.strip { overflow: hidden; }
	.track { display: flex; gap: 0.5rem; width: max-content; animation: scroll calc(var(--n) * 5s) linear infinite; }
	.strip.reverse .track { animation-direction: reverse; }
	.strips:hover .track { animation-play-state: paused; }
	@keyframes scroll { to { transform: translateX(calc(-50% - 0.25rem)); } }
	@media (prefers-reduced-motion: reduce) {
		.strip { overflow-x: auto; }
		.track { animation: none; }
	}
	.slide { display: block; flex: none; height: 11rem; aspect-ratio: 4 / 5; background: var(--tile); }
	.slide img { display: block; width: 100%; height: 100%; object-fit: contain; padding: 8%; box-sizing: border-box; mix-blend-mode: multiply; }
	.slide.sticker img { mix-blend-mode: normal; }
	/* Photos and magazine pages show whole, at their own shape. */
	.slide:is(.real, .page) { aspect-ratio: auto; }
	.slide:is(.real, .page) img { width: auto; padding: 0; mix-blend-mode: normal; }
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
			/* Pinned beside the sheet, centred below the header, so it never runs off the screen. */
			display: flex; flex-direction: column; gap: 0.35rem; position: fixed; z-index: 2; top: calc(50% + 2rem); transform: translateY(-50%);
			left: calc(50% + 23.5rem); width: min(20rem, calc((100vw - 40rem) / 2 - 3.5rem)); max-height: calc(100vh - 6rem); overflow: hidden; padding: 1rem;
			background: white; box-shadow: 0 6px 24px rgba(0, 0, 0, 0.12); pointer-events: none; font-size: 0.9rem; line-height: 1.45;
		}
		.peek p { margin: 0; display: -webkit-box; -webkit-line-clamp: 6; line-clamp: 6; -webkit-box-orient: vertical; overflow: hidden; }
		.peek-img { position: relative; display: flex; align-items: center; justify-content: center; width: 100%; aspect-ratio: 4 / 3; margin-bottom: 0.4rem; background: var(--tile); }
		.peek-img img { width: 85%; height: 85%; object-fit: contain; mix-blend-mode: multiply; }
		.peek-img.sticker img { mix-blend-mode: normal; }
		/* Once the 3D view or clip is up it replaces the photo. */
		.peek-img:has(:global(.spin.ready)) > img { opacity: 0; }
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
	/* Read first, then tick: the checkbox sits under the text. */
	.understand { display: grid; gap: 0.6rem; padding: 1rem 1.25rem; background: var(--tile); }
	.understand p { margin: 0; }
	.understand .choice { background: white; }
</style>
