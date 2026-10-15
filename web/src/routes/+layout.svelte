<script lang="ts">
	import { onMount } from 'svelte';
	import { afterNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { trackPage } from '$lib/track';
	import { session, loadSession, logout } from '$lib/session.svelte';
	import '../app.css';

	let { children } = $props();
	onMount(loadSession);
	// Usage analytics: page views and time on each page (see $lib/track).
	afterNavigate(({ to }) => to && trackPage(to.url.pathname));

	const links = $derived([
		{ href: '/', label: 'Swag', show: true },
		{ href: '/requests', label: 'My requests', show: !!session.me?.user },
		{ href: '/author', label: 'YSWS author', show: !!session.me?.roles.author },
		{ href: '/admin', label: 'Admin', show: !!session.me?.roles.admin }
	]);
</script>

<header>
	<a class="brand" href="/" aria-label="Hack Club merch"><span class="box">Hack Club</span><span class="sub">merch</span></a>
	<nav>
		{#each links.filter((l) => l.show) as l (l.href)}
			<a href={l.href} class:active={page.url.pathname === l.href}>{l.label}</a>
		{/each}
	</nav>
	<div class="who">
		{#if session.me?.user}
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
