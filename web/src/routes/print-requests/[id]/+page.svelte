<script lang="ts">
	import { blankAddress, prefillAddress } from '$lib/address';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { session } from '$lib/session.svelte';
	import {
		stageLabel,
		stages,
		nextOwner,
		poState,
		actionPreview,
		actionLabel,
		printEventLabel,
		approvalReviewer,
		approvedQuantityLabel,
		stockStale,
		poLinkingMessage,
		type PrintDetail,
		type PO,
		type PrintRun
	} from '$lib/print-run';
	let data = $state<PrintDetail | null>(null),
		error = $state(''),
		busy = $state(false),
		action = $state(''),
		quantity = $state(0),
		specs = $state(''),
		sku = $state(''),
		itemID = $state(0),
		note = $state(''),
		internal = $state(''),
		eta = $state(''),
		tracking = $state(''),
		confirmation = $state(''),
		disposition = $state(''),
		reviewed = $state(false),
		suggestions = $state<PO[]>([]),
		selected = $state<PO[]>([]);
	let dispatchTime = $state(''),
		updateSource = $state(''),
		firstQuantity = $state(25),
		deliveryAddress = $state(prefillAddress(blankAddress(), session.me?.user?.address)),
		notificationID = $state(0),
		deliveryOutcome = $state('sent');
	const deliveryFields = [
		['first_name', 'First name'],
		['last_name', 'Last name'],
		['line_1', 'Street address'],
		['line_2', 'Address line 2'],
		['city', 'City'],
		['state', 'State / region'],
		['postal_code', 'Postal code'],
		['country', 'Country (two-letter code)'],
		['phone_number', 'Phone']
	] as const;
	const admin = $derived(!!session.me?.roles.admin);
	const run = $derived(data?.run);
	const id = $derived(Number(page.params.id));
	async function load() {
		data = await api.get<PrintDetail>(`/api/print-runs/${id}`);
		if (!quantity) quantity = data.run.approved_quantity || data.run.request.quantity;
		sku = data.run.sku;
		itemID = data.run.item_id;
		if (data.run.request.address) deliveryAddress = { ...data.run.request.address };
		firstQuantity = data.run.request.initial_quantity || 25;
	}
	onMount(() => load().catch((e) => (error = e.message)));
	async function suggest() {
		error = '';
		try {
			suggestions = (
				await api.get<{ suggestions: PO[] }>(`/api/admin/print-po-suggestions?sku=${encodeURIComponent(sku)}`)
			).suggestions;
		} catch (e) {
			error =
				e instanceof ApiError && [502, 503].includes(e.status)
					? 'The PO copy is temporarily unavailable. Your request is saved; try again shortly.'
					: (e as Error).message;
		}
	}
	function choose(a: string) {
		action = a;
		reviewed = false;
		note = '';
		internal = '';
		confirmation = '';
		disposition = '';
		if (['link', 'replace_links'].includes(a)) {
			selected = [];
			suggestions = [];
		}
	}
	async function save() {
		if (!run) return;
		busy = true;
		error = '';
		try {
			await api.post(`/api/print-runs/${id}/actions`, {
				action,
				version: run.version,
				dispatched_at: dispatchTime ? new Date(dispatchTime).toISOString() : undefined,
				initial_quantity: firstQuantity,
				address: { ...deliveryAddress, country: deliveryAddress.country.trim().toUpperCase() },
				notification_id: notificationID,
				delivery_outcome: deliveryOutcome,
				approved_quantity: quantity,
				specifications: specs,
				sku,
				item_id: itemID,
				note,
				internal_note: internal,
				eta,
				eta_source: ['update', 'review_receipt'].includes(action) ? updateSource : 'HQ update',
				tracking,
				confirmation_url: confirmation,
				cancellation_disposition: disposition,
				links: selected.map((p) => ({ po_id: p.po_id, line_id: p.line_id, sku: p.sku }))
			});
			action = '';
			internal = '';
			await load();
		} catch (e) {
			error =
				e instanceof ApiError && e.status === 503 && ['link', 'replace_links'].includes(action)
					? poLinkingMessage(false)
					: (e as Error).message;
		} finally {
			busy = false;
		}
	}
	async function preference() {
		if (!run) return;
		try {
			await api.post(`/api/print-runs/${id}/actions`, {
				action: 'preferences',
				version: run.version,
				slack_opt_in: !run.slack_opt_in
			});
			await load();
		} catch (e) {
			error = (e as Error).message;
		}
	}
	async function reorder() {
		if (!run) return;
		busy = true;
		try {
			const next = await api.post<PrintRun>(`/api/print-runs/${id}/actions`, {
				action: 'reorder',
				version: run.version
			});
			window.location.href = `/print-requests/${next.id}`;
		} catch (e) {
			error = (e as Error).message;
			busy = false;
		}
	}
	const hasOrder = $derived(
		!!run &&
			(['ordered', 'in_transit'].includes(run.stage) || run.links.some((p) => !p.draft && !p.deleted && !!p.created_at))
	);
	const fmt = (v: string | null | undefined) => (v ? new Date(v).toLocaleString() : 'Unknown');
