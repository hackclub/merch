<script lang="ts">
	// One button per size of an item; the picked one is highlighted.
	import type { Variant } from './types';
	let { variants, value, onchange, label = 'Size' }: { variants: Variant[]; value: string | undefined; onchange: (sku: string) => void; label?: string } =
		$props();
</script>

<div class="sizes" role="radiogroup" aria-label={label}>
	{#each variants as v (v.sku)}
		<button type="button" role="radio" aria-checked={v.sku === value} class:picked={v.sku === value} onclick={() => onchange(v.sku)}>{v.label}</button>
	{/each}
</div>

<style>
	.sizes { display: flex; gap: 0.35rem; flex-wrap: wrap; }
	.sizes button {
		min-width: 3.25rem; height: 2.75rem; padding: 0 0.75rem;
		background: white; color: var(--dark); border: 1.5px solid var(--smoke);
	}
	.sizes button:hover { border-color: var(--dark); }
	.sizes button.picked { border-color: var(--dark); background: var(--dark); color: white; }
</style>
