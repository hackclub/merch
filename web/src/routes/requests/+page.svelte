<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, formatCents, statusLabel } from '$lib/api';
	import { purposeLabel } from '$lib/purpose';
	import { arrivalText } from '$lib/shipping';
	import { customsInvoiceURL, lineText } from '$lib/ship';
	import { paidNotice, payShipping, type CardForm } from '$lib/payment';
	import CardField from '$lib/CardField.svelte';
	import Haul3D from '$lib/Haul3D.svelte';
	import { requestHaul } from '$lib/haul';
	import { page } from '$app/state';
	import { session } from '$lib/session.svelte';
	import type { Item, SwagRequest } from '$lib/types';

	let requests = $state<SwagRequest[] | null>(null);
	// Every item in them (hidden ones too) and what they ship with, for the 3D view.
	let items = $state<Item[]>([]);
	let error = $state('');
	let paidNow = $state<string | null>(null);
	const thanks = $derived(paidNotice(requests, paidNow ?? page.url.searchParams.get('paid')));

	// Card fields for requests still waiting on their shipping payment, and why a card failed.
	let cards = $state<Record<number, CardForm>>({});
	const unpaid = Number(page.url.searchParams.get('unpaid'));
	let payErrors = $state<Record<number, string>>(unpaid && page.state.payError ? { [unpaid]: page.state.payError } : {});
	let paying = $state(0);

	async function pay(id: number) {
		const card = cards[id];
		if (!card) return;
		paying = id;
		payErrors[id] = '';
		try {
			const problem = (await card.check()) || (await payShipping(card, id, (path) => api.post(path), window.location.origin));
			payErrors[id] = problem;
			if (!problem) paidNow = String(id);
			await load();
		} finally {
			paying = 0;
		}
	}

	async function load() {
		try {
			const got = await api.get<{ requests: SwagRequest[]; items: Item[] }>('/api/requests');
			items = got.items ?? [];
			requests = got.requests;
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not load your requests.';
		}
	}
	onMount(load);

	async function cancel(id: number) {
		const paid = requests?.find((r) => r.id === id)?.paid_at;
		if (!confirm(paid ? 'Cancel this request? We’ll refund your postage to your card.' : 'Cancel this request?')) return;
		try {
			await api.post(`/api/requests/${id}/cancel`);
			await load();
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not cancel.';
		}
	}
</script>

<svelte:head><title>My requests · Hack Club Merch Request</title></svelte:head>

<p class="back label"><a href="/" data-track="back-to-request">← Request swag</a></p>
<h1>My requests</h1>
{#if !session.me?.user}
	<p><a class="button" href="/auth/login" data-sveltekit-reload>Sign in with Hack Club</a></p>
{:else}
	{#if error}<p class="error">{error}</p>{/if}
	{#if thanks}<p>{thanks}</p>{/if}
	{#each requests ?? [] as r (r.id)}
		{@const haul = requestHaul(r.lines, items)}
		<div class="card request" class:with-3d={haul.length}>
			{#if haul.length}<div class="haul-3d" data-track="request-3d"><Haul3D lines={haul} includes={items} compact /></div>{/if}
			<div class="details">
				<div class="row">
					<strong>#{r.id}</strong>
					<span class="badge {r.status}">{statusLabel[r.status]}</span>
					<span class="muted">{new Date(r.created_at).toLocaleDateString()}</span>
					{#if r.purpose}<span class="badge">{purposeLabel(r.purpose)}</span>{/if}
				</div>
				<ul>
					{#each r.lines as l}<li>{lineText(l)}</li>{/each}
				</ul>
				{#if r.status === 'awaiting_payment'}
					<p>Shipping to {r.address.country} costs {formatCents(r.shipping_fee_cents)}.</p>
					{#if r.stripe_key}
						<CardField bind:this={cards[r.id]} stripeKey={r.stripe_key} amountCents={r.shipping_fee_cents} billing={r.address} />
						{#if payErrors[r.id]}<p class="error">{payErrors[r.id]}</p>{/if}
						<button disabled={paying === r.id} onclick={() => pay(r.id)}>{paying === r.id ? 'Paying…' : `Pay ${formatCents(r.shipping_fee_cents)}`}</button>
					{/if}
				{/if}
				{#if arrivalText(r.estimate)}<p>{arrivalText(r.estimate)}, by {r.estimate?.service}.</p>{/if}
				{#if r.mailed_at}<p>Mailed {new Date(r.mailed_at).toLocaleDateString()}</p>{/if}
				{#if r.tracking_number}<p>Tracking: {r.carrier} {r.tracking_number}</p>{/if}
				{#if customsInvoiceURL(r)}
					<p><a href={customsInvoiceURL(r)} target="_blank" rel="external noopener" data-track="customs-invoice">Customs invoice (PDF)</a> <span class="muted">· shows customs this is a gift, at what it cost Hack Club</span></p>
				{/if}
				{#if r.admin_note}<p class="muted">Note from Hack Club: {r.admin_note}</p>{/if}
				{#if r.refunded_cents}<p>{formatCents(r.refunded_cents)} of your postage refunded to your card.</p>{/if}
				{#if r.status === 'pending' || r.status === 'awaiting_payment'}
					<button class="link" onclick={() => cancel(r.id)}>Cancel request</button>
				{/if}
			</div>
		</div>
	{:else}
		{#if requests}<p class="muted">No requests yet. <a href="/">Pick some swag!</a></p>{/if}
	{/each}
{/if}

<style>
	.back { margin: 0 0 1rem; }
	.back a { text-decoration: none; color: var(--muted); }
	.back a:hover { color: var(--dark); }
	.request { margin-bottom: 1rem; }
	.with-3d { display: grid; grid-template-columns: minmax(0, 26rem) minmax(0, 1fr); gap: 1.5rem; align-items: start; }
	@media (max-width: 700px) {
		.with-3d { grid-template-columns: minmax(0, 1fr); }
	}
</style>
