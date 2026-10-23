<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, fileToBase64, formatCents, statusLabel } from '$lib/api';
	import { parseQuickPicks } from '$lib/cart';
	import { linkify } from '$lib/markdown';
	import { purposeLabel, PURPOSES } from '$lib/purpose';
	import { byFilename, itemPath, moveImage, printFileText } from '$lib/items';
	import InternalNote from '$lib/InternalNote.svelte';
	import SpendDashboard from '$lib/SpendDashboard.svelte';
	import { filterRequests, shippedBreakdown, statusCounts, type Dashboard, type Filter } from '$lib/dashboard';
	import { session } from '$lib/session.svelte';
	import { arrivalText, costBreakdown, ozText } from '$lib/shipping';
	import { lineText, shipBody } from '$lib/ship';
	import type { APIKey, Item, SentEmail, Settings, Submission, SwagRequest, WarehouseSKU } from '$lib/types';
	import { thumb } from '$lib/thumb';

	type Tab = 'requests' | 'items' | 'settings' | 'submissions' | 'emails' | 'api keys';
	let tab = $state<Tab>('requests');
	let error = $state('');
	let flash = $state('');

	let requests = $state<SwagRequest[]>([]);
	let filter = $state<Filter>({ status: 'open', q: '', region: '', source: '', purpose: '', sort: 'newest' });
	const PAGE = 100;
	let shown = $state(PAGE);
	const filtered = $derived(filterRequests(requests, filter));
	const counts = $derived(statusCounts(requests));
	let dashboard = $state<Dashboard | null>(null);
	let days = $state(30);
	let refreshing = $state(false);
	let items = $state<Item[]>([]);
	let skus = $state<WarehouseSKU[]>([]);
	let settings = $state<Settings | null>(null);
	let submissions = $state<Submission[]>([]);
	let apiKeys = $state<APIKey[]>([]);
	let emails = $state<SentEmail[]>([]);
	let emailStatus = $state('');
	let newKeyName = $state('');
	let newToken = $state(''); // shown once, right after the key is made

	async function guard(fn: () => Promise<unknown>, ok = '') {
		error = flash = '';
		try {
			await fn();
			flash = ok;
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		}
	}

	// Every request comes down once; the filters work in the browser.
	const loadRequests = () =>
		guard(async () => {
			requests = (await api.get<{ requests: SwagRequest[] }>('/api/admin/requests')).requests;
		});
	const loadDashboard = (d: number) =>
		guard(async () => {
			dashboard = await api.get<Dashboard>(`/api/admin/dashboard?days=${d}`);
		});
	$effect(() => {
		if (session.me?.roles.admin) loadDashboard(days);
	});
	async function pullCosts() {
		refreshing = true;
		await guard(async () => {
			const r = await api.post<{ checked: number; updated: number; mailed: number; failed: number; left: number }>('/api/admin/requests/refresh');
			await Promise.all([loadDashboard(days), loadRequests()]);
			flash = `Asked the warehouse about ${r.checked} shipped request${r.checked === 1 ? '' : 's'}: ${r.mailed} mailed with postage${r.failed ? `, ${r.failed} failed` : ''}${r.left ? `, ${r.left} left (pull again)` : ''}.`;
		});
		refreshing = false;
	}
	const loadItems = () =>
		guard(async () => {
			items = (await api.get<{ items: Item[] }>('/api/admin/items')).items;
		});
	const loadSKUs = () =>
		guard(async () => {
			skus = (await api.get<{ skus: WarehouseSKU[] }>('/api/admin/warehouse/skus')).skus;
		});
	const loadSettings = () =>
		guard(async () => {
			settings = await api.get<Settings>('/api/admin/settings');
		});
	const loadSubmissions = () =>
		guard(async () => {
			submissions = (await api.get<{ submissions: Submission[] }>('/api/admin/submissions')).submissions;
		});

	const loadEmails = () =>
		guard(async () => {
			const q = emailStatus ? `?status=${emailStatus}` : '';
			emails = (await api.get<{ emails: SentEmail[] }>(`/api/admin/emails${q}`)).emails;
		});

	const loadKeys = () =>
		guard(async () => {
			apiKeys = (await api.get<{ keys: APIKey[] }>('/api/admin/api-keys')).keys;
		});
	const makeKey = () =>
		guard(async () => {
			const res = await api.post<{ key: APIKey; token: string }>('/api/admin/api-keys', { name: newKeyName });
			newToken = res.token;
			newKeyName = '';
			await loadKeys();
		}, 'Key made. Copy it now: it won\'t be shown again.');
	const revokeKey = (k: APIKey) => {
		if (!confirm(`Revoke "${k.name}"? Agents using it stop working.`)) return;
		guard(async () => {
			await api.post(`/api/admin/api-keys/${k.id}/revoke`);
			await loadKeys();
		}, `Revoked ${k.name}`);
	};

	onMount(() => {
		if (!session.me?.roles.admin) return;
		loadRequests();
		loadItems();
		loadSettings();
		loadSubmissions();
		loadEmails();
		loadKeys();
	});

	const act = (id: number, action: string, body: object = {}) =>
		guard(async () => {
			await api.post(`/api/admin/requests/${id}/${action}`, body);
			await Promise.all([loadRequests(), loadDashboard(days)]);
		}, `Request #${id}: ${action} done`);

	// Before shipping, an admin can change quantities (0 = don't send) and write a note for the shipped email.
	let shipQty = $state<Record<number, Record<string, number>>>({});
	let shipNote = $state<Record<number, string>>({});
	const ship = (r: SwagRequest) =>
		guard(async () => {
			const res = await api.post<{ warning?: string }>(`/api/admin/requests/${r.id}/dispatch`, shipBody(r.lines, shipQty[r.id] ?? {}, shipNote[r.id] ?? ''));
			await Promise.all([loadRequests(), loadDashboard(days)]);
			if (res.warning) throw new Error(`Request #${r.id}: ${res.warning}`);
		}, `Request #${r.id}: shipped`);

	function reject(r: SwagRequest) {
		const refund = r.paid_at && r.shipping_fee_cents && r.stripe_payment_intent
			? `\n\nTheir ${formatCents(r.shipping_fee_cents)} card payment is refunded automatically.`
			: '';
		const note = prompt(`Reason (in the email to the requester; optional):${refund}`);
		if (note === null) return;
		act(r.id, 'reject', { note });
	}

	// ---- items ----
	const listedSKUs = $derived(new Set(items.flatMap((i) => [i.sku, ...i.variants.map((v) => v.sku)])));
	const saveItem = (it: Item) =>
		guard(async () => {
			await api.put(`/api/admin/items/${it.id}`, it);
			await loadItems();
		}, `Saved ${it.name}`);
	let uploading = $state<number | null>(null);
	// Uploads go straight to the CDN and onto the item; only the gallery is
	// refreshed, so unsaved edits to the item's other fields survive.
	async function uploadImages(it: Item, files: FileList | null) {
		if (!files?.length) return;
		uploading = it.id;
		await guard(async () => {
			for (const f of files) {
				const upd = await api.post<Item>(`/api/admin/items/${it.id}/images`, { filename: f.name, data: await fileToBase64(f) });
				it.images = upd.images;
			}
		}, `Uploaded ${files.length} image${files.length === 1 ? '' : 's'} to ${it.name}`);
		uploading = null;
	}
	// Booklet pages upload in filename order (page-2 before page-10), one at a time.
	async function uploadPages(it: Item, files: FileList | null) {
		if (!files?.length) return;
		uploading = it.id;
		await guard(async () => {
			for (const f of byFilename(files)) {
				const upd = await api.post<Item>(`/api/admin/items/${it.id}/pages`, { filename: f.name, data: await fileToBase64(f) });
				it.pages = upd.pages;
			}
		}, `Added ${files.length} page${files.length === 1 ? '' : 's'} to ${it.name}`);
		uploading = null;
	}
	async function uploadPhoto(it: Item, files: FileList | null) {
		const f = files?.[0];
		if (!f) return;
		const caption = prompt('Caption (optional):') ?? '';
		const credit = prompt('Who took it? (credit, e.g. their Slack name)') ?? '';
		uploading = it.id;
		await guard(async () => {
			const upd = await api.post<Item>(`/api/admin/items/${it.id}/photos`, { filename: f.name, data: await fileToBase64(f), caption, credit });
			it.photos = upd.photos;
		}, `Added a photo to ${it.name}`);
		uploading = null;
	}
	async function uploadPrintFile(it: Item, files: FileList | null) {
		const f = files?.[0];
		if (!f) return;
		const label = prompt('Label (e.g. Front, Back):') ?? '';
		uploading = it.id;
		await guard(async () => {
			const upd = await api.post<Item>(`/api/admin/items/${it.id}/print-files`, { filename: f.name, data: await fileToBase64(f), label });
			it.print_files = upd.print_files;
		}, `Added a print file to ${it.name}`);
		uploading = null;
	}
	const listSKU = (s: WarehouseSKU) =>
		guard(async () => {
			await api.post('/api/admin/items', { sku: s.sku, name: s.name, max_per_request: 1, visible: false });
			await loadItems();
		}, `Listed ${s.name} (hidden until you make it visible)`);

	const saveSettings = () =>
		guard(async () => {
			settings = await api.put<Settings>('/api/admin/settings', settings);
		}, 'Settings saved');

	const saveSubmission = (s: Submission) =>
		guard(async () => {
			await api.put(`/api/admin/submissions/${s.id}`, { status: s.status, admin_note: s.admin_note, sku: s.sku });
			await loadSubmissions();
		}, `Saved submission #${s.id}`);
