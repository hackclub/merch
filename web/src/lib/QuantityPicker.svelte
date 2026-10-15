<script lang="ts">
	// Quick-pick amounts in a row of square buttons (like the T-shirt sizes), then
	// "Custom" to type any other amount. A typed amount must fit the item's pack
	// rules and limits (quantityProblem); if it doesn't, the rule shows under the box.
	import { presetQuantities, quantityProblem } from './cart';
	import type { Item } from './types';
	let { item, value, onchange }: { item: Item; value: number; onchange: (n: number) => void } = $props();

	const presets = $derived(presetQuantities(item));
	let custom = $state(false);
	let problem = $state('');
	// An amount that isn't one of the presets is shown in the box.
	const typing = $derived(custom || (value > 0 && !presets.includes(value)));

	function commit(e: Event) {
		const raw = (e.currentTarget as HTMLInputElement).value.trim();
		if (!raw) return;
		const n = Number(raw);
		problem = Number.isInteger(n) && n > 0 ? quantityProblem(item, n) : 'Type a whole number.';
		if (!problem) onchange(n);
	}
</script>

<div class="amounts" role="group" aria-label="How many {item.name}">
	{#each presets as q (q)}
		<button type="button" class:picked={!typing && value === q} aria-pressed={!typing && value === q}
			onclick={() => ((custom = false), (problem = ''), onchange(q))}>{q}</button>
	{/each}
	<button type="button" class:picked={typing} aria-pressed={typing} onclick={() => (custom = true)}>Custom</button>
</div>
{#if typing}
	<input class="custom" type="number" inputmode="numeric" min="1" max={item.max_per_request} placeholder="How many?"
		value={value > 0 && !presets.includes(value) ? value : ''} aria-label="How many {item.name}" aria-invalid={!!problem}
		onchange={commit} onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), commit(e))} />
{/if}
{#if value > 0}
	<button type="button" class="link" onclick={() => ((custom = false), (problem = ''), onchange(0))}>Remove</button>
{/if}
{#if typing && problem}<p class="problem">{problem}</p>{/if}

<style>
	.amounts { display: flex; flex-wrap: wrap; gap: 0.35rem; }
	.amounts button {
		min-width: 3rem; height: 2.6rem; padding: 0 0.8rem;
		background: white; color: var(--dark); border: 1.5px solid var(--smoke); font-weight: 700;
	}
	.amounts button:hover { border-color: var(--dark); }
	.amounts button.picked { border-color: var(--dark); background: var(--dark); color: white; }
	.custom { width: 7rem; height: 2.6rem; }
	.custom[aria-invalid='true'] { border-color: var(--red); }
	.link { font-size: 0.9rem; }
	.problem { flex-basis: 100%; margin: 0; color: var(--red); font-size: 0.9rem; }
</style>
