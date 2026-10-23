<script lang="ts">
	// Staff getting postcards or posters made: pick the product, design to its specs (bleed template),
	// pick the art (checked and shown in 3D right away), see the cost and timeline, then send it.
	// Files upload untouched on Submit (POST /api/print/files), then the request (POST /api/print-requests).
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, fileToBase64 } from './api';
	import { session } from './session.svelte';
	import { track } from './track';
	import Product3D from './Product3D.svelte';
	import type { Preview3D } from './items';
	import {
		checkArt, estimateLine, guides, neededByNote, printArtProps, quantityPresets, readyText, sheetFor, templateSVG, trimCrop,
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
	let title = $state('');
	let quantity = $state(1000);
	let neededBy = $state('');
	let note = $state('');
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
		// Art for a postcard isn't the shape of a poster: check it again.
		for (const side of ['front', 'back'] as const) {
			const a = art[side];
			if (a) art[side] = { ...a, check: checkArt(p, a.width, a.height) };
		}
	}

	// Re-estimate as the product or quantity changes.
	$effect(() => {
		if (!product) return;
		const q = quantity;
		const k = product.key;
		const timer = setTimeout(async () => {
			try {
				estimate = await api.get<PrintEstimate>(`/api/print/estimate?product=${k}&quantity=${q}`);
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
		if (!/^image\/(png|jpeg)$/.test(file.type)) {
			reading[side] = 'Pick a PNG or JPEG. (Export a PDF as a 300 DPI PNG.)';
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
	}

	function download(landscape: boolean) {
		if (!product) return;
		const url = URL.createObjectURL(new Blob([templateSVG(product, landscape)], { type: 'image/svg+xml' }));
		const a = Object.assign(document.createElement('a'), { href: url, download: `${product.key}-${landscape ? 'landscape' : 'portrait'}-template.svg` });
		a.click();
		setTimeout(() => URL.revokeObjectURL(url), 1000);
	}

	const front = $derived(art.front);
	const sheet = $derived(product && front ? sheetFor(product, front.width, front.height) : null);
	// What the 3D view shows: one card (turn it over for the back) or the sheet, next to a phone for scale.
	const preview = $derived.by((): Preview3D | null => {
		if (!product || !front || !sheet || front.check.problem) return null;
		const back = art.back && !art.back.check.problem ? art.back.trimmed : undefined;
		const opts = { front: front.trimmed, back, widthIn: sheet.widthIn, heightIn: sheet.heightIn, finish: product.finish, phone: true };
		return product.postcard
			? { postcards: { kind: 'postcards', ...opts, thicknessIn: product.thickness_in } }
			: { sheet: { ...opts, thicknessIn: product.thickness_in } };
	});
	const problems = $derived((['front', 'back'] as const).filter((s) => art[s]?.check.problem || reading[s]));
	const late = $derived(estimate ? neededByNote(neededBy, estimate) : '');

	async function upload(side: Side, a: Art): Promise<string> {
		submitting = `Uploading the ${side}…`;
		const f = await api.post<{ url: string }>('/api/print/files', { product: key, filename: a.file.name, data: await fileToBase64(a.file) });
		return f.url;
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (!front || problems.length) return;
		error = '';
		try {
			const front_url = await upload('front', front);
			const back_url = art.back ? await upload('back', art.back) : '';
			submitting = 'Sending…';
			await api.post<PrintRequest>('/api/print-requests', { product: key, title, quantity, front_url, back_url, note, needed_by: neededBy });
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
	<p>
		Send us print-ready art and we'll order it from Vistaprint, the same way HQ prints its own postcards and posters. It ships to our
		warehouse, and once it's in, we list it in the shop so you, and anyone you send there, can request it.
	</p>

	{#if product}
		<fieldset class="ask">
			<legend>What should we print?</legend>
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
				<tr><td>Art size, with bleed</td><td>{product.bleed_width_in}" x {product.bleed_height_in}" · <strong>{product.bleed_width_px} x {product.bleed_height_px} px</strong> at {product.dpi} DPI</td></tr>
				<tr><td>Bleed</td><td>⅛" past the trim on every side. Run backgrounds and photos to the edge of the art; this strip is cut off.</td></tr>
				<tr><td>Safe area</td><td>{product.safe_width_in}" x {product.safe_height_in}". Keep text and logos ⅛" inside the trim.</td></tr>
				<tr><td>Files</td><td>PNG or JPEG, one per side, portrait or landscape.{product.postcard ? ' Postcards print on both sides; a blank back is fine.' : ' The back is optional.'}</td></tr>
				<tr><td>Printed on</td><td>{product.paper}. {product.vendor}.</td></tr>
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
			<div class="sides">
				{#each ['front', 'back'] as const as side (side)}
					{@const a = art[side]}
					<div class="side">
						<label>{side === 'front' ? 'Front' : 'Back (optional)'}
							<input type="file" accept="image/png,image/jpeg" required={side === 'front'} name="print_{side}" onchange={(e) => pickFile(side, e)} />
						</label>
						{#if a}
							{@const g = guides(product, a.width, a.height)}
							<div class="flat" style="aspect-ratio: {a.width} / {a.height}">
								<img src={a.url} alt="{side} art" />
								<span class="guide trim" style="inset: {g.trim.y}% {g.trim.x}%"></span>
								<span class="guide safe" style="inset: {g.safe.y}% {g.safe.x}%"></span>
							</div>
							<p class="fact">{a.width} x {a.height} px{a.check.dpi ? ` · ${a.check.dpi} DPI` : ''}</p>
							{#if a.check.problem}<p class="error">{a.check.problem}</p>
							{:else if a.check.warning}<p class="warn">{a.check.warning}</p>
							{:else}<p class="ok">Ready to print: full bleed at {a.check.dpi} DPI.</p>{/if}
							{#if side === 'back'}<button type="button" class="link" onclick={() => clearSide('back')}>No back</button>{/if}
						{/if}
						{#if reading[side]}<p class="error">{reading[side]}</p>{/if}
					</div>
				{/each}
			</div>
			{#if art.front || art.back}<p class="muted legend"><span class="key trim"></span> trim <span class="key safe"></span> safe area: keep text inside</p>{/if}

			{#if preview}
				<div class="preview">
					{#key preview}<Product3D {preview} name={title || product.name} count={1} />{/key}
				</div>
			{/if}

			<h3>How many?</h3>
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
					<p>{readyText(estimate)}. Then it ships to you like anything else in the shop, usually the next weekday.</p>
					<p class="muted">Estimated from Hack Club's own Vistaprint orders, 2025–26.</p>
				{:else if estimateError}
					<p class="error">{estimateError}</p>
				{/if}
			</div>

			<label>Name it <input required maxlength="200" bind:value={title} placeholder="e.g. Haven postcard" /></label>
			<label>Need them by (optional) <input type="date" bind:value={neededBy} /></label>
			{#if late}<p class="warn">{late}</p>{/if}
			<label>How will you use them? Which event or program, about how many people, and anything else we should know.
				<textarea required rows="3" maxlength="5000" bind:value={note} placeholder="e.g. Handing them out at 40 Haven events in November, about 25 each."></textarea></label>

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
	.guide { position: absolute; pointer-events: none; }
	.guide.trim, .key.trim { border: 1.5px solid var(--red); }
	.guide.safe, .key.safe { border: 1.5px dashed #338eda; }
	.key { display: inline-block; width: 1rem; height: 0.7rem; margin: 0 0.25rem 0 0.75rem; vertical-align: middle; }
	.legend { font-size: 0.85rem; margin-top: 0.5rem; }
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
