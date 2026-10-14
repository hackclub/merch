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
		min-width: 2.6rem; height: 2.2rem; padding: 0 0.6rem; border-radius: 8px;
		background: white; color: var(--dark); border: 2px solid var(--smoke);
	}
	.sizes button.picked { border-color: var(--red); color: var(--red); }
</style>
