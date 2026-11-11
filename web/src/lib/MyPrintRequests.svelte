<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from './api';
	import { stageLabel, nextOwner, type PrintRun } from './print-run';
	let runs = $state<PrintRun[]>([]),
		error = $state('');
	onMount(async () => {
		try {
			runs = (await api.get<{ runs: PrintRun[] }>('/api/print-runs')).runs;
		} catch (e) {
			error = (e as Error).message;
		}
	});
</script>

{#if error}<p class="error">{error}</p>{/if}
{#if runs.length}<h2>Print requests</h2>
	{#each runs as r}<div class="card" data-track="my_print_run">
			<strong><a href={`/print-requests/${r.id}`} data-private>{r.request.title} · Print #{r.id}</a></strong>
			<span class="badge">{stageLabel[r.stage]}</span>
			<p>{nextOwner(r)}</p>
			<p>
				{r.stock?.available ?? 'Unknown'} available to request · {r.stock?.held ?? 'Unknown'} held · {r.stock
					?.inbound ?? 'Unknown'} inbound
			</p>
			<p>Requested {r.request.quantity.toLocaleString()} · Approved {r.approved_quantity || 'Not yet approved'}</p>
			{#if r.shipment_request_id}<p>
					<a href={`/requests#request-${r.shipment_request_id}`}>First shipment request #{r.shipment_request_id}</a> · {r.shipment_status ===
					'pending'
						? 'Pending review'
						: r.shipment_status === 'awaiting_payment'
							? 'Awaiting postage payment'
							: r.shipment_status}.
				</p>{:else}<p>
					{['rejected', 'cancelled'].includes(r.stage)
						? 'No parcel was created.'
						: 'First shipment awaits receipt and shop release.'}
				</p>{/if}
		</div>{/each}
	<h2>Shipment requests</h2>{/if}
