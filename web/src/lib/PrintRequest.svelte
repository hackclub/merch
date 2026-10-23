<script lang="ts">
	// Staff getting postcards or posters made: pick the product, design to its specs (bleed template),
	// pick the art (checked and shown in 3D right away), see the cost and timeline, then send it.
	// Files upload untouched on Submit (POST /api/print/files), then the request (POST /api/print-requests).
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, fileToBase64, isInternational } from './api';
	import { blankAddress, prefillAddress } from './address';
	import { applyPlace } from './addressSearch';
	import AddressLine1 from './AddressLine1.svelte';
	import type { Address } from './types';
	import { session } from './session.svelte';
	import { track } from './track';
	import Product3D from './Product3D.svelte';
	import type { Preview3D } from './items';
	import {
		arrivalLine, checkArt, estimateLine, firstShipmentOptions, guides, printArtProps, quantityPresets, sheetFor, templateSVG, timelineRows, trimCrop,
		type ArtCheck, type PrintEstimate, type PrintProduct, type PrintRequest
	} from './print';

	type Side = 'front' | 'back';
	// A picked file: its size, how it fits, the whole art for the flat view, and the trimmed art for 3D.
	type Art = { file: File; width: number; height: number; check: ArtCheck; url: string; trimmed: string };

	let products = $state<PrintProduct[]>([]);
	let key = $state('postcard_4x6');
	const product = $derived(products.find((p) => p.key === key));
	let art = $state<Record<Side, Art | null>>({ front: null, back: null });
	let reading = $state<Record<Side, string>>({ front: '', back: '' }); // why a file couldn't be read
	// The same art on both sides unless they upload a different back.
	let backMode = $state<'same' | 'different'>('same');
	let title = $state(''); // what the item is called in the warehouse
	let program = $state('');
	let deadline = $state('');
	let quantity = $state(1000);
	let note = $state('');
	// The first ones sent to them once the run reaches the warehouse, and where.
	let initial = $state(25);
	let address = $state(prefillAddress(blankAddress(), session.me?.user?.address));
	// Each side shows the art with its bleed, trim and safe zones, or as it'll look trimmed.
	let asPrinted = $state<Record<Side, boolean>>({ front: false, back: false });
	let estimate = $state<PrintEstimate | null>(null);
	let estimateError = $state('');
	let submitting = $state('');
	let error = $state('');
	const signedIn = $derived(!!session.me?.user);

	onMount(async () => {
		products = (await api.get<{ products: PrintProduct[] }>('/api/print/products')).products;
	});

	function pickProduct(k: string) {
		key = k;
		const p = products.find((x) => x.key === k)!;
		quantity = quantityPresets(p)[2];
		initial = p.postcard ? 25 : 5;
		// Art for a postcard isn't the shape of a poster: check it again.
		for (const side of ['front', 'back'] as const) {
			const a = art[side];
			if (a) art[side] = { ...a, check: checkArt(p, a.width, a.height) };
		}
	}

	// Re-estimate as the product, quantity or first shipment changes: the last leg is to their country.
	$effect(() => {
		if (!product) return;
		const q = quantity;
		const k = product.key;
		const first = initial > 0 ? `&first=${initial}&country=${encodeURIComponent(address.country.trim() || 'US')}` : '';
		const timer = setTimeout(async () => {
			try {
				estimate = await api.get<PrintEstimate>(`/api/print/estimate?product=${k}&quantity=${q}${first}`);
				estimateError = '';
			} catch (e) {
				estimate = null;
				estimateError = e instanceof ApiError ? e.message : '';
			}
		}, 250);
		return () => clearTimeout(timer);
	});

	// The art with its bleed cut off, at most 2048 px across, for the 3D view's texture.
	async function trim(bitmap: ImageBitmap, p: PrintProduct): Promise<string> {
		const c = trimCrop(p, bitmap.width, bitmap.height);
		const scale = Math.min(1, 2048 / Math.max(c.sw, c.sh));
		const canvas = document.createElement('canvas');
		canvas.width = Math.round(c.sw * scale);
		canvas.height = Math.round(c.sh * scale);
		canvas.getContext('2d')!.drawImage(bitmap, c.sx, c.sy, c.sw, c.sh, 0, 0, canvas.width, canvas.height);
		const blob = await new Promise<Blob | null>((r) => canvas.toBlob(r, 'image/jpeg', 0.9));
		return blob ? URL.createObjectURL(blob) : '';
	}

	async function pickFile(side: Side, e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		reading[side] = '';
		const old = art[side];
		if (old) (URL.revokeObjectURL(old.url), URL.revokeObjectURL(old.trimmed));
		art[side] = null;
		if (!file || !product) return;
		if (file.type !== 'image/png') {
			reading[side] = 'Pick a PNG. (Export your art as a PNG at 300 DPI or higher.)';
			track('print_art', printArtProps(product.key, side, null, file.size, file.type));
			input.value = '';
			return;
		}
		try {
			const bitmap = await createImageBitmap(file);
			const check = checkArt(product, bitmap.width, bitmap.height);
			art[side] = { file, width: bitmap.width, height: bitmap.height, check, url: URL.createObjectURL(file), trimmed: await trim(bitmap, product) };
			bitmap.close();
			track('print_art', printArtProps(product.key, side, check, file.size));
		} catch {
			reading[side] = "That file couldn't be read as an image.";
		}
	}

	function clearSide(side: Side) {
		const a = art[side];
		if (a) (URL.revokeObjectURL(a.url), URL.revokeObjectURL(a.trimmed));
		art[side] = null;
		reading[side] = '';
	}

	function pickBack(mode: 'same' | 'different') {
		backMode = mode;
		if (mode === 'same') clearSide('back');
	}

	const pickPlace = (place: Address) => (address = applyPlace(address, place));

	function download(landscape: boolean) {
		if (!product) return;
		const url = URL.createObjectURL(new Blob([templateSVG(product, landscape)], { type: 'image/svg+xml' }));
		const a = Object.assign(document.createElement('a'), { href: url, download: `${product.key}-${landscape ? 'landscape' : 'portrait'}-template.svg` });
		a.click();
		setTimeout(() => URL.revokeObjectURL(url), 1000);
	}

	const front = $derived(art.front);
	const sides = $derived<Side[]>(backMode === 'same' ? ['front'] : ['front', 'back']);
	const sheet = $derived(product && front ? sheetFor(product, front.width, front.height) : null);
	// What the 3D view shows: one card (turn it over for the back) or the sheet, next to a phone for scale.
	const preview = $derived.by((): Preview3D | null => {
		if (!product || !front || !sheet || front.check.problem) return null;
		const back = backMode === 'same' ? front.trimmed : art.back && !art.back.check.problem ? art.back.trimmed : undefined;
		const opts = { front: front.trimmed, back, widthIn: sheet.widthIn, heightIn: sheet.heightIn, finish: product.finish, phone: true };
		return product.postcard
			? { postcards: { kind: 'postcards', ...opts, thicknessIn: product.thickness_in } }
			: { sheet: { ...opts, thicknessIn: product.thickness_in } };
	});
	// A smaller run, or a switch to posters, can leave the first shipment bigger than what's offered: take the nearest below.
	$effect(() => {
		if (!product) return;
		const options = firstShipmentOptions(product, quantity);
		if (!options.includes(initial)) initial = options.filter((n) => n <= initial).at(-1) ?? 0;
	});
	const problems = $derived((['front', 'back'] as const).filter((s) => art[s]?.check.problem || reading[s]));

	async function upload(side: Side, a: Art): Promise<{ url: string; preview_url: string }> {
		submitting = `Uploading the ${side}…`;
		return api.post('/api/print/files', { product: key, filename: a.file.name, data: await fileToBase64(a.file) });
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (!front || problems.length) return;
		error = '';
		try {
			const f = await upload('front', front);
			const b = backMode === 'same' ? f : await upload('back', art.back!);
			submitting = 'Sending…';
			await api.post<PrintRequest>('/api/print-requests', {
				product: key, title, program, deadline, quantity, front_url: f.url, back_url: b.url, front_preview_url: f.preview_url, back_preview_url: b.preview_url,
				landscape: front.width > front.height, note,
				initial_quantity: initial, address: initial > 0 ? { ...address, country: address.country.trim().toUpperCase() } : null
			});
			goto('/requests?printed=1');
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Something went wrong.';
		} finally {
			submitting = '';
		}
	}
