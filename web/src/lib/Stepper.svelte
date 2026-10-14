<script lang="ts">
	// −/+ quantity control clamped to an item's per-request max. With options
	// (items sold in packs), it steps through those quantities and picks from a list.
	let {
		value,
		max,
		onchange,
		label = 'Quantity',
		options
	}: { value: number; max: number; onchange: (n: number) => void; label?: string; options?: number[] } = $props();

	const at = $derived(options ? options.indexOf(value) : -1);
	const step = (dir: 1 | -1) => (options ? (options[at + dir] ?? value) : value + dir);
</script>

<div class="stepper" role="group" aria-label={label}>
	<button type="button" class="secondary" aria-label="Fewer" disabled={value <= 0} onclick={() => onchange(step(-1))}>−</button>
	{#if options}
		<select {value} aria-label={label} onchange={(e) => onchange(+e.currentTarget.value)}>
			{#each options as n}<option value={n}>{n}</option>{/each}
		</select>
	{:else}
		<input type="number" min="0" {max} {value} aria-label={label} oninput={(e) => onchange(+e.currentTarget.value)} />
	{/if}
	<button type="button" class="secondary" aria-label="More" disabled={options ? at === options.length - 1 : value >= max}
		onclick={() => onchange(step(1))}>+</button>
</div>

<style>
	.stepper { display: inline-flex; align-self: flex-start; align-items: stretch; border: 1.5px solid var(--smoke); height: 2.6rem; }
	.stepper button { width: 2.5rem; padding: 0; font-size: 1.1rem; line-height: 1; background: white; color: var(--dark); }
	.stepper button:disabled { opacity: 0.25; }
	.stepper :is(input, select) { border: 0; font-family: var(--mono); font-size: 0.85rem; }
	.stepper input { width: 3.5rem; text-align: center; appearance: textfield; -moz-appearance: textfield; }
	.stepper select { width: auto; min-width: 4.5rem; }
	.stepper input::-webkit-inner-spin-button { appearance: none; -webkit-appearance: none; }
</style>
