<script lang="ts">
	// −/+ quantity control clamped to an item's per-request max.
	let { value, max, onchange, label = 'Quantity' }: { value: number; max: number; onchange: (n: number) => void; label?: string } =
		$props();
</script>

<div class="stepper" role="group" aria-label={label}>
	<button type="button" class="secondary" aria-label="Fewer" disabled={value <= 0} onclick={() => onchange(value - 1)}>−</button>
	<input type="number" min="0" {max} {value} aria-label={label} oninput={(e) => onchange(+e.currentTarget.value)} />
	<button type="button" class="secondary" aria-label="More" disabled={value >= max} onclick={() => onchange(value + 1)}>+</button>
</div>

<style>
	.stepper { display: inline-flex; align-items: center; gap: 0.35rem; }
	.stepper button { width: 2.2rem; height: 2.2rem; padding: 0; font-size: 1.1rem; line-height: 1; }
	.stepper input { width: 3.5rem; text-align: center; appearance: textfield; -moz-appearance: textfield; }
	.stepper input::-webkit-inner-spin-button { appearance: none; -webkit-appearance: none; }
</style>
