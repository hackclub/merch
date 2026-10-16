<script lang="ts">
	import { onMount } from 'svelte';
	import { afterNavigate } from '$app/navigation';
	import { trackPage } from '$lib/track';
	import { session, loadSession, logout } from '$lib/session.svelte';
	import '../app.css';

	let { children } = $props();
	onMount(loadSession);
	// Usage analytics: page views and time on each page (see $lib/track).
	afterNavigate(({ to }) => to && trackPage(to.url.pathname));
</script>

<header>
	<div class="who">
		{#if session.me?.user}
			{#if session.me.roles.admin}<a class="button admin" href="/admin">Admin</a>{/if}
			<span>{session.me.user.name || session.me.user.email}</span>
			<button class="link" onclick={logout}>Sign out</button>
		{:else if session.loaded}
			<a class="button" href="/auth/login" data-sveltekit-reload>Sign in<span class="wide">{' '}with Hack Club</span></a>
		{/if}
	</div>
</header>

<main>
	{#if session.loaded}
		{@render children()}
	{:else}
		<p class="muted">Loading…</p>
	{/if}
</main>
