import { browser } from '$app/environment';
import { parseCart, parseSizes, type Cart, type Sizes } from './cart';

// The cart is shared by the catalog and product pages and survives reloads,
// so picking things on product pages and checking out on / just works.
const KEY = 'merch:cart';
const SIZES_KEY = 'merch:sizes';

export const bag = $state<{ cart: Cart; sizes: Sizes }>({
	cart: browser ? parseCart(localStorage.getItem(KEY)) : {},
	sizes: browser ? parseSizes(localStorage.getItem(SIZES_KEY)) : {}
});

export function setCart(cart: Cart) {
	bag.cart = cart;
	if (browser) localStorage.setItem(KEY, JSON.stringify(cart));
}

export function setSizes(sizes: Sizes) {
	bag.sizes = sizes;
	if (browser) localStorage.setItem(SIZES_KEY, JSON.stringify(sizes));
}
