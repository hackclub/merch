import { describe, expect, it, vi } from 'vitest';
import { readFileSync } from 'node:fs';

// web/static/service-worker.js replaces the old offline-cache worker with one that
// removes itself, so browsers that installed the old one stop serving from it.
const src = readFileSync('static/service-worker.js', 'utf8');

function run() {
	const listeners: Record<string, (e: unknown) => void> = {};
	const deleted: string[] = [];
	const navigated: string[] = [];
	const self = {
		addEventListener: (name: string, fn: (e: unknown) => void) => (listeners[name] = fn),
		skipWaiting: vi.fn(),
		registration: { unregister: vi.fn(async () => true) },
		clients: {
			matchAll: async () => [{ url: 'https://merch.hackclub.com/', navigate: async (u: string) => navigated.push(u) }]
		}
	};
	const caches = { keys: async () => ['shell-1', 'shell-2'], delete: async (k: string) => deleted.push(k) };
	new Function('self', 'caches', src)(self, caches);
	return { listeners, self, deleted, navigated };
}

describe('service worker kill switch', () => {
	it('never answers requests', () => {
		expect(run().listeners.fetch).toBeUndefined();
	});

	it('takes over at once, clears caches, unregisters and reloads tabs', async () => {
		const { listeners, self, deleted, navigated } = run();
		listeners.install({});
		expect(self.skipWaiting).toHaveBeenCalled();
		let done: Promise<unknown> = Promise.resolve();
		listeners.activate({ waitUntil: (p: Promise<unknown>) => (done = p) });
		await done;
		expect(deleted).toEqual(['shell-1', 'shell-2']);
		expect(self.registration.unregister).toHaveBeenCalled();
		expect(navigated).toEqual(['https://merch.hackclub.com/']);
	});
});
