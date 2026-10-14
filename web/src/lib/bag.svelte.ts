import { browser } from '$app/environment';
import { parseCart, type Cart } from './cart';

// The cart is shared by the catalog and product pages and survives reloads,
// so picking things on product pages and checking out on / just works.
const KEY = 'merch:cart';

export const bag = $state<{ cart: Cart }>({ cart: browser ? parseCart(localStorage.getItem(KEY)) : {} });

export function setCart(cart: Cart) {
	bag.cart = cart;
	if (browser) localStorage.setItem(KEY, JSON.stringify(cart));
}
