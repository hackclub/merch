<script lang="ts">
	// Admin: staff print requests (postcards, posters) to check, order from Vistaprint, and stock.
	import { onMount } from 'svelte';
	import { api, ApiError, formatCents } from './api';
	import { printStatusLabel, type PrintRequest } from './print';

	let prints = $state<PrintRequest[]>([]);
	let error = $state('');
	let saved = $state('');

	const load = async () => (prints = (await api.get<{ print_requests: PrintRequest[] }>('/api/admin/print-requests')).print_requests);
	onMount(() => load().catch((e) => (error = e.message)));

	async function save(p: PrintRequest) {
		error = saved = '';
		try {
			await api.put(`/api/admin/print-requests/${p.id}`, { status: p.status, admin_note: p.admin_note, sku: p.sku });
			saved = `Saved print request #${p.id}`;
			await load();
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not save.';
		}
	}
</script>

<h2>Print requests</h2>
{#if error}<p class="error">{error}</p>{/if}
{#if saved}<p class="ok">{saved}</p>{/if}
<table>
	<thead><tr><th>Who</th><th>What</th><th>Art</th><th>Qty / estimate</th><th>Status / SKU / note</th><th></th></tr></thead>
	<tbody>
		{#each prints as p (p.id)}
			<tr>
				<td data-private>{p.user_name}<div class="muted">{p.user_email}</div></td>
				<td><strong>{p.title}</strong> <span class="muted">({p.product})</span>
					{#if p.program}<div>For {p.program}</div>{/if}
					<div class="muted" data-private>{p.note}</div>
					{#if p.deadline}<div data-private>Deadline: {p.deadline}</div>{/if}
					{#if p.needed_by}<div>Needed by {p.needed_by.slice(0, 10)}</div>{/if}
					<div class="muted">{new Date(p.created_at).toLocaleDateString()}</div></td>
				<td><a href={p.front_url} target="_blank" rel="noreferrer">Front</a>{#if p.back_url === p.front_url} <span class="muted">(both sides)</span>{:else if p.back_url} · <a href={p.back_url} target="_blank" rel="noreferrer">Back</a>{/if}</td>
				<td>{p.quantity.toLocaleString('en-US')}<div class="muted">{formatCents(p.estimate_cents)}</div>
					{#if p.initial_quantity}<div>First {p.initial_quantity} to them{p.shipment_request_id ? `: request #${p.shipment_request_id}` : ' once stocked'}</div>{/if}</td>
				<td>
					<select bind:value={p.status}>
						{#each Object.entries(printStatusLabel) as [st, label]}<option value={st}>{label}</option>{/each}
					</select>
					<input bind:value={p.sku} placeholder="warehouse SKU once stocked" title="Stocked lists the SKU in the shop (made from their art if it's new) and requests their first shipment" />
					<input bind:value={p.admin_note} placeholder="note to them" />
				</td>
				<td><button onclick={() => save(p)}>Save</button></td>
			</tr>
		{:else}
			<tr><td colspan="6" class="muted">No print requests yet.</td></tr>
		{/each}
	</tbody>
</table>
<h2>YSWS author submissions</h2>
