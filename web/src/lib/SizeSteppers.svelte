<script lang="ts">
	// One quantity per size of an item ("S − 1 +  M − 2 +"), so a request can mix sizes.
	// The item's per-request max counts every size together.
	import type { Sizes } from './cart';
	import Stepper from './Stepper.svelte';
	import type { Item } from './types';
	let { item, sizes, onchange }: { item: Item; sizes: Sizes; onchange: (sku: string, n: number) => void } = $props();

	const counts = $derived(sizes[item.id] ?? {});
	const total = $derived(Object.values(counts).reduce((a, b) => a + b, 0));
</script>

<div class="sizes" role="group" aria-label="Sizes of {item.name}">
	{#each item.variants as v (v.sku)}
		{@const n = counts[v.sku] ?? 0}
		<div class="size" class:on={n > 0}>
			<span>{v.label}</span>
			<Stepper value={n} max={item.max_per_request - total + n} label="{item.name}, size {v.label}" onchange={(q) => onchange(v.sku, q)} />
		</div>
	{/each}
</div>

<style>
	/* Label over a compact stepper, so S M L XL sit in one row. */
	.sizes { display: flex; flex-wrap: wrap; gap: 0.5rem 0.75rem; }
	.size { display: grid; gap: 0.2rem; }
	.size span { font-weight: 700; font-size: 0.9rem; color: var(--muted); }
	.size.on span { color: var(--dark); }
	.size :global(.stepper button) { width: 2.1rem; }
	.size :global(.stepper input) { width: 2.2rem; }
</style>
