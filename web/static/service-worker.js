// The site no longer uses a service worker. Browsers that installed the old one
// keep checking this URL for updates, so this version removes itself: it deletes
// the old caches, unregisters, and reloads open tabs straight from the network.
// It has no fetch handler, so it never answers a request. Keep this file until
// old installs have had time to update (at least a few months from 2026-10-17).
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => {
	event.waitUntil(
		(async () => {
			const keys = await caches.keys();
			await Promise.all(keys.map((k) => caches.delete(k)));
			await self.registration.unregister();
			const tabs = await self.clients.matchAll({ type: 'window' });
			await Promise.all(tabs.map((t) => t.navigate(t.url).catch(() => {})));
		})()
	);
});
