import { track, visitId } from './track';

export class ApiError extends Error {
	constructor(
		public status: number,
		message: string
	) {
		super(message);
	}
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
	const init: RequestInit & { headers: Record<string, string> } = {
		method,
		credentials: 'same-origin',
		headers: { Accept: 'application/json', 'X-Merch-Visit': visitId() }
	};
	if (body !== undefined) {
		init.headers['Content-Type'] = 'application/json';
		init.body = JSON.stringify(body);
	}
	const res = await fetch(path, init);
	const text = await res.text();
	const data = text ? JSON.parse(text) : undefined;
	if (!res.ok) {
		// The path without its query (address searches carry what people typed).
		track('api_error', { method, api: path.split('?')[0], status: res.status, error: String(data?.error ?? res.statusText).slice(0, 200) });
		throw new ApiError(res.status, data?.error ?? res.statusText);
	}
	return data as T;
}

export const api = {
	get: <T>(path: string) => request<T>('GET', path),
	post: <T>(path: string, body: unknown = {}) => request<T>('POST', path, body),
	put: <T>(path: string, body: unknown) => request<T>('PUT', path, body)
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
