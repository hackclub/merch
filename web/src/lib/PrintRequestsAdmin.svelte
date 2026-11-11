<script lang="ts">
 import {onMount} from 'svelte';import {api} from './api';import {stageLabel,nextOwner,type PrintRun} from './print-run';
 let runs=$state<PrintRun[]>([]),error=$state(''),origin=$state(''),stage=$state(''),needs=$state(false);
 const filtered=$derived(runs.filter(r=>(!origin||r.origin===origin)&&(!stage||r.stage===stage)&&(!needs||!['available','cancelled','rejected'].includes(r.stage))));
 onMount(async()=>{try{runs=(await api.get<{runs:PrintRun[]}>('/api/admin/print-runs')).runs}catch(e){error=(e as Error).message}});
</script>
<h2>Print requests</h2><p>One queue for staff print runs and YSWS author material.</p>
<div class="row" data-track="print_queue_filter"><button class:secondary={!needs} onclick={()=>needs=!needs}>Needs action</button><label>Origin<select bind:value={origin}><option value="">All</option>{#each ['staff','author','legacy'] as o}<option>{o}</option>{/each}</select></label><label>Stage<select bind:value={stage}><option value="">All stages</option>{#each Object.entries(stageLabel) as [key,label]}<option value={key}>{label}</option>{/each}</select></label></div>
{#if error}<p class="error">{error}</p>{/if}
<table data-track="print_queue"><thead><tr><th>Request / program</th><th>Origin / owner</th><th>Stage</th><th>Next action</th><th>Stock</th></tr></thead><tbody>{#each filtered as r}<tr><td><a href={`/print-requests/${r.id}`} data-private>{r.request.title}</a><div class="muted" data-private>#{r.id} · {r.request.program}</div></td><td>{r.origin}<div data-private>{r.user_name||r.user_email}</div></td><td>{stageLabel[r.stage]}</td><td>{nextOwner(r)}</td><td>{r.links.length?r.links.every(p=>p.completed)?'PO complete':'Received quantity unavailable':'Not linked'}<div class="muted">Open detail for current stock</div></td></tr>{:else}<tr><td colspan="5">No matching print requests.</td></tr>{/each}</tbody></table>
