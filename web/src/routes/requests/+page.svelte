<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, formatCents, statusLabel } from '$lib/api';
	import { purposeLabel } from '$lib/purpose';
	import { arrivalText } from '$lib/shipping';
	import { lineText } from '$lib/ship';
	import { paidNotice, payShipping, type CardForm } from '$lib/payment';
	import CardField from '$lib/CardField.svelte';
	import { page } from '$app/state';
	import { session } from '$lib/session.svelte';
	import type { SwagRequest } from '$lib/types';

	let requests = $state<SwagRequest[] | null>(null);
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
			requests = (await api.get<{ requests: SwagRequest[] }>('/api/requests')).requests;
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not load your requests.';
		}
	}
	onMount(load);

	async function cancel(id: number) {
		if (!confirm('Cancel this request?')) return;
		try {
			await api.post(`/api/requests/${id}/cancel`);
			await load();
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not cancel.';
		}
	}
</script>

<svelte:head><title>My requests · Hack Club Merch Request</title></svelte:head>

<h1>My requests</h1>
{#if !session.me?.user}
	<p><a class="button" href="/auth/login" data-sveltekit-reload>Sign in with Hack Club</a></p>
{:else}
	{#if error}<p class="error">{error}</p>{/if}
	{#if thanks}<p>{thanks}</p>{/if}
	{#each requests ?? [] as r (r.id)}
		<div class="card" style="margin-bottom: 1rem">
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
				<p>
					Shipping to {r.address.country} costs {formatCents(r.shipping_fee_cents)}.
					{#if r.payment_url}<a class="button" href={r.payment_url} data-track="pay-shipping">Pay shipping on HCB</a>{/if}
				</p>
				{#if r.stripe_key}
					<CardField bind:this={cards[r.id]} stripeKey={r.stripe_key} amountCents={r.shipping_fee_cents} billing={r.address} />
					{#if payErrors[r.id]}<p class="error">{payErrors[r.id]}</p>{/if}
					<button disabled={paying === r.id} onclick={() => pay(r.id)}>{paying === r.id ? 'Paying…' : `Pay ${formatCents(r.shipping_fee_cents)}`}</button>
				{/if}
			{/if}
			{#if arrivalText(r.estimate)}<p>{arrivalText(r.estimate)}, by {r.estimate?.service}.</p>{/if}
			{#if r.mailed_at}<p>Mailed {new Date(r.mailed_at).toLocaleDateString()}</p>{/if}
			{#if r.tracking_number}<p>Tracking: {r.carrier} {r.tracking_number}</p>{/if}
			{#if r.admin_note}<p class="muted">Note from Hack Club: {r.admin_note}</p>{/if}
			{#if r.status === 'pending' || r.status === 'awaiting_payment'}
				<button class="link" onclick={() => cancel(r.id)}>Cancel request</button>
			{/if}
		</div>
	{:else}
		{#if requests}<p class="muted">No requests yet. <a href="/">Pick some swag!</a></p>{/if}
	{/each}
{/if}
