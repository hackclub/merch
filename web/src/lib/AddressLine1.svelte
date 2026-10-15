<script lang="ts">
	// Address line 1 with suggestions from the server's Google Places proxy.
	// Picking one fills the rest of the address. Without a Places key on the
	// server (404) it's a plain input.
	import { api, ApiError } from './api';
	import { createSearcher, moveHighlight, newSessionToken, type AddressSuggestion } from './addressSearch';
	import type { Address } from './types';

	let { value = $bindable(), onpick }: { value: string; onpick: (place: Address) => void } = $props();

	let off = $state(false);
	let open = $state(false);
	let suggestions = $state<AddressSuggestion[]>([]);
	let highlight = $state(-1);
	let session = newSessionToken();
	const listId = `address-suggestions-${Math.random().toString(36).slice(2)}`;

	const search = createSearcher((q) =>
		api
			.get<{ suggestions: AddressSuggestion[] }>(`/api/address/suggest?q=${encodeURIComponent(q)}&session=${session}`)
			.then((r) => r.suggestions)
	);

	async function oninput() {
		if (off) return;
		try {
			const r = await search(value);
			if (r === null) return;
			suggestions = r;
			highlight = -1;
			open = r.length > 0;
		} catch (e) {
			if (e instanceof ApiError && e.status === 404) off = true;
			suggestions = [];
			open = false;
		}
	}

	async function pick(s: AddressSuggestion) {
		open = false;
		suggestions = [];
		value = s.main;
		try {
			onpick(await api.get<Address>(`/api/address/place?id=${encodeURIComponent(s.place_id)}&session=${session}`));
		} catch {
			// Keep the street they picked; they can fill the rest by hand.
		}
		session = newSessionToken();
	}

	function onkeydown(e: KeyboardEvent) {
		if (!open) return;
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
			e.preventDefault();
			highlight = moveHighlight(highlight, e.key === 'ArrowDown' ? 1 : -1, suggestions.length);
		} else if (e.key === 'Enter' && highlight >= 0) {
			e.preventDefault();
			pick(suggestions[highlight]);
		} else if (e.key === 'Escape') {
			open = false;
		}
	}
</script>

<div class="wrap">
	<label>Address line 1
		<input required bind:value {oninput} {onkeydown} onblur={() => (open = false)} autocomplete="address-line1"
			role="combobox" aria-autocomplete="list" aria-expanded={open} aria-controls={listId}
			aria-activedescendant={open && highlight >= 0 ? `${listId}-${highlight}` : undefined} />
	</label>
	{#if open}
		<ul id={listId} role="listbox">
			{#each suggestions as s, i (s.place_id)}
				<!-- mousedown, not click: it fires before the input's blur closes the list. -->
				<li id="{listId}-{i}" role="option" aria-selected={i === highlight} class:on={i === highlight}
					onmousedown={(e) => { e.preventDefault(); pick(s); }}>
					<span class="main">{s.main}</span> <span class="secondary">{s.secondary}</span>
				</li>
			{/each}
			<li class="credit" aria-hidden="true">Powered by Google</li>
		</ul>
	{/if}
</div>

<style>
	.wrap { position: relative; }
	ul {
		position: absolute; z-index: 10; top: 100%; left: 0; right: 0; margin: -1.5px 0 0; padding: 0;
		list-style: none; background: white; border: 1.5px solid var(--dark); min-width: 18rem;
	}
	li[role='option'] { padding: 0.55rem 0.7rem; cursor: pointer; line-height: 1.3; }
	li.on, li[role='option']:hover { background: var(--tile); }
	.secondary { color: var(--muted); font-size: 0.85rem; }
	.credit {
		padding: 0.3rem 0.7rem; text-align: right; color: var(--muted); border-top: 1.5px solid var(--smoke);
		font-family: var(--mono); font-size: 0.65rem; text-transform: uppercase; letter-spacing: 0.02em;
	}
</style>
