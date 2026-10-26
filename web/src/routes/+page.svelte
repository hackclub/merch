<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { MediaQuery } from 'svelte/reactivity';
	import { goto } from '$app/navigation';
	import { api, ApiError, formatCents, isInternational } from '$lib/api';
	import { addressComplete, addressSummary, blankAddress, prefillAddress } from '$lib/address';
	import AddressLine1 from '$lib/AddressLine1.svelte';
	import { backorderNote, shortNote, soldOut, stockErrors } from '$lib/backorder';
	import { applyPlace } from '$lib/addressSearch';
	import { bag, setCart, setNeed, setPurpose, setSizes } from '$lib/bag.svelte';
	import { cartErrors, cartLines, emptyCart, pruneCart, pruneSizes, setQuantity, setSizeQuantity, sizeLabel, sizesSummary, stickerNudge, totalQuantity } from '$lib/cart';
	import { feeTooltip, galleryRows, roughFee } from '$lib/gallery';
	import { pickedLines } from '$lib/haul';
	import { clearDraft, readDraft, resumeAddress, resumeStep, saveDraft, VERIFY_EMAIL_URL, verifyEmailProps } from '$lib/draft';
	import { cardSides, itemPath, previewCount, showsAmountIn3D, thumbnail, tilePreview } from '$lib/items';
	import { forGroups, itemsFor, needsReason, printing, PURPOSES, STAFF_NEEDS } from '$lib/purpose';
	import PrintRequest from '$lib/PrintRequest.svelte';
	import { sectionize, tileFact } from '$lib/sections';
	import { session } from '$lib/session.svelte';
	import { payShipping, type CardForm } from '$lib/payment';
	import CardField from '$lib/CardField.svelte';
	import { arrivalText, costRows, summaryText, costToYou, usd, worthText, type CostSides, type Packing, type ShippingEstimate, type ShippingSummary } from '$lib/shipping';
	import SizeSteppers from '$lib/SizeSteppers.svelte';
	import Haul3D from '$lib/Haul3D.svelte';
	import QuantityPicker from '$lib/QuantityPicker.svelte';
	import Tile3D from '$lib/Tile3D.svelte';
	import { blockerMessage, problemItemId, submitBlockedProps, submitBlockers, type SubmitBlocker } from '$lib/submit';
	import { thumb } from '$lib/thumb';
	import { track } from '$lib/track';
	import { heavyNote, tooHeavy, weightCrossing, weightLimitProps, weightMeter, type TooHeavy } from '$lib/weight';
	import type { Address, Catalog, Item, Purpose, SwagRequest } from '$lib/types';

	let catalog = $state<Catalog | null>(null);
	// Prefilled from the person's Hack Club Auth address when they have one.
	const prefilled = prefillAddress(blankAddress(), session.me?.user?.address);
	let address = $state<Address>(prefilled);
	// A complete address folds into a "Ship to" summary. Editing shows the fields until they press "Use this address";
	// `manual` shows every field instead of just the autocomplete search.
	let editing = $state(!addressComplete(prefilled));
	// Signed out, address search isn't offered (lookups cost money), so every field shows.
	let manual = $state(!!prefilled.line_1.trim() || !session.me?.user);
	let note = $state('');
	let understood = $state(false);
	let customsOK = $state(false); // outside the US: they'll pay their country's customs fees
	let error = $state('');
	// Why Submit didn't go through, shown next to the button.
	let submitError = $state('');
	let submitting = $state(false);
	let card = $state<CardForm | null>(null);
	let form = $state<HTMLFormElement | null>(null);
	// Where a blocked Submit points: the boxes it can name, and the error lines by the button.
	let understoodBox = $state<HTMLInputElement | null>(null);
	let customsBox = $state<HTMLInputElement | null>(null);
	let submitNote = $state<HTMLDivElement | null>(null);
	// The first reason the last Submit tap didn't go through, while it still applies.
	let blockedReason = $state<SubmitBlocker | null>(null);
	// Back from verifying their email abroad: the card is all that's left.
	let verified = $state(false);
	// Delivery times from past shipments, and this request's packing and arrival estimate.
	let summary = $state<ShippingSummary | null>(null);
	// `costs`: what it costs Hack Club to send, in the US and abroad.
	let quote = $state<{ estimate: ShippingEstimate; packing: Packing; costs?: CostSides } | null>(null);
	let quoteError = $state('');
	// The server's 3 lb refusal, with its weights, for the meter over the list.
	let heavy = $state<TooHeavy | null>(null);

	onMount(async () => {
		api.get<ShippingSummary>('/api/shipping').then((s) => (summary = s), () => {});
		try {
			catalog = await api.get<Catalog>('/api/catalog');
			const listed = itemsFor(catalog.items, bag.purpose);
			setSizes(pruneSizes(bag.sizes, listed));
			setCart(pruneCart(bag.cart, listed, bag.sizes));
			if (await resume()) return;
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
	// Staff can get postcards or posters made instead: the print flow takes the place of the swag list.
	const print = $derived(printing(purpose, bag.need));
	// Personal requests don't offer club-only items (the full-size magazine, the T-shirt).
	const shown = $derived(catalog ? itemsFor(catalog.items, purpose) : []);
	const sections = $derived(sectionize(shown));
	// Anyone can fill in the whole request; signing in only comes at Submit, to verify their email.
	const open = $derived(!!catalog?.settings.requests_open);
	const signedIn = $derived(!!session.me?.user);
	const count = $derived(totalQuantity(cart));
	const problems = $derived(catalog ? [...cartErrors(cart, catalog.items, sizes), ...stockErrors(cart, catalog.items, sizes, catalog.stock)] : []);
	const intl = $derived(isInternational(address.country));
	// Clubs and events asking for a few each of many sticker designs: suggest packs of 50 (never blocks).
	const nudge = $derived(catalog ? stickerNudge(cart, shown, purpose) : null);
	let nudged = false;
	// They've read the note: we may send more of fewer designs. Submit waits for it while the note shows.
	let regroupOK = $state(false);
	// Why Submit can't go yet. The button stays enabled so tapping it can say so (and log submit_blocked).
	const blockers = $derived(submitBlockers({ understood, customsOK, intl, nudge: !!nudge, regroupOK, problems, quoteError }));
	// Only the two boxes have no error line of their own by the button; the rest are already spelled out there.
	const blockedText = $derived(
		blockedReason && blockers.includes(blockedReason) && (blockedReason === 'understood' || blockedReason === 'customs')
			? blockerMessage(blockedReason, problems, quoteError)
			: ''
	);
	$effect(() => {
		if (nudge && !nudged) track('sticker_nudge', { designs: nudge.designs, loose: nudge.loose });
		nudged = !!nudge;
	});
	// Abroad, the packed weight against the 3 lb limit, while they pick (the server weighs it).
	const meter = $derived(intl && count > 0 ? weightMeter(quote?.packing, heavy) : null);
	let wasOver = false;
	$effect(() => {
		const c = weightCrossing(wasOver, meter);
		if (c.fire && meter) track('weight_limit', weightLimitProps(meter, address.country));
		wasOver = c.over;
	});
	// International shipping is paid by card right on the form (once they've verified their email).
	const payByCard = $derived(intl && !!quote?.estimate.fee_cents);
	const cardNow = $derived(payByCard && signedIn);
	// Two strips of everything under the intro: photos and products on top, magazine pages and backs below.
	const rows = $derived(catalog ? galleryRows(catalog.items) : null);
	// Image boxes in CSS pixels, so thumbnails come at the size they show (see .slide and .thumb below):
	// slides are 11rem tall, products 4:5 with 8% padding; list thumbnails 85% of 3.75rem wide, grid ones of about 7rem
	// (by width only: portrait ones grow taller than the square);
	// the hover peek 85% of an 18rem-wide 4:3 box.
	const SLIDE = { h: 176 };
	const SLIDE_PRODUCT = { w: 120, h: 154 };
	const LINE_THUMB = { w: 52 };
	const CELL_THUMB = { w: 96 };
	const PEEK = { w: 245, h: 184 };
	// What's coming, line by line (one per size), for the 3D preview: beside the sheet on wide screens, above Submit otherwise.
	const haul = $derived(
		cartLines(cart, shown, sizes).map((l) => {
			const item = shown.find((i) => i.id === l.item_id)!;
			return { item, quantity: l.quantity, size: l.sku ? sizeLabel(item, l.sku) : undefined };
		})
	);
	// Wide enough for the haul to sit in the grey space left of the sheet, so it grows as people pick.
	const side = new MediaQuery('min-width: 74rem');
	// The side haul can grow to fill the window for a closer look.
	let bigHaul = $state(false);
	const fee = $derived(catalog?.settings.international_shipping_fee_cents ?? 0);

	// Re-quote shipping as the bag or country changes. The server refuses what won't pack together
	// (a poster with a stack of postcards) or is over 3 lb abroad, so this also catches those before Submit.
	$effect(() => {
		if (!catalog || !open || count === 0) return;
		const body = { country: address.country.trim().toUpperCase(), lines: cartLines(cart, catalog.items, sizes) };
		const timer = setTimeout(async () => {
			try {
				quote = await api.post('/api/shipping/estimate', body);
				quoteError = '';
				heavy = null;
			} catch (e) {
				quote = null;
				quoteError = e instanceof ApiError && e.status === 422 ? e.message : '';
				heavy = e instanceof ApiError && e.status === 422 ? tooHeavy(e.data) : null;
			}
		}, 300);
		return () => clearTimeout(timer);
	});

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

	// Empty the whole request (asks first, since it can't be undone).
	function clearAll() {
		if (!confirm('Clear everything from your request?')) return;
		setCart(emptyCart());
		setSizes({});
		picking = {};
	}

	function setAmount(item: Item, n: number) {
		setCart(setQuantity(cart, item, n));
		if (n === 0) picking[item.id] = false;
	}

	// The form as they left it to verify their email. Back signed in, US requests send themselves;
	// abroad the card field waits. Returns whether it picked one up.
	async function resume(): Promise<boolean> {
		const d = readDraft(sessionStorage);
		if (!d) return false;
		address = resumeAddress(d.address, session.me?.user?.address);
		note = d.note;
		understood = d.understood;
		customsOK = d.customs_ok;
		regroupOK = d.regroup_ok;
		editing = !addressComplete(address);
		manual = true;
		if (!signedIn) return false; // they backed out of signing in: the form is still filled in
		clearDraft(sessionStorage);
		const next = resumeStep(isInternational(address.country));
		track('verify_email_back', { next, items: count });
		await tick();
		form?.scrollIntoView({ behavior: 'smooth' });
		if (next === 'card') verified = true;
		else if (count > 0 && !blockers.length) form?.requestSubmit();
		return true;
	}

	// Point at the first thing keeping Submit from going: its box, the sticker note, the cart line that's
	// wrong, or the error lines by the button. (Empty address/note fields never get here: the browser
	// blocks those itself and logs field_invalid, so nothing is reported twice.)
	async function focusBlocker(b: SubmitBlocker) {
		await tick();
		let el: HTMLElement | null = null;
		if (b === 'understood') el = understoodBox;
		else if (b === 'customs') el = customsBox;
		else if (b === 'sticker_regroup') el = document.querySelector('#sticker-note input[type="checkbox"]');
		else if (b === 'cart_problem') {
			const id = catalog ? problemItemId(problems, catalog.items) : null;
			const row = id ? document.querySelector(`[data-item="${id}"]`) : null;
			el = row?.querySelector<HTMLElement>('input, select, button') ?? (row instanceof HTMLElement ? row : submitNote);
		} else el = submitNote;
		el?.scrollIntoView({ behavior: 'smooth', block: 'center' });
		el?.focus?.({ preventScroll: true });
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (!catalog) return;
		if (blockers.length) {
			blockedReason = blockers[0];
			track('submit_blocked', submitBlockedProps('request', blockers, { intl, items: count, signedIn }));
			void focusBlocker(blockers[0]);
			return;
		}
		blockedReason = null;
		if (!signedIn) {
			const d = { address, note: needsReason(purpose) ? note : '', understood, customs_ok: customsOK, regroup_ok: regroupOK };
			saveDraft(sessionStorage, d);
			track('verify_email', verifyEmailProps(d, { items: count, intl, purpose }));
			location.href = VERIFY_EMAIL_URL;
			return;
		}
		submitError = '';
		submitting = true;
		try {
			// Check the card before the request exists, so a typo doesn't leave an unpaid request.
			const cardProblem = payByCard && card ? await card.check() : null;
			if (cardProblem) {
				submitError = cardProblem;
				return;
			}
			const r = await api.post<SwagRequest>('/api/requests', {
				purpose,
				address: { ...address, country: address.country.trim().toUpperCase() },
				lines: cartLines(cart, catalog.items, sizes),
				note: needsReason(purpose) ? note : '',
				customs_ok: intl && customsOK
			});
			// Charge the card before emptying the bag: the card field goes away with it.
			const failed = r.stripe_key && card ? await payShipping(card, r.id, (path) => api.post(path), window.location.origin) : null;
			setCart(emptyCart());
			setSizes({});
			if (failed !== null) {
				// Paid: My requests thanks them. Not paid: the request waits there, with the card field to try again.
				goto(failed ? `/requests?unpaid=${r.id}` : `/requests?paid=${r.id}`, { state: { payError: failed } });
			} else goto('/requests');
		} catch (e) {
			submitError = e instanceof ApiError ? e.message : 'Something went wrong.';
		} finally {
			submitting = false;
		}
	}
</script>

<svelte:head><title>Hack Club Merch Request</title></svelte:head>

<!-- One simple column, top to bottom, like the old Fillout request forms: intro, gallery, questions, shipping, submit. -->
<div class="sheet">
	<div class="intro">
		<p>Teenager and want to spread <a href="https://hackclub.com" target="_blank" rel="noreferrer">Hack Club</a> in your community?</p>
		<p>Get some awesome hand-designed swag made by your fellow Hack Clubbers!</p>
		<p>Free shipping in the US.{#if fee > 0}{' '}Globally you cover the postage, usually <span class="fee" title={feeTooltip}>{roughFee(summary?.international.fee_cents || fee)}</span>.{/if}</p>
		<p>Shipped next day Mon–Fri. {summaryText(summary) || '2–5 day delivery in the US. 2–4 weeks globally.'}</p>
	</div>

	{#if rows && rows[0].length}
		<!-- Each row renders twice so it loops without a seam; the copy is hidden from screen readers. -->
		<div class="strips" data-track="strips">
			{#each rows as row, r (r)}
				<div class="strip" class:reverse={r === 1}>
					<div class="track" style="--n: {row.length}">
						{#each [0, 1] as copy}
							{#each row as s, i (i)}
								<a href={s.href} class="slide" class:real={s.real} class:page={s.page} class:sticker={s.sticker}
									aria-hidden={copy === 1} tabindex={copy === 1 ? -1 : undefined}>
									<img {...thumb(s.src, s.real || s.page ? SLIDE : SLIDE_PRODUCT)} alt={copy === 1 ? '' : s.alt} decoding="async" />
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
		{#if open}
			<fieldset class="ask purposes" id="purpose">
				<legend>What kind of request is this?</legend>
				{#each PURPOSES as p, i (p.value)}
					<label class="choice" class:on={purpose === p.value} class:beside={p.beside} class:paired={PURPOSES[i + 1]?.beside}>
						<input type="radio" name="purpose" value={p.value} checked={purpose === p.value} onchange={() => choose(p.value)} />
						{p.label}
					</label>
				{/each}
			</fieldset>
			{#if purpose === 'staff'}
				<fieldset class="ask need" id="staff-need">
					<legend>What do you need?</legend>
					{#each STAFF_NEEDS as n (n.value)}
						<label class="choice" class:on={bag.need === n.value}>
							<input type="radio" name="staff_need" value={n.value} checked={bag.need === n.value} onchange={() => setNeed(n.value)} />
							{n.label}
						</label>
					{/each}
				</fieldset>
			{/if}
		{/if}

		{#snippet add(item: Item)}
			<div class="add">
				{#if item.variants.length}
					<SizeSteppers {item} {sizes} onchange={(sku, n) => setSize(item, sku, n)} />
				{:else if soldOut(item, catalog?.stock) && !cart[item.id]}
					<!-- Nothing to add: the note under its name says it's out of stock. -->
				{:else if expanded(item)}
					<QuantityPicker {item} value={cart[item.id] ?? 0} onchange={(n) => setAmount(item, n)} />
				{:else}
					<button type="button" class="secondary" onclick={() => (picking[item.id] = true)}>Add</button>
				{/if}
			</div>
		{/snippet}

		{#snippet peekPanel(item: Item)}
			{@const preview = tilePreview(item)}
			{@const sides = cardSides(item)}
			<div class="peek" aria-hidden="true">
				{#if sides && preview}
					<!-- Cards show both sides in 3D: the stack as it comes, then turned over. -->
					{#each sides as src, i (i)}
						<div class="peek-img side" class:portrait={(item.height_in ?? 0) > (item.width_in ?? 0)}>
							<img {...thumb(src, PEEK)} alt="" />
							<Tile3D {preview} active back={i === 1} count={previewCount(item, cart[item.id] ?? 0)} rest={-0.45} />
							<span class="side-label">{i ? 'Back' : 'Front'}</span>
						</div>
					{/each}
				{:else if thumbnail(item) || preview}
					<div class="peek-img" class:sticker={item.packs === 'sticker'}>
						{#if thumbnail(item)}<img {...thumb(thumbnail(item), PEEK)} alt="" />{/if}
						{#if preview}<Tile3D {preview} active count={previewCount(item, cart[item.id] ?? 0)} rest={'stickers' in preview ? -0.25 : -0.45} />{/if}
					</div>
				{/if}
				{#if showsAmountIn3D(item)}
					<div class="peek-3d"><Haul3D lines={pickedLines(item, cart[item.id] ?? 0)} includes={catalog?.includes} compact /></div>
				{/if}
				<strong>{item.name}</strong>
				<span class="muted">{tileFact(item)}</span>
				{#if item.description}<p>{item.description}</p>{/if}
				<span class="muted">Click for photos{item.width_in ? ', the 3D view' : ''} and more →</span>
			</div>
		{/snippet}

		{#if print}
			<PrintRequest />
		{:else}
		<h2>What would you like us to send you?</h2>
		{#if open && meter}
			<div class="weight" class:over={meter.over} data-track="weight_meter" role="status">
				<div class="bar"><span style="width: {meter.pct}%"></span></div>
				<p>{meter.text}</p>
				{#if meter.heaviestText}<p>{meter.heaviestText}</p>{/if}
			</div>
		{/if}
		{#each sections as { section, rows, grid }, n (section.key)}
			<!-- What we most want people to hand out comes first; everything else is an extra. -->
			{#if section.extra && !sections[n - 1]?.section.extra}
				<div class="extras">
					<h3>Extras</h3>
					<p class="muted">Also available, if you have a use for them.</p>
				</div>
			{/if}
			<section class="group" aria-labelledby="group-{section.key}">
				<h3 id="group-{section.key}">{section.title}</h3>
				{#if section.blurb}<p class="blurb">{section.blurb}</p>{/if}
				{#if section.key === 'stickers' && forGroups(purpose) && !nudge}
					<p class="blurb">For clubs and events: pick a few favorite designs in packs of 50. They're much easier for our team to ship than a few each of many designs.</p>
				{/if}
				{#each rows as { item } (item.id)}
					<!-- svelte-ignore a11y_no_static_element_interactions -->
					<div class="line" data-item={item.id} class:picked={cart[item.id]} class:sized={open && expanded(item)}
						onmouseenter={() => (peek = item.id)} onmouseleave={() => (peek = null)}>
						<a href={itemPath(item)} class="thumb" class:sticker={item.packs === 'sticker'} tabindex="-1" aria-hidden="true">
							{#if thumbnail(item)}<img {...thumb(thumbnail(item), LINE_THUMB)} alt="" loading="lazy" />{/if}
						</a>
						<div class="what">
							<a href={itemPath(item)}>{item.name}</a>
							<span class="muted">{tileFact(item)}{#if sizesSummary(item, sizes)}{tileFact(item) ? ' · ' : ''}<strong class="in">{sizesSummary(item, sizes)}</strong>{/if}</span>
							{#if backorderNote(item, catalog.stock, cart, sizes)}<span class="backorder" data-track="backorder">{backorderNote(item, catalog.stock, cart, sizes)}</span>{/if}
							{#if shortNote(item, catalog.stock, cart, sizes)}<span class="backorder" data-track="out_of_stock">{shortNote(item, catalog.stock, cart, sizes)}</span>{/if}
							{#if heavyNote(meter, item.id)}<span class="backorder" data-track="weight_heavy">{heavyNote(meter, item.id)}</span>{/if}
						</div>
						{#if open}{@render add(item)}{/if}
						{#if open && expanded(item) && showsAmountIn3D(item)}
							<div class="amount-3d"><Haul3D lines={pickedLines(item, cart[item.id] ?? 0)} includes={catalog.includes} compact /></div>
						{/if}
						{#if peek === item.id}{@render peekPanel(item)}{/if}
					</div>
				{/each}
				{#if grid.length}
					<div class="cells">
						{#each grid as { item } (item.id)}
							<!-- svelte-ignore a11y_no_static_element_interactions -->
							<div class="cell" data-item={item.id} class:picked={cart[item.id]} onmouseenter={() => (peek = item.id)} onmouseleave={() => (peek = null)}>
								<a href={itemPath(item)} class="thumb" class:sticker={item.packs === 'sticker'} tabindex="-1" aria-hidden="true">
									{#if thumbnail(item)}<img {...thumb(thumbnail(item), CELL_THUMB)} alt="" loading="lazy" />{/if}
								</a>
								<a href={itemPath(item)} class="name">{item.name}</a>
								{#if backorderNote(item, catalog.stock, cart, sizes)}<span class="backorder" data-track="backorder">{backorderNote(item, catalog.stock, cart, sizes)}</span>{/if}
							{#if shortNote(item, catalog.stock, cart, sizes)}<span class="backorder" data-track="out_of_stock">{shortNote(item, catalog.stock, cart, sizes)}</span>{/if}
								{#if heavyNote(meter, item.id)}<span class="backorder" data-track="weight_heavy">{heavyNote(meter, item.id)}</span>{/if}
								{#if open}{@render add(item)}{/if}
								{#if peek === item.id}{@render peekPanel(item)}{/if}
							</div>
						{/each}
					</div>
				{/if}
				<!-- Under the stickers, once they've picked a few each of many designs. Submit waits for the box. -->
				{#if section.key === 'stickers' && nudge}
					<div class="nudge" id="sticker-note" data-track="sticker_nudge" role="status">
						<p>{nudge.text}</p>
						<label class="choice" class:on={regroupOK}>
							<input type="checkbox" name="sticker_regroup_ok" bind:checked={regroupOK} />
							<span>I understand.</span>
						</label>
					</div>
				{/if}
			</section>
		{:else}
			<p class="muted">Nothing listed yet.</p>
		{/each}

		{#if open && !purpose}
			<p class="muted">Pick what kind of request this is at the top, then we'll ask where to send it.</p>
		{:else if open && totalQuantity(cart) > 0}
			<form id="request" bind:this={form} onsubmit={submit} data-track="form">
				<h2>Shipping info</h2>
				{#if !editing}
					<div class="ship-to">
						<div>
							<span class="muted">Ship to</span>
							<address data-private>{#each addressSummary(address) as l}<span>{l}</span>{/each}</address>
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
					<label><span class="reason">{purposeInfo?.reason}</span>
						<textarea bind:value={note} rows="3" required placeholder={purposeInfo?.example}></textarea></label>
				{/if}
				{#if haul.length && !side.current}<Haul3D lines={haul} includes={catalog.includes} table={forGroups(purpose)} />
					<button type="button" class="link" onclick={clearAll}>Clear request</button>{/if}
				<div class="understand">
					<p>Hack Club is a charity. The most effective way we can get more teenagers into Hack Club is to help people like you spread it!</p>
					<p>But it's wasteful when we send more than gets used. Ask for what you'll actually hand out, put up or wear. If you run out, come back and get more!</p>
					{#if shown.some((i) => i.variants.length && cart[i.id])}
						<p>T-shirts are for you to wear yourself, not to give away. If you need shirts for prizes or an event, get in touch with Zach.</p>
					{/if}
					<p>Our goal for marketing is to spend no more than $50 USD per new teenager who joins Hack Club and ships 1 weighted technical project in one of our programs (1 weighted project = 10 approved hours).</p>
					<p>{(quote?.costs && worthText(intl ? quote.costs.international : quote.costs.us)) || 'If you get 1 teen to join Hack Club and ship a weighted project, then this is worth it!'}</p>
					{#if quote?.costs}
						<table class="costs">
							<thead><tr><th>Cost to Hack Club</th><th>{intl ? address.country.trim().toUpperCase() : 'US'}</th></tr></thead>
							<tbody>
								{#each costRows(quote.costs, intl) as [label, amount]}<tr><td>{label}</td><td>{amount}</td></tr>{/each}
							</tbody>
						</table>
						<p><strong>Cost to you: {costToYou(intl, quote.estimate.fee_cents)}</strong></p>
					{/if}
					<!-- Not `required`: submit() catches this box itself (submit_blocked), so the browser's field_invalid would double-report it. -->
					<label class="choice" class:on={understood}>
						<input type="checkbox" bind:this={understoodBox} bind:checked={understood} />
						<span>I understand, and I'll only request what I'll use.</span>
					</label>
				</div>
				{#if intl}
					<div class="pay">
						{#if verified}<p><strong>Email verified.</strong> Pay the postage by card to send your request.</p>{/if}
						{#if cardNow && quote}
							<CardField bind:this={card} stripeKey={catalog.settings.stripe_key} amountCents={quote.estimate.fee_cents} billing={address} />
						{/if}
						<label class="choice" class:on={customsOK}>
							<input type="checkbox" name="customs_ok" bind:this={customsBox} bind:checked={customsOK} />
							<span>I understand I'm responsible for any customs fees or import taxes my country charges to receive a package from the USA.</span>
						</label>
					</div>
				{/if}
				<div class="submit" tabindex="-1" bind:this={submitNote}>
					{#if quote && arrivalText(quote.estimate)}<p>{arrivalText(quote.estimate)}, by {quote.estimate.service}.</p>{/if}
					{#if nudge && !regroupOK}<p class="error"><a href="#sticker-note">Check “I understand.” under Stickers</a> to send this request.</p>{/if}
					{#each problems as p}<p class="error">{p}</p>{/each}
					{#if quoteError}<p class="error">{quoteError}</p>{/if}
					{#if blockedText}<p class="error" role="alert">{blockedText}</p>{/if}
					{#if submitError}<p class="error" role="alert">{submitError}</p>{/if}
					{#if !signedIn}<p class="muted">Next, sign in with Hack Club to verify your email{payByCard && quote ? `, then pay the ${usd(quote.estimate.fee_cents)} postage by card` : ''}.</p>{/if}
					<button disabled={submitting}>
						{!signedIn ? 'Verify email to submit' : submitting ? 'Sending…' : cardNow && quote ? `Submit and pay ${usd(quote.estimate.fee_cents)}` : `Submit request (${totalQuantity(cart)} item${totalQuantity(cart) === 1 ? '' : 's'})`}
					</button>
				</div>
			</form>
		{/if}
		{/if}
	{:else if !error}
		<p class="muted">Loading swag…</p>
	{/if}
	{#if catalog && count > 0 && haul.length && side.current && !print}
		<aside class="haul-side" class:big={bigHaul} aria-label="Your request so far">
			<div class="haul-head">
				<strong>Your request · {count} item{count === 1 ? '' : 's'}</strong>
				<span class="haul-actions">
					<button type="button" class="link" data-track={bigHaul ? 'haul-shrink' : 'haul-expand'} aria-expanded={bigHaul} onclick={() => (bigHaul = !bigHaul)}>{bigHaul ? 'Shrink' : 'Expand'}</button>
					<button type="button" class="link" onclick={clearAll}>Clear</button>
				</span>
			</div>
			<Haul3D lines={haul} includes={catalog.includes} table={forGroups(purpose)} />
		</aside>
	{/if}
</div>

<svelte:window onkeydown={(e) => e.key === 'Escape' && bigHaul && (bigHaul = false)} />

<style>
	.reason {
		white-space: pre-line;
	}
	.fee {
		text-decoration: underline dotted;
		cursor: help;
	}
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
	/* Most options take the whole row; a `beside` one sits on the right of the one before it. */
	.purposes { grid-template-columns: 1fr auto; }
	.purposes .choice:not(.paired, .beside) { grid-column: 1 / -1; }
	/* Staff's second question sits right under the first. */
	.ask.need { margin-top: 1rem; padding-top: 0; border-top: 0; }
	.group { margin-bottom: 1.5rem; }
	.group h3 { margin: 0 0 0.25rem; font-size: 1rem; line-height: 1.3; }
	.group .blurb { margin: 0 0 0.25rem; }
	.nudge { display: grid; gap: 0.6rem; margin: 0.75rem 0 0.25rem; padding: 0.75rem 1rem; background: var(--tile); border-left: 3px solid var(--red); font-size: 0.95rem; }
	.nudge p { margin: 0; }
	/* The line between the start section and the extras. */
	.extras { margin: 2.5rem 0 1rem; padding-top: 1.5rem; border-top: 1px solid var(--smoke); }
	.extras h3 { margin: 0; font-size: 1.1rem; line-height: 1.3; }
	.extras p { margin: 0.25rem 0 0; }
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
	.backorder { font-size: 0.9rem; color: var(--red); }
	/* Abroad: the packed weight against the 3 lb limit. */
	.weight { margin: 0 0 1rem; font-size: 0.95rem; }
	.weight p { margin: 0.25rem 0 0; }
	.weight .bar { height: 6px; background: var(--tile); }
	.weight .bar span { display: block; height: 100%; background: currentColor; opacity: 0.5; }
	.weight.over { color: var(--red); }
	.weight.over .bar span { opacity: 1; }
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
		.peek-3d :global(.controls) { display: none; }
		.peek-img.side { aspect-ratio: 16 / 10; overflow: hidden; margin-bottom: 0.1rem; }
		.peek-img.side.portrait { aspect-ratio: 4 / 3; }
		.side-label {
			position: absolute; z-index: 1; left: 0.4rem; bottom: 0.4rem; padding: 0.1rem 0.35rem; background: white;
			font: 0.65rem var(--mono); text-transform: uppercase; letter-spacing: 0.04em;
		}
		/* The hover panel shows the amount in 3D, so the row doesn't. */
		.amount-3d { display: none; }
		/* Once the 3D view or clip is up it replaces the photo. */
		.peek-img:has(:global(.spin.ready)) > img { opacity: 0; }
	}
	/* The haul so far, pinned to the bottom left in the grey space beside the sheet (the hover details take the right). */
	.haul-side {
		position: fixed; z-index: 1; bottom: 1.5rem; left: 1.5rem; width: min(24rem, calc((100vw - 40rem) / 2 - 3rem));
		max-height: calc(100vh - 7rem); overflow-y: auto; display: grid; gap: 0.6rem; padding: 1rem; background: white; font-size: 0.9rem; line-height: 1.45;
	}
	.haul-head { display: flex; justify-content: space-between; align-items: baseline; gap: 1rem; }
	.haul-head button.link { font-size: 0.9rem; }
	.haul-actions { display: flex; gap: 1rem; }
	/* Expanded: the haul fills the window, its view as tall as the window allows. */
	.haul-side.big {
		z-index: 3; top: 1.5rem; right: 1.5rem; width: auto; max-height: none; overflow: hidden; grid-template-rows: auto 1fr;
		box-shadow: 0 0 0 100vmax rgb(0 0 0 / 0.5);
	}
	.haul-side.big :global(.haul) { grid-template-rows: 1fr auto; min-height: 0; }
	.haul-side.big :global(.stage) { aspect-ratio: auto; min-height: 0; }
	.send button.link { margin-top: 0.5rem; }
	/* Picking how many of a booklet or an item that plays a clip: the amount, stacked up in 3D, under the row. */
	.amount-3d { flex-basis: 100%; padding-left: 4.65rem; }
	@media (max-width: 600px) { .amount-3d { padding-left: 0; } }
	/* The rest of a grid section (the other stickers) under its lead row: a compact grid of picture, name, add. */
	.cells { display: grid; grid-template-columns: repeat(auto-fill, minmax(6rem, 1fr)); gap: 1rem 0.6rem; padding-top: 0.75rem; }
	.cell { display: grid; gap: 0.35rem; align-content: start; font-size: 0.9rem; line-height: 1.3; }
	.cell .thumb { width: 100%; height: auto; aspect-ratio: 1; position: relative; }
	/* Out of flow, so a tall sticker fits the square instead of stretching it. */
	.cell .thumb img { position: absolute; inset: 7.5%; }
	.cell.picked .thumb { box-shadow: inset 0 0 0 2px var(--red); }
	.cell .name { font-weight: 700; text-decoration: none; }
	.cell .name:hover { text-decoration: underline; }
	.cell .add { justify-content: flex-start; padding-left: 0; flex-basis: auto; gap: 0.3rem; }
	.cell .add > button { height: 2rem; padding: 0 0.8rem; font-size: 0.9rem; }
	.cell .add :global(.amounts) { gap: 0.25rem; }
	.cell .add :global(.amounts button) { min-width: 2.2rem; height: 2rem; padding: 0 0.4rem; font-size: 0.85rem; }
	.cell .add :global(.custom) { width: 100%; height: 2rem; }
	.send { margin-top: 1.5rem; }
	form { display: grid; gap: 1.25rem; }
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
	.pay, .submit { display: grid; gap: 0.75rem; }
	.pay p, .submit p { margin: 0; }
	/* Admins only: the plain sheet's one font, no mono labels. */
	.costs th { font: inherit; text-transform: none; letter-spacing: 0; color: var(--dark); }
	.costs :is(th, td):not(:first-child) { text-align: right; white-space: nowrap; }
	.costs tr:last-child td { font-weight: 700; }
</style>