</script>

<svelte:head><title>Admin · Hack Club Merch Request</title></svelte:head>

<h1>Admin</h1>
{#if !session.me?.roles.admin}
	<p class="error">Admins only.</p>
{:else}
	<div class="row" style="margin-bottom: 1rem">
		{#each ['requests', 'items', 'settings', 'submissions', 'emails', 'api keys'] as t}
			<button class:secondary={tab !== t} onclick={() => (tab = t as Tab)}>{t}</button>
		{/each}
	</div>
	{#if error}<p class="error">{error}</p>{/if}
	{#if flash}<p class="ok">{flash}</p>{/if}

	{#if tab === 'requests'}
		{#if dashboard}<SpendDashboard data={dashboard} bind:days onrefresh={pullCosts} {refreshing} />{/if}
		<div class="row statuses" data-track="request_status_filter">
			{#each [['open', 'Needs action'], ['pending', statusLabel.pending], ['awaiting_payment', 'Awaiting payment'], ['dispatched', statusLabel.dispatched], ['rejected', statusLabel.rejected], ['cancelled', statusLabel.cancelled], ['', 'All']] as [s, label]}
				<button class="chip" class:on={filter.status === s} onclick={() => { filter.status = s; shown = PAGE; }}>{label} <span class="n">{counts[s] ?? 0}</span></button>
			{/each}
		</div>
		<div class="filters" data-track="request_filters">
			<input type="search" placeholder="Search #id, email, name, city, item, SKU, tracking" bind:value={filter.q} oninput={() => (shown = PAGE)} />
			<select bind:value={filter.region} aria-label="Where to">
				<option value="">US + international</option><option value="us">US</option><option value="intl">International</option>
			</select>
			<select bind:value={filter.purpose} aria-label="Purpose">
				<option value="">Any purpose</option>
				{#each PURPOSES as p}<option value={p.value}>{p.short}</option>{/each}
			</select>
			<select bind:value={filter.source} aria-label="Source">
				<option value="">App + imported</option><option value="app">App</option><option value="imported">Imported (Fillout)</option>
			</select>
			<select bind:value={filter.sort} aria-label="Sort">
				<option value="newest">Newest first</option><option value="oldest">Oldest first</option>
			</select>
		</div>
		<p class="muted">{filtered.length} request{filtered.length === 1 ? '' : 's'}{filtered.length > shown ? `, showing ${shown}` : ''}</p>
		<table>
			<thead><tr><th>#</th><th>Who</th><th>Items</th><th>Ship to</th><th>Status</th><th></th></tr></thead>
			<tbody>
				{#each filtered.slice(0, shown) as r (r.id)}
					<tr>
						<td>{r.id}<div class="muted">{new Date(r.created_at).toLocaleDateString()}</div>
							{#if r.source !== 'app'}<span class="badge" title={r.airtable_record_id}>imported · {r.source}</span>{/if}</td>
						<td>{r.user_email}{#if r.purpose}<div><span class="badge">{purposeLabel(r.purpose)}</span></div>{/if}
							<InternalNote request={r} /></td>
						<td>
							{#if r.note}<blockquote class="wrote" data-private><span class="label">They wrote</span>{@html linkify(r.note)}</blockquote>{/if}
							{#if r.status === 'pending' && r.source === 'app'}
								{#each r.lines as l}
									<div class="row">
										<input type="number" min="0" style="width: 4.5rem" aria-label="Quantity to ship" value={shipQty[r.id]?.[l.sku] ?? l.quantity}
											oninput={(e) => (shipQty[r.id] = { ...shipQty[r.id], [l.sku]: Math.max(0, Math.floor(+e.currentTarget.value || 0)) })} />
										× {l.name}{l.variant ? ` (${l.variant})` : ''} <span class="muted">{l.sku}</span>
									</div>
								{/each}
								<textarea rows="2" placeholder="Note to the requester (optional; in the shipped email)" bind:value={shipNote[r.id]}></textarea>
							{:else}
								{#each r.lines as l}<div>{lineText(l)} <span class="muted">{l.sku}</span></div>{/each}
							{/if}
						</td>
						<td>{r.address.first_name} {r.address.last_name}<br />{r.address.city}, {r.address.state} {r.address.country}</td>
						<td>
							<span class="badge {r.status}">{statusLabel[r.status]}</span>
							{#if r.shipping_fee_cents}<div class="muted">fee {formatCents(r.shipping_fee_cents)}{r.paid_at ? ' (paid)' : ''}{r.refunded_cents ? `, ${formatCents(r.refunded_cents)} refunded` : ''}</div>{/if}
							{#if r.dispatched_at && r.source === 'app'}<div class="muted">shipped {new Date(r.dispatched_at).toLocaleDateString()}</div>{/if}
							{#if r.mailed_at}<div class="muted">mailed {new Date(r.mailed_at).toLocaleDateString()}</div>{/if}
							{#if r.tracking_number}<div class="muted">{r.carrier} {r.tracking_number}</div>{/if}
							{#if r.estimate}
								<div class="muted" title="From past shipments: {r.estimate.transit_samples} like it to {r.estimate.transit_basis}, postage from {r.estimate.postage_samples} to {r.estimate.postage_basis}">
									{r.estimate.package} · {ozText(r.estimate.weight_oz)} · {r.estimate.service}
									{#if r.estimate.postage_cents[1]}· postage {formatCents(r.estimate.postage_cents[0])}–{formatCents(r.estimate.postage_cents[1])}{/if}
									{#if arrivalText(r.estimate)}<br />{arrivalText(r.estimate)}{/if}
								</div>
							{/if}
							{#if dashboard?.costs[r.id]}
								{@const c = dashboard.costs[r.id]}
								<table class="cost" data-track="request_costs" title="What it cost Hack Club. Actual from mail.hackclub.com: {c.actual.join(', ') || 'nothing yet'}; the rest estimated">
									<tbody>
										{#each shippedBreakdown(c) as [label, amount]}<tr><td>{label}</td><td>{amount}</td></tr>{/each}
									</tbody>
								</table>
							{:else if r.costs}
								<table class="cost" data-track="request_costs" title="What it costs Hack Club: warehouse unit costs, AGH labor ($1.80 + $0.20 a SKU), the package, and typical postage of past packages like it">
									<tbody>
										{#each costBreakdown(r.costs) as [label, amount]}<tr><td>{label}</td><td>{amount}</td></tr>{/each}
									</tbody>
								</table>
							{/if}
						</td>
						<td class="row">
							{#if r.status === 'pending' && r.source === 'app'}<button onclick={() => ship(r)}>Ship</button>{/if}
							{#if r.status === 'pending' || r.status === 'awaiting_payment'}<button class="secondary" onclick={() => reject(r)}>Reject</button>{/if}
							{#if r.status === 'dispatched' && r.theseus_order_id}<button class="secondary" onclick={() => act(r.id, 'refresh')}>Refresh tracking</button>{/if}
						</td>
					</tr>
				{:else}
					<tr><td colspan="6" class="muted">No requests match.</td></tr>
				{/each}
			</tbody>
		</table>
		{#if filtered.length > shown}<button class="secondary" onclick={() => (shown += PAGE)}>Show {Math.min(PAGE, filtered.length - shown)} more</button>{/if}
	{:else if tab === 'items'}
		<h2>Listed items</h2>
		<table>
			<thead><tr><th>Item</th><th>Limits</th><th>Display</th><th></th></tr></thead>
			<tbody>
				{#each items as it (it.id)}
					<tr>
						<td>
							<input bind:value={it.name} /><div class="muted">{it.sku}{#if it.visible}&nbsp;· <a href={itemPath(it)}>view page</a>{/if}</div>
							<textarea bind:value={it.description} rows="4" placeholder="description (shown on the product page)"></textarea>
							<div class="gallery">
								{#each it.images as src, i (i + src)}
									<figure>
										<img {...thumb(src, { w: 80, h: 80 })} alt="" />
										<figcaption>
											<button class="link" title="Move left" disabled={i === 0} onclick={() => (it.images = moveImage(it.images, i, -1))}>←</button>
											<button class="link" title="Remove" onclick={() => (it.images = it.images.filter((_, j) => j !== i))}>✕</button>
											<button class="link" title="Move right" disabled={i === it.images.length - 1} onclick={() => (it.images = moveImage(it.images, i, 1))}>→</button>
										</figcaption>
									</figure>
								{/each}
								<label class="add-image" class:busy={uploading === it.id}>
									{uploading === it.id ? 'Uploading…' : '+ Image'}
									<input type="file" accept="image/png,image/jpeg,image/webp" multiple hidden disabled={uploading !== null}
										onchange={(e) => { uploadImages(it, e.currentTarget.files); e.currentTarget.value = ''; }} />
								</label>
							</div>
							<div class="muted">First image is the thumbnail (and the front in 3D; the second is the back). Reorder or remove, then Save.</div>
							<details open={it.variants.length > 0}>
								<summary class="muted">Sizes ({it.variants.length || 'one size'})</summary>
								{#each it.variants as v, i}
									<div class="row size-row">
										<input bind:value={v.label} placeholder="label (M)" style="width: 5rem" />
										<input bind:value={v.sku} placeholder="warehouse SKU" />
										<input type="number" min="0" step="0.01" title="This size's weight in oz (empty = the item's)" placeholder="oz" style="width: 4.5rem" value={v.weight_oz ?? ''}
											oninput={(e) => (v.weight_oz = e.currentTarget.value === '' ? null : +e.currentTarget.value)} />
										<button class="link" title="Remove size" onclick={() => (it.variants = it.variants.filter((_, j) => j !== i))}>✕</button>
									</div>
								{/each}
								<button class="link" onclick={() => (it.variants = [...it.variants, { label: '', sku: '' }])}>+ Size</button>
								<div class="muted">Each size ships its own SKU and can have its own weight (oz, without the mailer); limits count all sizes together. Saving folds items listed under these SKUs into this one.</div>
							</details>
							<details class="photos-edit" open={it.pages.length > 0}>
								<summary>Booklet pages ({it.pages.length})</summary>
								<label>Binding
									<select bind:value={it.binding}>
										<option value="">Not a booklet</option>
										<option value="staple">Stapled (saddle-stitched)</option>
										<option value="perfect">Square spine (perfect bound)</option>
										<option value="trifold">Trifold brochure (one sheet: images 1 and 2 are its outside and inside)</option>
									</select></label>
								{#if it.pages.length}
									<div class="pages">
										{#each it.pages.slice(0, 8) as src, i (i + src)}<img {...thumb(src, { h: 48 })} alt="Page {i + 1}" />{/each}
										{#if it.pages.length > 8}<span class="muted">+{it.pages.length - 8} more</span>{/if}
									</div>
								{/if}
								<div class="row">
									<label class="button small-btn" class:busy={uploading === it.id}>
										{uploading === it.id ? 'Uploading…' : '+ Pages'}
										<input type="file" accept="image/png,image/jpeg,image/webp" multiple hidden disabled={uploading !== null}
											onchange={(e) => { uploadPages(it, e.currentTarget.files); e.currentTarget.value = ''; }} />
									</label>
									{#if it.pages.length}<button class="link" onclick={() => (it.pages = [])}>Remove all pages</button>{/if}
								</div>
								<div class="muted">One image per page, front cover first and back cover last. With a binding and a printed size, the 3D view opens and flips through them.</div>
							</details>
							<details class="photos-edit" open={it.photos.length > 0}>
								<summary>Real-world photos ({it.photos.length})</summary>
								{#each it.photos as p, i (p.url)}
									<div class="photo-row">
										<img {...thumb(p.url, { w: 64, h: 48, fit: 'cover' })} alt="" />
										<div>
											<input bind:value={p.caption} placeholder="caption" aria-label="Photo caption" />
											<input bind:value={p.credit} placeholder="credit" aria-label="Photo credit" />
										</div>
										<button class="link" title="Remove photo" onclick={() => (it.photos = it.photos.filter((_, j) => j !== i))}>✕</button>
									</div>
								{/each}
								<label class="button small-btn" class:busy={uploading === it.id}>
									{uploading === it.id ? 'Uploading…' : '+ Photo'}
									<input type="file" accept="image/png,image/jpeg,image/webp" hidden disabled={uploading !== null}
										onchange={(e) => { uploadPhoto(it, e.currentTarget.files); e.currentTarget.value = ''; }} />
								</label>
							</details>
							<details class="photos-edit" open={it.print_files.length > 0}>
								<summary>Print files ({it.print_files.length})</summary>
								{#each it.print_files as f, i (f.url)}
									<div class="file-row">
										<input bind:value={f.label} placeholder="label, e.g. Front" aria-label="Print file label" />
										<a href={f.url} target="_blank" rel="noreferrer">{printFileText(f)}</a>
										<button class="link" title="Remove print file" onclick={() => (it.print_files = it.print_files.filter((_, j) => j !== i))}>✕</button>
									</div>
								{/each}
								<label class="button small-btn" class:busy={uploading === it.id}>
									{uploading === it.id ? 'Uploading…' : '+ Print file'}
									<input type="file" accept="application/pdf,image/png,image/jpeg,image/tiff" hidden disabled={uploading !== null}
										onchange={(e) => { uploadPrintFile(it, e.currentTarget.files); e.currentTarget.value = ''; }} />
								</label>
								<div class="muted">The original PDFs or full-size images, shown on the product page as "Print your own". Edit labels or remove, then Save.</div>
							</details>
						</td>
						<td>
							<label>Min per request <input type="number" min="1" bind:value={it.min_per_request} /></label>
							<label>Per request <input type="number" min="1" bind:value={it.max_per_request} /></label>
							<label>Quick picks (blank = automatic)
								<input value={it.quick_picks.join(', ')} placeholder="e.g. 1, 3, 5, 10" onchange={(e) => (it.quick_picks = parseQuickPicks(e.currentTarget.value))} /></label>
							<label>Packs
								<select bind:value={it.packs}>
									<option value="">Any quantity</option>
									<option value="fives">In 5s (5, 10, 15…)</option>
									<option value="postcard">Postcards: 5–25 by 5s, then 50s</option>
									<option value="sticker">Stickers: 3, 10, then 50s</option>
								</select></label>
							<label>Ships with (warehouse SKUs, one per unit)
								<input value={it.ships_with.join(', ')} placeholder="e.g. Pri/M25/4x6/1st"
									oninput={(e) => (it.ships_with = e.currentTarget.value.split(',').map((x) => x.trim()).filter(Boolean))} /></label>
							<label>Shop tile clip (looping MP4, GIF or photo URL)
								<input type="url" bind:value={it.clip} placeholder="https://cdn.hackclub.com/…/flip.mp4" /></label>
							<label>Per person, ever (blank = no cap)
								<input type="number" min="0" value={it.max_per_user ?? ''}
									oninput={(e) => (it.max_per_user = e.currentTarget.value === '' ? null : +e.currentTarget.value)} /></label>
						</td>
						<td>
							<label class="row"><input type="checkbox" style="width: auto" bind:checked={it.visible} /> Visible</label>
							<label class="row" title="Hidden from personal requests"><input type="checkbox" style="width: auto" bind:checked={it.club_only} /> Clubs &amp; events only</label>
							<label class="row" title="Offered only to requests from Hack Club staff"><input type="checkbox" style="width: auto" bind:checked={it.staff_only} /> Staff only</label>
							<label class="row" title="Requests can take more than the warehouse has; they ship once the restock arrives"><input type="checkbox" style="width: auto" bind:checked={it.allow_backorder} /> Can be backordered</label>
							<label>Sort order <input type="number" bind:value={it.sort_order} /></label>
							<label>Printed size, inches (for the 3D preview)
								<span class="row size">
									<input type="number" min="0" step="0.25" placeholder="W" aria-label="Width in inches" value={it.width_in ?? ''}
										oninput={(e) => (it.width_in = e.currentTarget.value === '' ? null : +e.currentTarget.value)} />
									×
									<input type="number" min="0" step="0.25" placeholder="H" aria-label="Height in inches" value={it.height_in ?? ''}
										oninput={(e) => (it.height_in = e.currentTarget.value === '' ? null : +e.currentTarget.value)} />
								</span>
							</label>
							<label>Finish
								<select bind:value={it.finish}>
									{#each [['', '—'], ['matte', 'Matte'], ['gloss', 'Gloss'], ['linen', 'Linen'], ['uncoated', 'Uncoated'], ['enamel', 'Enamel pin']] as [v, l]}
										<option value={v}>{l}</option>
									{/each}
								</select>
							</label>
							<label>Thickness, inches (e.g. 0.016 for 16 pt card)
								<input type="number" min="0" step="0.001" placeholder="0.008" value={it.thickness_in ?? ''}
									oninput={(e) => (it.thickness_in = e.currentTarget.value === '' ? null : +e.currentTarget.value)} />
							</label>
							<label>Shipping weight, ounces each (blank: worked out from the paper size)
								<input type="number" min="0" step="0.01" placeholder={it.width_in ? 'from size' : '6.5'} value={it.weight_oz ?? ''}
									oninput={(e) => (it.weight_oz = e.currentTarget.value === '' ? null : +e.currentTarget.value)} />
							</label>
						</td>
						<td><button onclick={() => saveItem(it)}>Save</button></td>
					</tr>
				{:else}
					<tr><td colspan="4" class="muted">No items listed. Add some from the warehouse below.</td></tr>
				{/each}
			</tbody>
		</table>
		<h2>Warehouse inventory (mail.hackclub.com)</h2>
		{#if skus.length === 0}
			<button class="secondary" onclick={loadSKUs}>Load warehouse SKUs</button>
		{:else}
			<table>
				<thead><tr><th>SKU</th><th>Name</th><th>In stock</th><th>Unit cost</th><th></th></tr></thead>
				<tbody>
					{#each skus as s (s.sku)}
						<tr>
							<td>{s.sku}</td><td>{s.name}</td><td>{s.in_stock}</td><td>${s.unit_cost}</td>
							<td>{#if listedSKUs.has(s.sku)}<span class="muted">listed</span>{:else}<button onclick={() => listSKU(s)}>List</button>{/if}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	{:else if tab === 'settings' && settings}
		<form class="card" onsubmit={(e) => { e.preventDefault(); saveSettings(); }}>
			<div class="form-grid">
				<label class="row"><input type="checkbox" style="width: auto" bind:checked={settings.requests_open} /> Requests open</label>
				<label>International shipping fee (cents) when there's no postage history; 0 = free international shipping <input type="number" min="0" bind:value={settings.international_shipping_fee_cents} /></label>
			</div>
			<button style="margin-top: 0.75rem">Save settings</button>
		</form>
	{:else if tab === 'emails'}
		<p>Every email the site sends requesters, word for word (newest 100).</p>
		<div class="row">
			<select bind:value={emailStatus} onchange={loadEmails} style="width: auto">
				<option value="">All</option>
				{#each ['sent', 'failed', 'unsent'] as s}<option value={s}>{s}</option>{/each}
			</select>
		</div>
		<table>
			<thead><tr><th>When</th><th>Request</th><th>To</th><th>Email</th><th>Status</th></tr></thead>
			<tbody>
				{#each emails as e (e.id)}
					<tr>
						<td>{new Date(e.at).toLocaleString()}</td>
						<td>{e.request_id ? `#${e.request_id}` : ''} <span class="muted">{e.kind}</span></td>
						<td data-private>{e.to}</td>
						<td data-private>
							<details>
								<summary><strong>{e.subject}</strong> <span class="muted">{e.preview}</span></summary>
								<pre class="email-body">{e.body}</pre>
							</details>
						</td>
						<td>{e.status}{#if e.error}<div class="muted">{e.error}</div>{/if}</td>
					</tr>
				{:else}
					<tr><td colspan="5" class="muted">No emails yet.</td></tr>
				{/each}
			</tbody>
		</table>
	{:else if tab === 'api keys'}
		<p>Agents use these keys to review requests: read them, add notes, ship and reject. A key acts for you, and
			only on the <a href="/api/agent" target="_blank" rel="noreferrer">agent API</a> (that page is the agent's instructions).</p>
		<form class="row" onsubmit={(e) => { e.preventDefault(); makeKey(); }}>
			<input bind:value={newKeyName} placeholder="Name, e.g. Review agent" style="width: auto" />
			<button>Make key</button>
		</form>
		{#if newToken}<p data-private><code>{newToken}</code></p>{/if}
		<table>
			<thead><tr><th>Name</th><th>Key</th><th>Made</th><th>Last used</th><th></th></tr></thead>
			<tbody>
				{#each apiKeys as k (k.id)}
					<tr class:muted={k.revoked_at}>
						<td>{k.name}</td>
						<td><code>{k.prefix}…</code></td>
						<td>{new Date(k.created_at).toLocaleDateString()} <span class="muted">by {k.created_by_email}</span></td>
						<td>{k.last_used_at ? new Date(k.last_used_at).toLocaleString() : 'never'}</td>
						<td>{#if k.revoked_at}Revoked {new Date(k.revoked_at).toLocaleDateString()}{:else}<button class="secondary" onclick={() => revokeKey(k)}>Revoke</button>{/if}</td>
					</tr>
				{:else}
					<tr><td colspan="5" class="muted">No keys yet.</td></tr>
				{/each}
			</tbody>
		</table>
	{:else if tab === 'submissions'}
		<table>
			<thead><tr><th>Author</th><th>Material</th><th>Qty</th><th>Status / SKU / note</th><th></th></tr></thead>
			<tbody>
				{#each submissions as s (s.id)}
					<tr>
						<td>{s.user_email}<div class="muted">{s.program}</div></td>
						<td><a href={s.file_url} target="_blank" rel="noreferrer">{s.title}</a> <span class="muted">({s.kind})</span>
							{#if s.description}<div class="muted">{s.description}</div>{/if}</td>
						<td>{s.quantity}</td>
						<td>
							<select bind:value={s.status}>
								{#each ['submitted', 'approved', 'printing', 'stocked', 'rejected'] as st}<option value={st}>{statusLabel[st]}</option>{/each}
							</select>
							<input bind:value={s.sku} placeholder="warehouse SKU once stocked" />
							<input bind:value={s.admin_note} placeholder="note to author" />
						</td>
						<td><button onclick={() => saveSubmission(s)}>Save</button></td>
					</tr>
				{:else}
					<tr><td colspan="5" class="muted">No submissions yet.</td></tr>
				{/each}
			</tbody>
		</table>
	{/if}
{/if}

<style>
	.statuses { margin-bottom: 0.6rem; flex-wrap: wrap; }
	.chip { background: white; color: var(--dark); border: 1.5px solid var(--smoke); padding: 0.4rem 0.75rem; font-size: 0.75rem; }
	.chip.on { background: var(--dark); color: white; border-color: var(--dark); }
	.chip .n { font-family: var(--mono); opacity: 0.6; margin-left: 0.25rem; }
	.filters { display: grid; grid-template-columns: minmax(16rem, 2fr) repeat(4, minmax(8rem, 1fr)); gap: 0.5rem; }
	@media (max-width: 900px) { .filters { grid-template-columns: 1fr 1fr; } .filters input { grid-column: 1 / -1; } }
	.wrote { margin: 0 0 0.6rem; padding: 0.4rem 0.6rem; border-left: 3px solid var(--red); background: var(--snow); white-space: pre-wrap; overflow-wrap: anywhere; max-width: 32rem; }
	.wrote .label { display: block; color: var(--muted); }
	.cost { width: auto; margin-top: 0.4rem; font-size: 0.85em; }
	.cost td { padding: 0.1rem 0.6rem 0.1rem 0; border: 0; color: var(--muted); }
	.cost td:last-child { text-align: right; white-space: nowrap; color: var(--dark); }
	.cost tr:last-child td { font-weight: 700; color: var(--dark); border-top: 1px solid var(--smoke); }
	.gallery { display: flex; gap: 0.75rem; flex-wrap: wrap; margin-top: 0.5rem; }
	.gallery figure { margin: 0; width: 5rem; }
	.gallery img { width: 5rem; height: 5rem; object-fit: contain; background: var(--snow); border: 1px solid var(--smoke); border-radius: 6px; }
	.gallery figcaption { display: flex; justify-content: space-around; margin-top: 0.15rem; }
	.gallery figcaption button { color: var(--dark); font-size: 0.95rem; }
	.gallery figcaption button:disabled { color: var(--smoke); }
	.add-image {
		display: grid; place-items: center; width: 5rem; height: 5rem; border: 2px dashed var(--smoke);
		border-radius: 6px; cursor: pointer; color: var(--muted); font-size: 0.8rem; text-align: center;
	}
	.size-row { margin-top: 0.35rem; flex-wrap: nowrap; }
	.add-image.busy { cursor: progress; }
	.size { flex-wrap: nowrap; font-weight: 400; }
	.size input { width: 4.5rem; }
	.photos-edit { margin-top: 0.5rem; }
	.pages { display: flex; gap: 0.25rem; align-items: center; flex-wrap: wrap; margin: 0.4rem 0; }
	.pages img { height: 3rem; border-radius: 2px; box-shadow: 0 1px 3px rgba(0, 0, 0, 0.2); }
	.photos-edit summary { cursor: pointer; font-weight: 600; font-size: 0.9rem; }
	.photo-row { display: grid; grid-template-columns: 4rem 1fr auto; gap: 0.5rem; align-items: center; margin: 0.4rem 0; }
	.file-row { display: grid; grid-template-columns: 9rem 1fr auto; gap: 0.5rem; align-items: center; margin: 0.4rem 0; font-size: 0.85rem; }
	.photo-row img { width: 4rem; height: 3rem; object-fit: cover; border-radius: 4px; }
	.photo-row div { display: grid; gap: 0.25rem; }
	.email-body { white-space: pre-wrap; font-family: inherit; margin: 0.5rem 0 0; }
	.small-btn { background: var(--dark); padding: 0.3rem 0.8rem; font-size: 0.85rem; display: inline-block; margin-top: 0.25rem; cursor: pointer; }
</style>
