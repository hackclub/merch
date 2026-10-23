<script lang="ts">
	// My requests: postcards and posters someone asked us to print, and where each one is.
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api } from './api';
	import { printStatusLabel, type PrintRequest } from './print';

	let prints = $state<PrintRequest[]>([]);
	onMount(async () => {
		prints = (await api.get<{ print_requests: PrintRequest[] }>('/api/print-requests')).print_requests;
	});
</script>

{#if page.url.searchParams.get('printed')}
	<p class="ok">Sent! We'll check your art and order it from Vistaprint, usually within a business day. It shows up here as it moves along.</p>
{/if}
{#if prints.length}
	<h2>Printing</h2>
	{#each prints as p (p.id)}
		<div class="card print" data-track="print-request">
			<div class="row">
				<strong>{p.title}</strong>
				<span class="badge {p.status}">{printStatusLabel[p.status]}</span>
			</div>
			<p>{p.quantity.toLocaleString('en-US')} × {p.product === 'postcard_4x6' ? '4x6 postcard' : '11x17 poster'} · <a href={p.front_url} target="_blank" rel="noreferrer">front</a>{#if p.back_url} · <a href={p.back_url} target="_blank" rel="noreferrer">back</a>{/if}</p>
			<p class="muted">Sent {new Date(p.created_at).toLocaleDateString()}{p.needed_by ? ` · needed by ${p.needed_by.slice(0, 10)}` : ''}</p>
			{#if p.status === 'stocked' && p.sku}<p>In the warehouse. <a href="/">Request it from the shop</a> like any other item.</p>{/if}
			{#if p.admin_note}<p class="muted">Note from Hack Club: {p.admin_note}</p>{/if}
		</div>
	{/each}
	<h2>Swag</h2>
{/if}

<style>
	.print { margin-bottom: 0.75rem; }
	.print p { margin: 0.25rem 0 0; }
	.row { display: flex; gap: 0.75rem; align-items: center; justify-content: space-between; }
</style>
