<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from './api';
	import { stageLabel, nextOwner, printPOStatus, poLinkingMessage, filterPrintRuns, type PrintRun } from './print-run';
	let poLinkingAvailable = $state(true);
	let runs = $state<PrintRun[]>([]),
		error = $state(''),
		origin = $state(''),
		stage = $state(''),
		needs = $state(false),
		owner = $state(0);
	const filtered = $derived(filterPrintRuns(runs, { origin, stage, owner, needs }));
	const owners = $derived([
		...new Map(runs.map((r) => [r.user_id, r.user_name || r.user_email || String(r.user_id)])).entries()
	]);
	onMount(async () => {
		try {
			const result = await api.get<{ runs: PrintRun[]; po_linking_available: boolean }>('/api/admin/print-runs');
			runs = result.runs;
			poLinkingAvailable = result.po_linking_available;
		} catch (e) {
			error = (e as Error).message;
		}
	});
</script>

<h2>Print requests</h2>
<p>One queue for staff print runs and YSWS author material.</p>
{#if !poLinkingAvailable}<p class="muted" role="status">{poLinkingMessage(false)}</p>{/if}
<div class="row" data-track="print_queue_filter">
	<button class:secondary={!needs} onclick={() => (needs = !needs)}>Needs action</button><label
		>Origin<select bind:value={origin}
			><option value="">All</option>{#each ['staff', 'author', 'legacy'] as o}<option>{o}</option>{/each}</select
		></label
	><label
		>Requester<select bind:value={owner}
			><option value={0}>All</option>{#each owners as [id, name]}<option value={id}>{name}</option>{/each}</select
		></label
	><label
		>Stage<select bind:value={stage}
			><option value="">All stages</option>{#each Object.entries(stageLabel) as [key, label]}<option value={key}
					>{label}</option
				>{/each}</select
		></label
	>
</div>
{#if error}<p class="error">{error}</p>{/if}
<div class="queue-table">
	<table data-track="print_queue">
		<thead
			><tr><th>Request / program</th><th>Origin / owner</th><th>Stage</th><th>Next action</th><th>Stock</th></tr></thead
		><tbody
			>{#each filtered as r}<tr
					><td
						><a href={`/print-requests/${r.id}`} data-private>{r.request.title}</a>
						<div class="muted" data-private>#{r.id} · {r.request.program}</div></td
					><td
						>{r.origin}
						<div data-private>{r.user_name || r.user_email}</div></td
					><td>{stageLabel[r.stage]}</td><td>{nextOwner(r)}</td><td
						>{printPOStatus(r)}
						<div class="muted">
							{r.stock?.available ?? 'Unknown'} available · {r.stock?.held ?? 'Unknown'} held · {r.stock?.inbound ??
								'Unknown'} inbound
						</div></td
					></tr
				>{:else}<tr><td colspan="5">No matching print requests.</td></tr>{/each}</tbody
		>
	</table>
</div>

<style>
	.queue-table {
		overflow-x: auto;
	}
	table {
		min-width: 760px;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		align-items: end;
	}
</style>
