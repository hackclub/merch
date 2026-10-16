<!-- The card field: Stripe's Payment Element, cards only, styled like the form around it.
	 Card numbers go from Stripe's iframe straight to Stripe. The billing address is the
	 shipping address, so it asks for just the number, expiry and CVC. -->
<script lang="ts">
	import { onMount } from 'svelte';
	import { loadStripe, type Stripe, type StripeElements } from '@stripe/stripe-js';
	import { billingDetails } from './payment';
	import type { Address } from './types';

	let { stripeKey, amountCents, billing }: { stripeKey: string; amountCents: number; billing: Address } = $props();

	let box: HTMLDivElement;
	let stripe: Stripe | null = null;
	let elements = $state.raw<StripeElements | null>(null);
	let loadError = $state('');

	onMount(() => {
		let destroyed = false;
		let field: { destroy(): void } | null = null;
		loadStripe(stripeKey)
			.then((s) => {
				if (destroyed || !s) return;
				stripe = s;
				const els = s.elements({
					mode: 'payment',
					amount: amountCents,
					currency: 'usd',
					allowedPaymentMethodTypes: ['card'], // the PaymentIntent the server makes says the same
					fonts: [{ family: 'Phantom Sans', src: 'url(https://assets.hackclub.com/fonts/Phantom_Sans_0.7/Regular.woff2)', weight: '400' }],
					appearance: {
						theme: 'stripe',
						variables: {
							fontFamily: "'Phantom Sans', system-ui, sans-serif",
							fontSizeBase: '16px',
							colorPrimary: '#ec3750',
							colorDanger: '#ec3750',
							borderRadius: '0px'
						}
					}
				});
				const pe = els.create('payment', {
					layout: 'tabs', // with one method, just its fields
					fields: { billingDetails: { address: 'never' } },
					// Just the card: no wallets, and no Link (its bank and Klarna options ride on cards).
					wallets: { applePay: 'never', googlePay: 'never', link: 'never' },
					terms: { card: 'never' }
				});
				pe.on('loaderror', (e) => {
					console.error('stripe payment element', e.error);
					loadError = "The card form didn't load. Reload the page to try again.";
				});
				pe.mount(box);
				field = pe;
				elements = els;
			})
			.catch((e) => (console.error('stripe', e), loadError = "The card form didn't load. Check your connection and reload."));
		return () => {
			destroyed = true;
			field?.destroy();
		};
	});

	$effect(() => {
		elements?.update({ amount: amountCents });
	});

	export async function check(): Promise<string | null> {
		if (!elements) return loadError || 'The card form is still loading.';
		const { error } = await elements.submit();
		return error ? error.message || 'Check your card details.' : null;
	}

	export function setAmount(cents: number) {
		elements?.update({ amount: cents });
	}

	export async function confirm(clientSecret: string, returnUrl: string) {
		if (!stripe || !elements) return { error: { message: 'The card form is still loading.' } };
		return stripe.confirmPayment({
			elements,
			clientSecret,
			confirmParams: { return_url: returnUrl, payment_method_data: { billing_details: billingDetails(billing) } },
			redirect: 'if_required'
		});
	}
</script>

<div class="card-field" bind:this={box} data-private data-track="card-field"></div>
{#if loadError}<p class="error">{loadError}</p>{/if}

<style>
	.card-field { margin: 0.5rem 0 1rem; min-height: 3rem; }
</style>
