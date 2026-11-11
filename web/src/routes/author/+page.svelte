<script lang="ts">
	import { api } from '$lib/api';
	import { session } from '$lib/session.svelte';
	import { blankAddress, prefillAddress } from '$lib/address';
	import MyPrintRequests from '$lib/MyPrintRequests.svelte';
	let error = $state(''),
		busy = $state(false),
		slackOptIn = $state(false),
		initial = $state(25),
		deadline = $state('');
	let address = $state(prefillAddress(blankAddress(), session.me?.user?.address));
	let form = $state({ program: '', title: '', description: '', kind: 'sticker', file_url: '', quantity: 500 });
	const fields = [
		['first_name', 'First name'],
		['last_name', 'Last name'],
		['line_1', 'Street address'],
		['line_2', 'Address line 2'],
		['city', 'City'],
		['state', 'State / region'],
		['postal_code', 'Postal code'],
		['country', 'Country (two-letter code)'],
		['phone_number', 'Phone (required outside the US)']
	] as const;
	async function submit(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		busy = true;
		try {
			await api.post('/api/author/submissions', {
				...form,
				deadline,
				initial_quantity: initial,
				address: { ...address, country: address.country.trim().toUpperCase() },
				slack_opt_in: slackOptIn
			});
			window.location.href = '/requests?printed=1';
		} catch (e) {
			error = (e as Error).message;
			busy = false;
		}
	}
</script>

<svelte:head><title>YSWS author · Hack Club Merch</title></svelte:head>
<h1>Stock the warehouse</h1>
{#if !session.me?.roles.author}<p>
		This entry path is for YSWS authors recognized by their Hack Club Auth email. Ask HQ to update your author record if
		needed.
	</p>{:else}
	<p>
		Submit print-ready material for your YSWS. HQ reviews custom specifications and budget before ordering. No automated
		price or artwork checks are claimed for custom material.
	</p>
	<form class="card" onsubmit={submit} data-track="author_print_form">
		<div class="form-grid">
			<label>YSWS program<input required maxlength="200" bind:value={form.program} /></label><label
				>Item name<input required maxlength="200" bind:value={form.title} /></label
			><label
				>Kind<select bind:value={form.kind}
					><option value="sticker">Sticker</option><option value="poster">Poster</option><option value="card"
						>Card / flyer</option
					><option value="other">Other</option></select
				></label
			><label>Quantity<input type="number" required min="1" max="100000" bind:value={form.quantity} /></label>
		</div>
		<label>Print-ready file URL<input type="url" required bind:value={form.file_url} /></label><label
			>How will these be used? Include size and finish.<textarea required maxlength="5000" bind:value={form.description}
			></textarea></label
		><label>Program deadline<input required maxlength="2000" bind:value={deadline} /></label>
		<h2>Your first shipment</h2>
		<p>
			Created after warehouse receipt and HQ shop release. HQ reviews the parcel separately. Outside the US you pay its
			postage.
		</p>
		<label>First quantity<input type="number" required min="1" max={form.quantity} bind:value={initial} /></label>
		<div class="form-grid" data-private>
			{#each fields as [key, label]}<label
					>{label}<input
						required={!['line_2', 'state', 'phone_number'].includes(key) ||
							(key === 'phone_number' && address.country.toUpperCase() !== 'US')}
						bind:value={address[key]}
					/></label
				>{/each}
		</div>
		<p>Email at every stage. No printer order is placed on submission.</p>
		{#if session.me?.print_slack_available}<label class="inline"
				><input type="checkbox" name="print_slack_opt_in" bind:checked={slackOptIn} />Also send me Slack DMs (optional;
				off by default)</label
			>{/if}
		{#if error}<p class="error">{error}</p>{/if}<button disabled={busy}
			>{busy ? 'Submitting…' : 'Submit print request'}</button
		>
	</form>
	<MyPrintRequests />{/if}
