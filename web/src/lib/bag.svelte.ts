import { browser } from '$app/environment';
import { parseCart, parseSizes, type Cart, type Sizes } from './cart';
import { parseNeed, parsePurpose, type StaffNeed } from './purpose';
import { cartChanges, sizeChanges, track } from './track';
import type { Purpose } from './types';

// The cart is shared by the catalog and product pages and survives reloads,
// so picking things on product pages and checking out on / just works.
const KEY = 'merch:cart';
const SIZES_KEY = 'merch:sizes';
const PURPOSE_KEY = 'merch:purpose';
const NEED_KEY = 'merch:staff_need';

export const bag = $state<{ cart: Cart; sizes: Sizes; purpose: Purpose | ''; need: StaffNeed }>({
	cart: browser ? parseCart(localStorage.getItem(KEY)) : {},
	sizes: browser ? parseSizes(localStorage.getItem(SIZES_KEY)) : {},
	purpose: browser ? parsePurpose(localStorage.getItem(PURPOSE_KEY)) : '',
	need: browser ? parseNeed(localStorage.getItem(NEED_KEY)) : 'swag'
});

// Every change to what's picked is logged (usage analytics), whichever page made it.
export function setCart(cart: Cart) {
	const total = Object.values(cart).reduce((n, q) => n + q, 0);
	for (const c of cartChanges(bag.cart, cart)) track('cart', { ...c, total });
	bag.cart = cart;
	if (browser) localStorage.setItem(KEY, JSON.stringify(cart));
}

export function setSizes(sizes: Sizes) {
	for (const c of sizeChanges(bag.sizes, sizes)) track('cart_size', c);
	bag.sizes = sizes;
	if (browser) localStorage.setItem(SIZES_KEY, JSON.stringify(sizes));
}

export function setPurpose(purpose: Purpose | '') {
	if (purpose !== bag.purpose) track('purpose', { value: purpose, from: bag.purpose });
	bag.purpose = purpose;
	if (browser) localStorage.setItem(PURPOSE_KEY, purpose);
}

// Staff choose between requesting swag and getting postcards or posters printed.
export function setNeed(need: StaffNeed) {
	if (need !== bag.need) track('staff_need', { value: need, from: bag.need });
	bag.need = need;
	if (browser) localStorage.setItem(NEED_KEY, need);
}
