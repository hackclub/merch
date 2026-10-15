import { browser } from '$app/environment';
import { parseCart, parseSizes, type Cart, type Sizes } from './cart';
import { parsePurpose } from './purpose';
import type { Purpose } from './types';

// The cart is shared by the catalog and product pages and survives reloads,
// so picking things on product pages and checking out on / just works.
const KEY = 'merch:cart';
const SIZES_KEY = 'merch:sizes';
const PURPOSE_KEY = 'merch:purpose';

export const bag = $state<{ cart: Cart; sizes: Sizes; purpose: Purpose | '' }>({
	cart: browser ? parseCart(localStorage.getItem(KEY)) : {},
	sizes: browser ? parseSizes(localStorage.getItem(SIZES_KEY)) : {},
	purpose: browser ? parsePurpose(localStorage.getItem(PURPOSE_KEY)) : ''
});

export function setCart(cart: Cart) {
	bag.cart = cart;
	if (browser) localStorage.setItem(KEY, JSON.stringify(cart));
}

export function setSizes(sizes: Sizes) {
	bag.sizes = sizes;
	if (browser) localStorage.setItem(SIZES_KEY, JSON.stringify(sizes));
}

export function setPurpose(purpose: Purpose | '') {
	bag.purpose = purpose;
	if (browser) localStorage.setItem(PURPOSE_KEY, purpose);
}
