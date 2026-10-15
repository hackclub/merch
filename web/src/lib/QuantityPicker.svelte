<script lang="ts">
	// Quick-pick amounts in a row of square buttons (like the T-shirt sizes), then
	// "Custom" for any other amount via the stepper. The picked amount is dark.
	import { presetQuantities, quantityOptions } from './cart';
	import Stepper from './Stepper.svelte';
	import type { Item } from './types';
	let { item, value, onchange }: { item: Item; value: number; onchange: (n: number) => void } = $props();

	const presets = $derived(presetQuantities(item));
	let custom = $state(false);
	// An amount that isn't one of the presets is shown in the stepper.
	const showStepper = $derived(custom || (value > 0 && !presets.includes(value)));
</script>

<div class="amounts" role="group" aria-label="How many {item.name}">
	{#each presets as q (q)}
		<button type="button" class:picked={!showStepper && value === q} aria-pressed={!showStepper && value === q}
			onclick={() => ((custom = false), onchange(q))}>{q}</button>
	{/each}
	<button type="button" class:picked={showStepper} aria-pressed={showStepper} onclick={() => (custom = true)}>Custom</button>
</div>
{#if showStepper}
	<Stepper {value} max={item.max_per_request} options={quantityOptions(item)} label="Quantity of {item.name}" {onchange} />
{/if}
{#if value > 0}
	<button type="button" class="link" onclick={() => ((custom = false), onchange(0))}>Remove</button>
{/if}

<style>
	.amounts { display: flex; flex-wrap: wrap; gap: 0.35rem; }
	.amounts button {
		min-width: 3rem; height: 2.6rem; padding: 0 0.8rem;
		background: white; color: var(--dark); border: 1.5px solid var(--smoke); font-weight: 700;
	}
	.amounts button:hover { border-color: var(--dark); }
	.amounts button.picked { border-color: var(--dark); background: var(--dark); color: white; }
	.link { font-size: 0.9rem; }
</style>
