<script lang="ts">
	import { formatCents } from '$lib/api';
	import { bars, KINDS, offsetText, type Dashboard } from '$lib/dashboard';

	// What shipping requests costs Hack Club: spend by day and kind, the month's
	// projection, and whether international postage payments cover their postage.
	let { data, days = $bindable(), onrefresh, refreshing }: { data: Dashboard; days: number; onrefresh: () => void; refreshing: boolean } = $props();

	const H = 140;
	const s = $derived(data.summary);
	const chart = $derived(bars(s.days, H));
	const step = $derived(Math.max(1, Math.ceil(chart.length / 10))); // label at most ~10 days
	const monthName = $derived(new Date(`${s.month.month}-15T12:00:00Z`).toLocaleDateString('en-US', { month: 'long', timeZone: 'UTC' }));
	const share = (cents: number) => (s.totals.total_cents ? Math.round((cents / s.totals.total_cents) * 100) : 0);
</script>

<section class="dash" data-track="dashboard">
	<div class="head">
		<div class="row">
			{#each [[7, '7 days'], [30, '30 days'], [90, '90 days'], [0, 'All time']] as [d, label]}
				<button class="chip" class:on={days === d} onclick={() => (days = d as number)}>{label}</button>
			{/each}
		</div>
		<button class="link" disabled={refreshing} onclick={onrefresh}
			title="Ask mail.hackclub.com for tracking and what it charged (postage once mailed) for shipped requests that don't have it yet">
			{refreshing ? 'Pulling…' : 'Pull costs from warehouse'}
		</button>
	</div>

	<div class="kpis">
		<div class="kpi">
			<span class="label">Spent · {days ? `${days} days` : 'all time'}</span>
			<strong>{formatCents(s.totals.total_cents)}</strong>
			<span class="muted">{s.totals.requests} shipped · {s.totals.requests ? formatCents(Math.round(s.totals.total_cents / s.totals.requests)) : '$0'} each</span>
		</div>
		<div class="kpi">
			<span class="label">{monthName} so far</span>
			<strong>{formatCents(s.month.spent_cents)}</strong>
			<span class="muted">{s.month.requests} shipped</span>
		</div>
		<div class="kpi" title="The month so far, plus the last 7 days' average ({formatCents(s.month.daily_rate_cents)}/day) for each of the {s.month.days_left} days left">
			<span class="label">{monthName} projected</span>
			<strong>{formatCents(s.month.projected_cents)}</strong>
			<span class="muted">at {formatCents(s.month.daily_rate_cents)}/day for {s.month.days_left} more days</span>
		</div>
		<div class="kpi" title="Pending and awaiting-payment requests, at today's estimates">
			<span class="label">In the queue</span>
			<strong>{formatCents(data.queue.total_cents)}</strong>
			<span class="muted">{data.queue.requests} waiting to ship</span>
		</div>
	</div>

	<div class="split">
		<div class="chart">
			<svg viewBox="0 0 {chart.length * 10} {H}" preserveAspectRatio="none" role="img" aria-label="Spend per day">
				{#each chart as b, i (b.day)}
					<g><title>{b.title}</title>
						<rect x={i * 10} y="0" width="10" height={H} class="hit" />
						{#each b.segments as seg}
							{#if seg.h > 0}<rect x={i * 10 + 1.5} y={seg.y} width="7" height={seg.h} class={seg.kind} />{/if}
						{/each}
					</g>
				{/each}
			</svg>
			<div class="axis">
				{#each chart as b, i (b.day)}<span>{i % step === 0 || i === chart.length - 1 ? b.label : ''}</span>{/each}
			</div>
		</div>
		<table class="breakdown">
			<tbody>
				{#each KINDS as k}
					<tr><td><i class="swatch {k}"></i>{k}</td><td>{formatCents(s.totals[`${k}_cents`])}</td><td class="muted">{share(s.totals[`${k}_cents`])}%</td></tr>
				{/each}
				<tr><td>Total</td><td>{formatCents(s.totals.total_cents)}</td><td></td></tr>
			</tbody>
		</table>
	</div>
	<p class="muted small">
		Items and labor are mail.hackclub.com's charges where it has them, else today's unit costs and AGH's rates; packing is always estimated.
		Postage is the label's price for {s.totals.postage_actual} of {s.totals.requests} shipped requests, and the typical postage of past packages like it for the rest.
	</p>

	<h3>International: do payments cover postage?</h3>
	<p class:short={s.international.net_cents < 0} class="verdict">{offsetText(s.international)}</p>
	{#if s.international.requests}
		<table class="offset">
			<thead><tr><th>Country</th><th>Shipped</th><th>Paid</th><th>Postage</th><th>Net</th><th>Rest of cost</th></tr></thead>
			<tbody>
				{#each s.by_country as o (o.country)}
					<tr>
						<td>{o.country}</td><td>{o.requests}</td><td>{formatCents(o.collected_cents)}</td>
						<td title="{o.postage_actual} of {o.requests} from the label's real price">{formatCents(o.postage_cents)}{o.postage_actual < o.requests ? '*' : ''}</td>
						<td class:neg={o.net_cents < 0}>{o.net_cents < 0 ? '−' : '+'}{formatCents(Math.abs(o.net_cents))}</td>
						<td class="muted">{formatCents(o.other_cents)}</td>
					</tr>
				{/each}
			</tbody>
		</table>
		<p class="muted small">* includes estimated postage. "Rest of cost" is items, labor and packing for those packages, which Hack Club pays wherever they go.</p>
	{/if}
</section>

<style>
	.dash { border: 1.5px solid var(--smoke); padding: 1rem 1.25rem; margin-bottom: 1.5rem; }
	.head { display: flex; justify-content: space-between; align-items: center; gap: 1rem; flex-wrap: wrap; margin-bottom: 1rem; }
	.chip { background: white; color: var(--dark); border: 1.5px solid var(--smoke); padding: 0.35rem 0.7rem; font-size: 0.75rem; }
	.chip.on { background: var(--dark); color: white; border-color: var(--dark); }
	.kpis { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 0.75rem; margin-bottom: 1rem; }
	.kpi { display: grid; gap: 0.15rem; background: var(--snow); padding: 0.75rem 0.9rem; }
	.kpi strong { font-size: 1.6rem; line-height: 1.1; }
	.kpi .muted { font-size: 0.8rem; }
	.split { display: grid; grid-template-columns: 1fr auto; gap: 1.5rem; align-items: end; }
	@media (max-width: 800px) { .split { grid-template-columns: 1fr; } }
	svg { width: 100%; height: 140px; display: block; background: var(--snow); }
	.hit { fill: transparent; }
	g:hover .hit { fill: var(--smoke); }
	.items, .swatch.items { fill: var(--dark); background: var(--dark); }
	.labor, .swatch.labor { fill: var(--muted); background: var(--muted); }
	.packing, .swatch.packing { fill: #c8c8cf; background: #c8c8cf; }
	.postage, .swatch.postage { fill: var(--red); background: var(--red); }
	.axis { display: flex; font-family: var(--mono); font-size: 0.65rem; color: var(--muted); }
	.axis span { flex: 1; white-space: nowrap; overflow: visible; }
	.breakdown { width: auto; }
	.breakdown td { padding: 0.15rem 0.75rem 0.15rem 0; border: 0; font-size: 0.9rem; }
	.breakdown td:first-child { text-transform: capitalize; }
	.breakdown td:nth-child(2) { text-align: right; font-variant-numeric: tabular-nums; }
	.breakdown tr:last-child td { font-weight: 700; border-top: 1px solid var(--smoke); }
	.swatch { display: inline-block; width: 0.7rem; height: 0.7rem; margin-right: 0.4rem; vertical-align: -0.05rem; }
	.small { font-size: 0.8rem; margin: 0.5rem 0 0; }
	h3 { font-size: 0.95rem; margin: 1.5rem 0 0.4rem; }
	.verdict { font-weight: 700; color: #1a9e78; margin: 0 0 0.5rem; }
	.verdict.short { color: var(--red); }
	.offset { width: auto; font-size: 0.9rem; }
	.offset td, .offset th { padding: 0.2rem 1rem 0.2rem 0; text-align: right; }
	.offset td:first-child, .offset th:first-child { text-align: left; }
	.neg { color: var(--red); }
</style>