</script>

<section class="print" data-track="print">
	<h2>Get postcards or posters made</h2>

	{#if product}
		<fieldset class="ask">
			<legend>What do you want printed and stocked in the warehouse?</legend>
			{#each products as p (p.key)}
				<label class="choice" class:on={key === p.key}>
					<input type="radio" name="print_product" value={p.key} checked={key === p.key} onchange={() => pickProduct(p.key)} />
					<span><strong>{p.name}</strong><br /><span class="muted">{p.paper}</span></span>
				</label>
			{/each}
		</fieldset>

		<h3>Print specs</h3>
		<table class="specs">
			<tbody>
				<tr><td>Finished size (trim)</td><td>{product.width_in}" x {product.height_in}"</td></tr>
				<tr><td>Art size, with bleed</td><td>{product.bleed_width_in}" x {product.bleed_height_in}" at {product.dpi} DPI (or higher res is OK too)</td></tr>
				<tr><td>Bleed</td><td>⅛" past the trim on every side. Run backgrounds and photos to the edge of the art; this strip is cut off.</td></tr>
				<tr><td>Safe area</td><td>{product.safe_width_in}" x {product.safe_height_in}". Keep text and logos ⅛" inside the trim.</td></tr>
				<tr><td>Files</td><td>PNG only, portrait or landscape. We print the same art on both sides, or you can upload a different back.</td></tr>
				<tr><td>Printed on</td><td>{product.paper}.</td></tr>
			</tbody>
		</table>
		<p class="templates">
			Template with the bleed, trim and safe lines:
			<button type="button" class="link" data-track="print_template" onclick={() => download(false)}>portrait</button> ·
			<button type="button" class="link" data-track="print_template" onclick={() => download(true)}>landscape</button>
			<span class="muted">(SVG, opens in Figma, Illustrator and Canva)</span>
		</p>

		<form onsubmit={submit}>
			<h3>Your art</h3>
			<fieldset class="back-mode">
				<legend>Back</legend>
				<label class="inline"><input type="radio" name="print_back" value="same" checked={backMode === 'same'} onchange={() => pickBack('same')} /> Same art on both sides</label>
				<label class="inline"><input type="radio" name="print_back" value="different" checked={backMode === 'different'} onchange={() => pickBack('different')} /> A different back</label>
			</fieldset>
			<div class="sides">
				{#each sides as side (side)}
					{@const a = art[side]}
					<div class="side">
						<label>{side === 'back' ? 'Back' : backMode === 'same' ? 'Front and back' : 'Front'}
							<input type="file" accept="image/png" required name="print_{side}" onchange={(e) => pickFile(side, e)} />
						</label>
						{#if a}
							{@const g = guides(product, a.width, a.height)}
							<div class="views" role="group" aria-label="{side} preview">
								<button type="button" class="link" class:on={!asPrinted[side]} data-track="print_guides" onclick={() => (asPrinted[side] = false)}>Bleed and safe area</button>
								<button type="button" class="link" class:on={asPrinted[side]} data-track="print_as_printed" onclick={() => (asPrinted[side] = true)}>As printed</button>
							</div>
							{#if asPrinted[side]}
								{@const sheetSize = sheetFor(product, a.width, a.height)}
								<div class="flat printed" style="aspect-ratio: {sheetSize.widthIn} / {sheetSize.heightIn}">
									<img src={a.trimmed} alt="{side} as printed, trimmed" />
								</div>
							{:else}
								<!-- The bleed (striped, cut off), the trim line, the band inside it the cut can drift into, and the safe area. -->
								<div class="flat" style="aspect-ratio: {a.width} / {a.height}">
									<img src={a.url} alt="{side} art with its bleed, trim and safe area marked" />
									<span class="zone-trim" style="inset: {g.trim.y}% {g.trim.x}%">
										<span class="zone-safe" style="inset: {g.safeInTrim.y}% {g.safeInTrim.x}%"><span class="tag">Safe area</span></span>
									</span>
								</div>
							{/if}
							<p class="fact">{a.width} x {a.height} px{a.check.dpi ? ` · ${a.check.dpi} DPI` : ''}</p>
							{#if a.check.problem}<p class="error">{a.check.problem}</p>
							{:else if a.check.warning}<p class="warn">{a.check.warning}</p>
							{:else}<p class="ok">Ready to print: full bleed at {a.check.dpi} DPI.</p>{/if}
						{/if}
						{#if reading[side]}<p class="error">{reading[side]}</p>{/if}
					</div>
				{/each}
			</div>
			{#if art.front || art.back}
				<ul class="zones" data-track="print_zones">
					<li><span class="key bleed"></span><span><strong>Bleed: cut off.</strong> The outer ⅛". Run your background and photos into it, but put nothing you need there.</span></li>
					<li><span class="key cut"></span><span><strong>Trim line.</strong> Where it's cut to {product.width_in}" x {product.height_in}".</span></li>
					<li><span class="key unsafe"></span><span><strong>Not safe.</strong> The ⅛" inside the trim. Cuts can drift this far, so text and logos here may be cut off.</span></li>
					<li><span class="key safe"></span><span><strong>Safe area.</strong> Everything important (text, logos, QR codes, faces) goes inside the dashed line.</span></li>
				</ul>
			{/if}

			{#if preview}
				<div class="preview">
					{#key preview}<Product3D {preview} name={title || product.name} count={1} />{/key}
				</div>
			{/if}

			<h3>How many should we print?</h3>
			<p>
				We'll stock them at our warehouse and list them on the merch request form, where anyone can request them to help market your program.
				You can request more yourself any time: pick “I'm Hack Club staff” on this site.
			</p>
			<div class="presets">
				{#each quantityPresets(product) as q (q)}
					<button type="button" class="secondary" class:on={quantity === q} onclick={() => (quantity = q)}>{q.toLocaleString('en-US')}</button>
				{/each}
			</div>
			<label>Quantity <input type="number" min={product.min_quantity} max={product.max_quantity} step={product.step} required bind:value={quantity} /></label>
			<p class="muted">{product.postcard ? 'Postcards come banded in 50s' : 'Posters come in 25s'}, {product.min_quantity.toLocaleString('en-US')} to {product.max_quantity.toLocaleString('en-US')}.</p>

			<div class="estimate" data-track="print_estimate">
				{#if estimate}
					<p><strong>{estimateLine(estimate)}</strong></p>
					<table class="specs">
						<tbody>
							<tr><td>Printing</td><td>${(estimate.print_cents / 100).toFixed(2)}</td></tr>
							<tr><td>Express shipping to our warehouse</td><td>${(estimate.shipping_cents / 100).toFixed(2)}</td></tr>
						</tbody>
					</table>
					<table class="specs timeline">
						<tbody>
							{#each timelineRows(estimate) as [step, time] (step)}<tr><td>{step}</td><td>{time}</td></tr>{/each}
						</tbody>
					</table>
					<p><strong>{arrivalLine(estimate)}</strong></p>
					<p class="muted">Estimated from Hack Club's own Vistaprint orders, 2025–26, and how our packages have shipped.</p>
				{:else if estimateError}
					<p class="error">{estimateError}</p>
				{/if}
			</div>

			<h3>How many should we send you first?</h3>
			<p>When the print run reaches our warehouse, we'll send you this many so you can see them in person. It goes out as a request on your account, like any other.</p>
			<label>First shipment
				<select name="initial_quantity" bind:value={initial}>
					{#each firstShipmentOptions(product, quantity) as n (n)}<option value={n}>{n ? n.toLocaleString('en-US') : 'None, thanks'}</option>{/each}
				</select></label>
			{#if initial > 0}
				<div class="form-grid address" data-private>
					<label>First name <input required bind:value={address.first_name} autocomplete="given-name" /></label>
					<label>Last name <input required bind:value={address.last_name} autocomplete="family-name" /></label>
				</div>
				<div data-private>
					<AddressLine1 bind:value={address.line_1} onpick={pickPlace} placeholder="Start typing the street address…"
						hint="Pick it from the suggestions and we’ll fill in the rest." />
				</div>
				<div class="form-grid address" data-private>
					<label>Address line 2 (optional) <input bind:value={address.line_2} autocomplete="address-line2" /></label>
					<label>City <input required bind:value={address.city} autocomplete="address-level2" /></label>
					<label>State / province <input bind:value={address.state} autocomplete="address-level1" /></label>
					<label>ZIP / postal code <input required bind:value={address.postal_code} autocomplete="postal-code" /></label>
					<label>Country code (US, CA, GB…) <input required maxlength="2" bind:value={address.country} autocomplete="country" /></label>
					<label>Phone {isInternational(address.country) ? '(required for customs)' : '(optional)'}
						<input required={isInternational(address.country)} bind:value={address.phone_number} autocomplete="tel" /></label>
				</div>
				{#if isInternational(address.country)}<p class="muted">Outside the US, you'll pay this shipment's postage by card on My requests once it's ready, like any international request.</p>{/if}
			{/if}

			<label>What's the name of the program this is for? <input required maxlength="200" name="print_program" bind:value={program} placeholder="Ex. Haven" /></label>
			<label>What should we call this item in the warehouse? <input required maxlength="200" name="print_title" bind:value={title} placeholder="Ex. Haven postcard" /></label>
			<label>How will you use them?
				<span class="muted hint">More detail is more helpful here. Oftentimes people don't print enough, so we'll use our experience to print the right amount.</span>
				<textarea required rows="4" maxlength="5000" bind:value={note} placeholder="Ex. Handing them out at 40 Haven events in November, about 25 each."></textarea></label>
			<label>What is the final deadline for your program?
				<span class="muted hint">Ex. If it's an in-person event, when is it happening? Give the date and any details around it.</span>
				<textarea required rows="2" maxlength="2000" bind:value={deadline}></textarea></label>

			<div class="submit">
				{#if error}<p class="error" role="alert">{error}</p>{/if}
				{#if !signedIn}
					<p class="muted">Sign in with Hack Club to send it. Your art uploads when you send, so sign in first, then pick your files.</p>
					<a class="button" href="/auth/login?for=print">Sign in with Hack Club</a>
				{:else}
					<button disabled={!!submitting || !front || problems.length > 0 || !estimate}>
						{submitting || `Send for printing${estimate ? ` (${quantity.toLocaleString('en-US')})` : ''}`}
					</button>
				{/if}
			</div>
		</form>
	{:else}
		<p class="muted">Loading…</p>
	{/if}
</section>

<style>
	.print p { margin: 0 0 0.75rem; }
	h2 { margin: 2rem 0 1rem; padding-top: 1.5rem; border-top: 1px solid var(--smoke); font-size: 1.25rem; line-height: 1.3; }
	h3 { margin: 1.75rem 0 0.5rem; font-size: 1rem; line-height: 1.3; }
	.ask { margin: 1.5rem 0 0; padding: 0; border: 0; display: grid; gap: 0.5rem; }
	.ask legend { float: left; width: 100%; margin-bottom: 0.5rem; padding: 0; font-weight: 700; font-size: 1.1rem; }
	.choice {
		display: flex; gap: 0.75rem; align-items: flex-start; padding: 0.8rem 1rem; border: 1.5px solid var(--smoke); cursor: pointer;
		font-size: 1rem; color: var(--dark); line-height: 1.4;
	}
	.choice:hover { border-color: var(--dark); }
	.choice.on { border-color: var(--red); box-shadow: inset 0 0 0 1px var(--red); }
	.choice input { width: auto; margin: 0.2rem 0 0; accent-color: var(--red); }
	.specs { width: 100%; border-collapse: collapse; font-size: 0.95rem; }
	.specs td { padding: 0.35rem 0.5rem 0.35rem 0; border-bottom: 1px solid var(--smoke); vertical-align: top; }
	.specs td:first-child { width: 11rem; color: var(--muted); }
	.templates { margin-top: 0.75rem; }
	.sides { display: grid; gap: 1rem; grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr)); }
	.side { display: grid; gap: 0.4rem; align-content: start; }
	.side p { margin: 0; font-size: 0.9rem; }
	.fact { font-family: var(--mono); font-size: 0.8rem; text-transform: uppercase; color: var(--muted); }
	/* The art as uploaded, with the trim (solid) and safe area (dashed) drawn over it. */
	.flat { position: relative; max-height: 20rem; background: var(--tile); }
	.flat img { display: block; width: 100%; height: 100%; object-fit: contain; }
	.flat { overflow: hidden; }
	/* Everything outside the trim box is bleed: red. Inside it, everything outside the safe box is amber. */
	.zone-trim {
		position: absolute; overflow: hidden; pointer-events: none; outline: 1.5px solid var(--red);
		box-shadow: 0 0 0 100vmax rgba(236, 55, 80, 0.55);
	}
	.zone-safe { position: absolute; border: 1.5px dashed #338eda; box-shadow: 0 0 0 100vmax rgba(255, 166, 0, 0.42); }
	.tag {
		position: absolute; top: 0.2rem; left: 0.2rem; padding: 0.05rem 0.3rem; background: #338eda; color: white;
		font-family: var(--mono); font-size: 0.6rem; text-transform: uppercase; letter-spacing: 0.02em;
	}
	.views { display: flex; gap: 0.75rem; }
	.views .on { color: var(--dark); text-decoration: none; font-weight: 700; }
	.zones { list-style: none; margin: 0.75rem 0 0; padding: 0; display: grid; gap: 0.4rem; font-size: 0.9rem; }
	.zones li { display: flex; gap: 0.6rem; align-items: flex-start; }
	.key { flex: none; width: 1.4rem; height: 0.9rem; margin-top: 0.25rem; }
	.key.bleed { background: rgba(236, 55, 80, 0.55); }
	.key.cut { border-top: 2px solid var(--red); height: 0; margin-top: 0.7rem; }
	.key.unsafe { background: rgba(255, 166, 0, 0.42); }
	.key.safe { border: 1.5px dashed #338eda; }
	.address { margin-top: 0.75rem; }
	.back-mode { margin: 0 0 0.75rem; padding: 0; border: 0; display: flex; flex-wrap: wrap; gap: 0.5rem 1.25rem; }
	.back-mode legend { padding: 0; margin-bottom: 0.25rem; font-weight: 700; }
	form .back-mode label.inline { display: flex; gap: 0.4rem; align-items: center; margin-top: 0; }
	.back-mode input { width: auto; margin: 0; accent-color: var(--red); }
	.estimate .timeline td:first-child { width: auto; }
	form label .hint { font-weight: 400; }
	.address label { margin-top: 0; }
	.preview { margin: 1rem 0; }
	.warn { color: #a15c00; font-weight: 700; }
	.presets { display: flex; flex-wrap: wrap; gap: 0.5rem; margin-bottom: 0.75rem; }
	.presets .on { border-color: var(--red); color: var(--red); }
	.estimate { margin: 1rem 0; padding: 1rem 1.25rem; background: var(--tile); }
	.estimate .specs { margin-bottom: 0.75rem; }
	.estimate p:last-child { margin: 0; }
	form label { display: grid; gap: 0.25rem; margin-top: 0.75rem; }
	.submit { display: grid; gap: 0.75rem; margin-top: 1.25rem; }
	.submit p { margin: 0; }
</style>
