import { afterEach, describe, expect, it, vi } from 'vitest';
import { api, ApiError, formatCents, isInternational } from './api';

afterEach(() => vi.unstubAllGlobals());

function stubFetch(status: number, body: unknown) {
	const fn = vi.fn().mockResolvedValue(
		new Response(body === undefined ? null : JSON.stringify(body), {
			status,
			headers: { 'Content-Type': 'application/json' }
		})
	);
	vi.stubGlobal('fetch', fn);
	return fn;
}

describe('api', () => {
	it('sends JSON with same-origin credentials', async () => {
		const fetch = stubFetch(201, { id: 7 });
		const out = await api.post<{ id: number }>('/api/requests', { a: 1 });
		expect(out.id).toBe(7);
		const [url, init] = fetch.mock.calls[0];
		expect(url).toBe('/api/requests');
		expect(init.method).toBe('POST');
		expect(init.credentials).toBe('same-origin');
		expect(init.headers['Content-Type']).toBe('application/json');
		expect(JSON.parse(init.body)).toEqual({ a: 1 });
	});

	it('uploads a file as its own bytes and type, not base64 JSON', async () => {
		const fetch = stubFetch(201, { url: 'https://cdn.example/1/a.png' });
		const file = new Blob([new Uint8Array([137, 80, 78, 71])], { type: 'image/png' });
		const out = await api.upload<{ url: string }>('/api/print/files?product=postcard_4x6', file);
		expect(out.url).toBe('https://cdn.example/1/a.png');
		const [, init] = fetch.mock.calls[0];
		expect(init.method).toBe('POST');
		expect(init.headers['Content-Type']).toBe('image/png');
		expect(init.body).toBe(file);
	});

	it('throws ApiError carrying the server message', async () => {
		stubFetch(422, { error: 'you requested swag recently' });
		await expect(api.get('/api/requests')).rejects.toEqual(new ApiError(422, 'you requested swag recently', { error: 'you requested swag recently' }));
	});

	it('keeps the error body, for 422s that carry more than a message', async () => {
		stubFetch(422, { error: 'too heavy', weight_oz: 60, limit_oz: 48 });
		const err = (await api.post('/api/shipping/estimate', {}).catch((e) => e)) as ApiError;
		expect(err).toBeInstanceOf(ApiError);
		expect(err.data).toEqual({ error: 'too heavy', weight_oz: 60, limit_oz: 48 });
	});

	it('takes over the catalog fetch app.html started, once', async () => {
		const early = new Response(JSON.stringify({ items: [1] }), { status: 200 });
		vi.stubGlobal('__catalog', Promise.resolve(early));
		const fetch = stubFetch(200, { items: [2] });
		expect(await api.get('/api/catalog')).toEqual({ items: [1] });
		expect(fetch).not.toHaveBeenCalled();
		// Later loads (another page, a refresh of the data) fetch as usual.
		expect(await api.get('/api/catalog')).toEqual({ items: [2] });
		expect(fetch).toHaveBeenCalledTimes(1);
	});

	it('fetches again when the early catalog fetch failed', async () => {
		vi.stubGlobal('__catalog', Promise.reject(new TypeError('Failed to fetch')));
		const fetch = stubFetch(200, { items: [2] });
		expect(await api.get('/api/catalog')).toEqual({ items: [2] });
		expect(fetch).toHaveBeenCalledTimes(1);
	});

	it('handles empty 204 responses', async () => {
		stubFetch(204, undefined);
		await expect(api.post('/auth/logout', {})).resolves.toBeUndefined();
	});
});

describe('helpers', () => {
	it('formats cents as USD', () => {
		expect(formatCents(1500)).toBe('$15.00');
		expect(formatCents(0)).toBe('$0.00');
	});
	it('detects international addresses', () => {
		expect(isInternational('US')).toBe(false);
		expect(isInternational('us')).toBe(false);
		expect(isInternational('CA')).toBe(true);
		expect(isInternational('')).toBe(false);
	});
});