</script>

<svelte:head><title>Print request · Hack Club Merch</title></svelte:head>
<div class="print-page">
	<a href={admin ? '/admin?tab=submissions' : '/requests'}>← {admin ? 'Print requests' : 'My requests'}</a>
	{#if error}<p class="error">{error}</p>
		<button
			class="secondary"
			onclick={() => {
				error = '';
				load().catch((e) => (error = e.message));
			}}>Reload current request</button
		>{/if}
	{#if data && run}
		<h1 data-private>{run.request.title}</h1>
		<p class="meta">
			PRINT #{run.id} · {run.origin} · {run.request.product === 'postcard_4x6'
				? '4 × 6 postcard'
				: run.request.product === 'poster_11x17'
					? '11 × 17 poster'
					: 'Custom material'} · {run.request.finish} · {run.request.quantity.toLocaleString()} copies
		</p>
		<nav class="steps" aria-label="Print stages">
			{#each stages as st}<div class:current={st === run.stage} class:done={data.history.some((e) => e.stage === st)}>
					{stageLabel[st]}
				</div>{/each}
		</nav>
		<div class="columns">
			<section>
				<div class="card">
					<h2>{stageLabel[run.stage]}</h2>
					<p class="callout">{nextOwner(run, admin)}</p>
					{#if admin && run.legacy_review && !['available', 'rejected', 'cancelled'].includes(run.stage)}<p
							class="muted"
						>
							Legacy run. Unknown stage dates remain unknown. Audit the original order, listing and first shipment links
							before continuing.
						</p>{/if}
					{#if run.reconcile && !['rejected', 'cancelled'].includes(run.stage)}<p class="error">
							{admin
								? 'Warehouse evidence needs reconciliation. A reopened or corrected PO does not reverse receipt or release.'
								: 'HQ is reviewing the latest warehouse update. Your saved stage has not changed.'}
						</p>{/if}
					{#if run.request.front_preview_url}<div class="art">
							<img src={run.request.front_preview_url} alt="Front artwork" />{#if run.request.back_preview_url}<img
									src={run.request.back_preview_url}
									alt="Back artwork"
								/>{/if}
						</div>{/if}
					<p>
						<a href={run.request.front_url} target="_blank" rel="noreferrer">Original front artwork</a
						>{#if run.request.back_url}
							· <a href={run.request.back_url} target="_blank" rel="noreferrer">Original back artwork</a>{/if} · Art revision
						{run.revision || 1} (immutable)
					</p>
					<p data-private>{run.request.note}</p>
					<p data-private>
						Program: {run.request.program} · Deadline: {run.request.deadline ||
							run.request.needed_by?.slice(0, 10) ||
							'Unknown'}
					</p>
					{#if run.request.product === 'custom'}<p>
							Custom specifications require HQ review. No automated price or artwork checks are claimed.
						</p>{/if}
					<p>
						Requested: <strong>{run.request.quantity}</strong> · Approved:
						<strong
							>{run.approved_quantity || (run.origin === 'legacy' ? 'Unknown (legacy)' : 'Not yet approved')}</strong
						>
					</p>
					{#if run.approved_at}<p>Approved {fmt(run.approved_at)} · {approvalReviewer(admin, run.approved_by)}</p>
						<p data-private>{run.specifications}</p>{/if}
					{#if run.parent_id}<a href={`/print-requests/${run.parent_id}`}>Previous print run #{run.parent_id}</a>{/if}
					{#if run.public_note}<p data-private>HQ update: {run.public_note}</p>{/if}
					{#if run.eta}<p data-private>
							Expected warehouse arrival: {run.eta} ({run.eta_source}). This is not a delivery promise.
						</p>{/if}
					{#if run.tracking}<p data-private>
							Printer → warehouse: {run.tracking} · Dispatched {fmt(run.dispatched_at)}
						</p>{/if}
					<div class="row" data-track="print_actions">
						{#if admin}
							{#if !data.po_linking_available}<p class="muted" role="status">{poLinkingMessage(false)}</p>{/if}
							{#if run.stage === 'submitted'}<button onclick={() => choose('approve')}>Approve print run</button><button
									class="secondary"
									onclick={() => choose('reject')}>Reject with reason</button
								>{/if}
							{#if (run.stage === 'approved' || (run.stage === 'ordered' && run.reviewed_existing_order)) && !run.links.length}<button
									disabled={!data.po_linking_available || run.legacy_review}
									onclick={() => choose('link')}>Link warehouse purchase orders</button
								>{/if}
							{#if run.stage === 'approved' && !hasOrder}<button class="secondary" onclick={() => choose('reject')}
									>Reject with reason</button
								>{/if}
							{#if ['approved', 'ordered'].includes(run.stage) && run.links.length}<button
									class="secondary"
									disabled={!data.po_linking_available}
									onclick={() => choose('replace_links')}>Correct linked PO lines</button
								>{/if}
							{#if run.stage === 'ordered'}<button onclick={() => choose('dispatch')}>Record printer dispatch</button
								>{/if}
							{#if run.stage === 'received' && !run.legacy_review}<button onclick={() => choose('release')}
									>Make available in shop</button
								>{/if}
							{#if (run.reconcile || run.links.length > 0) && ['approved', 'ordered', 'in_transit'].includes(run.stage)}<button
									class="secondary"
									onclick={() => choose('review_order')}>Review order discrepancy</button
								>{/if}
							<button class="secondary" disabled={!data.po_automation} onclick={() => choose('refresh_po')}
								>Refresh PO copy</button
							>
							{#if run.reconcile && ['received', 'available'].includes(run.stage)}<button
									class="secondary"
									onclick={() => choose('review_receipt')}>Review corrected receipt</button
								>{/if}
							{#if run.legacy_review && !['available', 'rejected', 'cancelled'].includes(run.stage)}<button
									onclick={() => choose('audit_legacy')}>Audit legacy links</button
								>{/if}
							{#if !['rejected', 'cancelled'].includes(run.stage)}<button
									class="secondary"
									onclick={() => choose('update')}>Record warehouse / delivery update</button
								>{/if}
							<button class="secondary" onclick={() => choose('retry_notifications')}>Retry failed notifications</button
							><button class="secondary" onclick={() => choose('resolve_notification')}
								>Reconcile uncertain delivery</button
							>
						{/if}
						{#if session.me?.user?.id === run.user_id && !run.shipment_request_id && !['available', 'rejected', 'cancelled'].includes(run.stage)}<button
								class="secondary"
								onclick={() => choose('first_shipment')}>Set first shipment</button
							>{/if}
						{#if (['submitted', 'approved'].includes(run.stage) && !hasOrder) || (admin && ['approved', 'ordered', 'in_transit'].includes(run.stage))}<button
								class="secondary"
								onclick={() => choose('cancel')}>Cancel with reason</button
							>{/if}
						{#if ['available', 'rejected', 'cancelled'].includes(run.stage)}<button onclick={reorder} disabled={busy}
								>Request another run with this artwork</button
							><a href="/">Submit revised artwork as a new request</a>{/if}
					</div>
					{#if action}<form
							onsubmit={(e) => {
								e.preventDefault();
								save();
							}}
							data-track="print_action_form"
						>
							<h3>{actionLabel(action)}</h3>
							{#if ['approve', 'review_order', 'audit_legacy'].includes(action)}<label
									>Approved quantity<input type="number" min="1" max="100000" required bind:value={quantity} /></label
								><label
									>Reviewed specifications, artwork revision and budget<textarea required bind:value={specs}
									></textarea></label
								>{/if}
							{#if ['link', 'replace_links', 'audit_legacy'].includes(action)}<label
									>Exact warehouse SKU<input required bind:value={sku} /></label
								><label
									>Existing item ID (required for custom material / legacy audit)<input
										type="number"
										min="0"
										bind:value={itemID}
									/></label
								>{/if}
							{#if ['link', 'replace_links'].includes(action)}<label
									>Printer order confirmation URL<input type="url" required bind:value={confirmation} /></label
								><button type="button" class="secondary" onclick={suggest}>Suggest matching POs</button>
								<p>HQ chooses each exact line. A SKU match never attaches an order automatically.</p>
								{#each suggestions as p}<label class="candidate"
										><input
											type="checkbox"
											checked={selected.some((x) => x.po_id === p.po_id && x.line_id === p.line_id)}
											onchange={(e) =>
												(selected = e.currentTarget.checked
													? [...selected, p]
													: selected.filter((x) => x.po_id !== p.po_id || x.line_id !== p.line_id))}
										/>{p.order_number || 'Purchase order'} · PO ID {p.po_id} · line {p.line_id} · {p.sku} · {p.quantity} ordered
										· {p.draft ? 'Draft' : p.deleted ? 'Deleted' : p.completed ? 'Complete' : 'Open'} · Source sync {fmt(
											p.synced_at
										)}</label
									>{/each}
								<p>
									Selected: {selected.reduce((n, p) => n + p.quantity, 0)} units. Approved: {run.approved_quantity}.
									Differences require review and are never auto-confirmed.
								</p>{/if}
							{#if ['update', 'review_receipt'].includes(action)}<label
									>Evidence source / who confirmed<input required bind:value={updateSource} /></label
								>{/if}
							{#if action === 'dispatch'}<label
									>Printer dispatch time (leave blank if unknown)<input
										type="datetime-local"
										bind:value={dispatchTime}
									/></label
								><label>Carrier / tracking (if provided)<input bind:value={tracking} /></label>{/if}
							{#if ['dispatch', 'update'].includes(action)}<label
									>Expected warehouse arrival (if known)<input type="date" bind:value={eta} /></label
								>{/if}
							{#if action === 'cancel' && hasOrder}<label
									>Printer cancellation confirmation URL<input type="url" required bind:value={confirmation} /></label
								><label>Disposition of goods and payment<textarea required bind:value={disposition}></textarea></label
								>{/if}
							{#if ['cancel', 'reject', 'update', 'audit_legacy', 'review_order', 'review_receipt', 'resolve_notification', 'replace_links'].includes(action)}<label
									>Update to requester {#if action === 'reject'}(reason and how to revise){/if}<textarea
										required
										bind:value={note}></textarea></label
								>{/if}
							{#if action === 'first_shipment'}<label
									>First quantity<input
										type="number"
										min="1"
										max={run.approved_quantity || run.request.quantity}
										required
										bind:value={firstQuantity}
									/></label
								>
								<div class="form-grid" data-private>
									{#each deliveryFields as [key, label]}<label
											>{label}<input
												required={!['line_2', 'state', 'phone_number'].includes(key) ||
													(key === 'phone_number' && deliveryAddress.country.toUpperCase() !== 'US')}
												bind:value={deliveryAddress[key]}
											/></label
										>{/each}
								</div>{/if}
							{#if action === 'resolve_notification'}<label
									>Uncertain notification ID<input type="number" min="1" required bind:value={notificationID} /></label
								><label
									>Verified provider outcome<select bind:value={deliveryOutcome}
										><option value="sent">Already delivered</option><option value="not_sent"
											>Confirmed not delivered; retry safely</option
										></select
									></label
								>{/if}
							{#if admin}<label>Private HQ note<textarea bind:value={internal}></textarea></label>{/if}
							<h3>Requester notification preview</h3>
							<p class="callout" data-private>
								{actionPreview(
									['link', 'replace_links'].includes(action) ? { ...run, sku, links: selected } : run,
									action,
									quantity,
									note,
									eta,
									Date.now(),
									data.po_automation,
									updateSource
								)}
							</p>
							<label class="candidate"
								><input type="checkbox" required bind:checked={reviewed} />I reviewed this action and its requester
								update.</label
							>{#if action === 'retry_notifications'}{#each data.notifications.filter((n) => n.status === 'failed') as n}<details
										open
									>
										<summary>{n.subject}</summary>
										<p class="message" data-private>{n.body}</p>
									</details>{/each}{/if}<button disabled={busy}>{busy ? 'Saving…' : 'Save action'}</button><button
								type="button"
								class="secondary"
								onclick={() => (action = '')}>Back</button
							>
						</form>{/if}
				</div>
				<div class="card">
					<h2>Activity</h2>
					{#each data.history as e, i}<article>
							<strong>{printEventLabel(e, data.history[i + 1]?.stage, admin)}</strong>
							<div class="muted">{fmt(e.at)} · {e.source}{e.actor_id ? ` · account #${e.actor_id}` : ''}</div>
							{#if !e.at}<div class="muted">Recorded {fmt(e.recorded_at)}; event date unknown</div>{/if}{#if e.note}<p
									data-private
								>
									{e.note}
								</p>{/if}{#if e.details?.update_source}<p data-private>
									Source: {e.details.update_source}
								</p>{/if}{#if e.details?.specifications}<details>
									<summary>Approved specification snapshot</summary>
									<p data-private>{e.details.specifications}</p>
									<p>{e.details.approved_quantity} approved · art revision {e.details.art_revision}</p>
								</details>{/if}
						</article>{/each}
				</div>
			</section>
			<aside>
				<div class="card">
					<h2>Stock & first shipment</h2>
					<dl>
						<dt>Requested</dt>
						<dd>{run.request.quantity}</dd>
						<dt>Approved</dt>
						<dd>{approvedQuantityLabel(run)}</dd>
						<dt>PO line ordered</dt>
						<dd>
							{admin
								? run.links.length
									? run.links.reduce((n, p) => n + p.quantity, 0)
									: 'Not linked'
								: (run.po_line_ordered ?? 'Not linked')}
						</dd>
						<dt>Inbound stock</dt>
						<dd>{data.stock.inbound ?? 'Unavailable'}</dd>
						<dt>Warehouse sellable</dt>
						<dd>{data.stock.warehouse_sellable ?? 'Unavailable'}</dd>
						<dt>Held in merch</dt>
						<dd>{data.stock.held ?? 'Unavailable'}</dd>
						<dt>Available to request</dt>
						<dd>{data.stock.available ?? 'Unavailable'}</dd>
					</dl>
					<p class="muted">
						Individual units. Postcard pack sizes apply separately. Warehouse sellable already excludes warehouse
						allocations.
					</p>
					<p>
						Stock synced: {fmt(data.stock.synced_at)}{data.stock.synced_at &&
						stockStale(data.stock.synced_at, data.stock.max_age_seconds)
							? ' (stale; availability is not promised)'
							: ''}
					</p>
					<p>Shop: {data.stock.visible ? 'Visible' : 'Hidden until release'}</p>
					{#if run.item_id && data.stock.visible}<a href={`/items/${run.item_id}`}>Open shop item →</a>{/if}
					{#if data.shipment}<p>
							<a href={`/requests#request-${data.shipment.id}`}>First shipment #{data.shipment.id}</a> · {data.shipment
								.status === 'pending'
								? 'Pending review'
								: data.shipment.status === 'awaiting_payment'
									? 'Awaiting postage payment'
									: data.shipment.status}
						</p>
						<p>
							{data.shipment.status === 'dispatched'
								? 'Sent to warehouse for fulfillment.'
								: 'Your first parcel has not shipped.'}
						</p>
						{#if data.shipment.tracking_number}<p>
								{data.shipment.carrier} · {data.shipment.tracking_number}
							</p>{/if}{:else}<p>
							{#if ['rejected', 'cancelled'].includes(run.stage)}No parcel has been created.{:else}First {run.request
									.initial_quantity || 0} · Awaiting shop release. No parcel has been created.{/if}
						</p>{/if}
				</div>
				<div class="card">
					<h2>{admin ? 'Warehouse PO evidence' : 'Warehouse status'}</h2>
					{#if admin && !data.po_automation}<p class="muted">
							Automatic transitions are off until the read-only source is enabled.
						</p>{/if}{#if admin}{#each data.confirmations || [] as c}<p>
								<a href={c.url} target="_blank" rel="noreferrer">Printer confirmation · {fmt(c.at)}</a>
							</p>{/each}{/if}
					<p>Last merch poll: {fmt(run.last_poll_at)}</p>
					{#if run.poll_error}<p>{run.poll_error}</p>{/if}{#each run.links as p}<article>
							{#if admin}<strong>{p.order_number || 'Purchase order'} · PO ID {p.po_id} · line {p.line_id}</strong>{/if}
							<p>{poState(p)}</p>
							{#if admin}<p>{p.supplier || 'Supplier unknown'}</p>
								<p>Exact SKU: {p.sku} · PO line ordered: {p.quantity}</p>{/if}
							<p>
								Created: {fmt(p.created_at)}<br />Required-by: {p.required_by?.slice(0, 10) || 'Unknown'} (expected date)<br
								/>Completed: {fmt(p.completed_at)}<br />Source sync: {fmt(p.synced_at)}<br />Last poll: {fmt(
									p.polled_at
								)}
							</p>
						</article>{/each}
					<p class="muted">
						Completion is for the whole PO. Per-line received and outstanding quantities are unavailable. Carrier scans
						and stock increases do not confirm receipt.
					</p>
				</div>
				<div class="card">
					<h2>Requester notifications</h2>
					<p>Email at every stage. Failed delivery retries separately; saved stages stay saved.</p>
					{#if session.me?.user?.id === run.user_id && (session.me?.print_slack_available || run.slack_opt_in)}<label
							class="candidate"
							><input
								type="checkbox"
								name="print_slack_opt_in"
								disabled={session.me?.user?.id !== run.user_id}
								checked={run.slack_opt_in}
								onchange={preference}
							/>Also send me Slack DMs (optional; off by default)</label
						>{:else}<p>Requester Slack DMs: {run.slack_opt_in ? 'Opted in' : 'Off'}</p>{/if}
					<p class="muted">Verified account only; no channel posts, addresses or invoice details.</p>
					{#each data.notifications as n}<details>
							<summary>#{n.id} · {n.channel} · {n.status} · {n.attempts} attempts</summary><strong>{n.subject}</strong>
							<p class="message" data-private>{n.body}</p>
							{#if admin && n.error}<p class="error" data-private>{n.error}</p>{/if}
						</details>{/each}
				</div>
				{#if admin && run.internal_note}<div class="card">
						<h2>Private HQ notes</h2>
						<p class="message" data-private>{run.internal_note}</p>
					</div>{/if}
			</aside>
		</div>{/if}
</div>

<style>
	.print-page {
		max-width: 1124px;
		margin: 32px auto;
	}
	:global(body:has(.print-page)) {
		background: var(--snow);
	}
	.art {
		height: 180px;
	}
	.art img {
		height: 100%;
	}

	.meta,
	dt {
		font-family: var(--mono);
		font-size: 0.75rem;
		text-transform: uppercase;
	}
	.steps {
		display: flex;
		gap: 8px;
		margin: 1.5rem 0 2rem;
		overflow: auto;
	}
	.steps > div {
		border-top: 3px solid var(--smoke);
		padding-top: 10px;
		flex: 1;
		min-width: 110px;
		font-size: 0.8rem;
	}
	.steps .done {
		border-color: var(--dark);
	}
	.steps .current {
		border-color: var(--red);
		color: var(--red);
		font-weight: bold;
	}
	.columns {
		display: grid;
		grid-template-columns: minmax(0, 1.7fr) minmax(280px, 1fr);
		gap: 24px;
	}
	.card {
		margin-bottom: 18px;
		padding: 24px;
	}
	.card h2 {
		margin-top: 0;
	}
	.art {
		background: var(--tile);
		display: flex;
		gap: 12px;
		padding: 20px;
	}
	.art img {
		width: 48%;
		object-fit: contain;
	}
	.callout {
		background: var(--tile);
		padding: 16px;
		border-left: 3px solid var(--red);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	label {
		display: block;
		margin: 12px 0;
	}
	.candidate {
		display: flex;
		gap: 8px;
		align-items: baseline;
	}
	.candidate input {
		width: auto;
	}
	article {
		padding: 14px 0;
		border-bottom: 1px solid var(--smoke);
	}
	dl {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 8px;
	}
	dd {
		margin: 0;
		text-align: right;
		font-weight: bold;
	}
	details {
		margin-top: 12px;
	}
	.message {
		white-space: pre-wrap;
	}
	form {
		margin-top: 24px;
	}
	@media (max-width: 780px) {
		.columns {
			grid-template-columns: 1fr;
		}
		.card {
			padding: 18px;
		}
	}
</style>
