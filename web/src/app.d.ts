// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
declare global {
	const __MERCH_RELEASE__: string; // the build's time (vite.config.ts), stamped on usage events
	namespace App {
		// interface Error {}
		// interface Locals {}
		// interface PageData {}
		interface PageState {
			payError?: string; // why the card on the request form didn't go through
		}
		// interface Platform {}
	}
}

export {};
