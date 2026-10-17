<script lang="ts">
	import { api, ApiError } from '$lib/api';
	import { renderMarkdown } from '$lib/markdown';
	import type { SwagRequest } from '$lib/types';

	// The admins' own note on a request, in Markdown. The requester never sees it.
	let { request }: { request: SwagRequest } = $props();

	let editing = $state(false);
	let preview = $state(false);
	let draft = $state('');
	let saving = $state(false);
	let error = $state('');
	const html = $derived(renderMarkdown(request.internal_note ?? ''));

	function edit() {
		draft = request.internal_note ?? '';
		preview = false;
		error = '';
		editing = true;
	}

	async function save() {
		saving = true;
		error = '';
		try {
			const r = await api.put<SwagRequest>(`/api/admin/requests/${request.id}/internal-note`, { internal_note: draft });
			request.internal_note = r.internal_note ?? '';
			editing = false;
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		}
		saving = false;
	}
</script>

<div class="internal-note" data-track="internal_note" data-private>
	{#if editing}
		<div class="row tabs">
			<button class="link" class:on={!preview} onclick={() => (preview = false)}>Write</button>
			<button class="link" class:on={preview} onclick={() => (preview = true)}>Preview</button>
			<span class="muted">Markdown · admins only</span>
		</div>
		{#if preview}
			<div class="md">{@html renderMarkdown(draft) || '<p class="muted">Nothing to preview.</p>'}</div>
		{:else}
			<!-- svelte-ignore a11y_autofocus -->
			<textarea rows="5" bind:value={draft} autofocus placeholder="Internal note (only admins see it). **Markdown** works: lists, links, `code`, tables."
				onkeydown={(e) => { if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') save(); if (e.key === 'Escape') editing = false; }}></textarea>
		{/if}
		{#if error}<p class="error">{error}</p>{/if}
		<div class="row">
			<button onclick={save} disabled={saving}>{saving ? 'Saving…' : 'Save note'}</button>
			<button class="secondary" onclick={() => (editing = false)} disabled={saving}>Cancel</button>
		</div>
	{:else if html}
		<div class="label muted">Internal note <button class="link" onclick={edit}>edit</button></div>
		<div class="md">{@html html}</div>
	{:else}
		<button class="link muted" onclick={edit}>+ internal note</button>
	{/if}
</div>

<style>
	.internal-note {
		margin-top: 0.4rem;
	}
	.label {
		font-family: var(--mono);
		text-transform: uppercase;
		font-size: 0.7rem;
	}
	.tabs .on {
		text-decoration: underline;
		font-weight: bold;
	}
	.md {
		background: var(--tile);
		padding: 0.4rem 0.6rem;
		max-width: 32rem;
		overflow-wrap: anywhere;
	}
	.md :global(:first-child) {
		margin-top: 0;
	}
	.md :global(:last-child) {
		margin-bottom: 0;
	}
	.md :global(p),
	.md :global(ul),
	.md :global(ol),
	.md :global(pre),
	.md :global(blockquote),
	.md :global(table) {
		margin: 0.4rem 0;
	}
	.md :global(ul),
	.md :global(ol) {
		padding-left: 1.2rem;
	}
	.md :global(code) {
		font-family: var(--mono);
		font-size: 0.85em;
	}
	.md :global(pre) {
		white-space: pre-wrap;
		background: #fff;
		padding: 0.4rem;
	}
	.md :global(blockquote) {
		border-left: 3px solid var(--red);
		padding-left: 0.6rem;
	}
	.md :global(img) {
		max-width: 100%;
	}
	.md :global(h1),
	.md :global(h2),
	.md :global(h3) {
		font-size: 1em;
		margin: 0.5rem 0 0.2rem;
	}
</style>
