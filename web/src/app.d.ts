// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
declare global {
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
