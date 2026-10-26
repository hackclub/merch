import { track, visitId } from './track';

export class ApiError extends Error {
	constructor(
		public status: number,
		message: string,
		// The whole error body, for errors that carry more than a message (the 3 lb refusal's weights).
		public data?: unknown
	) {
		super(message);
	}
}

declare global {
	// app.html starts fetching the catalog with the HTML, before this code has loaded.
	var __catalog: Promise<Response> | undefined;
}

/** The catalog fetch app.html started, the first time it's asked for; undefined after (or if it failed). */
async function early(method: string, path: string): Promise<Response | undefined> {
	if (method !== 'GET' || path !== '/api/catalog' || !globalThis.__catalog) return undefined;
	const p = globalThis.__catalog;
	globalThis.__catalog = undefined;
	return p.catch(() => undefined);
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
	const init: RequestInit & { headers: Record<string, string> } = {
		method,
		credentials: 'same-origin',
		headers: { Accept: 'application/json', 'X-Merch-Visit': visitId() }
	};
	if (body instanceof Blob) {
		// A file goes as its own bytes: base64 in JSON is a third bigger, and the server holds it twice.
		init.headers['Content-Type'] = body.type;
		init.body = body;
	} else if (body !== undefined) {
		init.headers['Content-Type'] = 'application/json';
		init.body = JSON.stringify(body);
	}
	const res = (await early(method, path)) ?? (await fetch(path, init));
	const text = await res.text();
	const data = text ? JSON.parse(text) : undefined;
	if (!res.ok) {
		// The path without its query (address searches carry what people typed).
		track('api_error', { method, api: path.split('?')[0], status: res.status, error: String(data?.error ?? res.statusText).slice(0, 200) });
		throw new ApiError(res.status, data?.error ?? res.statusText, data);
	}
	return data as T;
}

export const api = {
	get: <T>(path: string) => request<T>('GET', path),
	post: <T>(path: string, body: unknown = {}) => request<T>('POST', path, body),
	put: <T>(path: string, body: unknown) => request<T>('PUT', path, body),
	/** POSTs a file as its own bytes and type (only /api/print/files takes these). */
	upload: <T>(path: string, file: Blob) => request<T>('POST', path, file)
};

/** Base64 of a file's bytes, for JSON uploads (the API only accepts application/json). */
export const fileToBase64 = (file: Blob): Promise<string> =>
	new Promise((resolve, reject) => {
		const r = new FileReader();
		r.onload = () => resolve(String(r.result).slice(String(r.result).indexOf(',') + 1));
		r.onerror = () => reject(r.error);
		r.readAsDataURL(file);
	});

export const formatCents = (cents: number) =>
	new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(cents / 100);

export const isInternational = (country: string) => country.trim() !== '' && country.trim().toUpperCase() !== 'US';

export const statusLabel: Record<string, string> = {
	awaiting_payment: 'Waiting on shipping payment',
	pending: 'In review',
	dispatched: 'Sent to warehouse',
	rejected: 'Rejected',
	cancelled: 'Cancelled',
	submitted: 'Submitted',
	approved: 'Approved',
	printing: 'Printing',
	stocked: 'Stocked in warehouse'
};
