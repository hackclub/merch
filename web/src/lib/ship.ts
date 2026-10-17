import type { RequestLine } from './types';

/** "2 × Sticker", with what was asked for when an admin shipped something else. */
export function lineText(l: RequestLine): string {
	const name = l.variant ? `${l.name} (${l.variant})` : l.name;
	if (l.requested_quantity == null) return `${l.quantity} × ${name}`;
	if (l.requested_quantity === 0) return `${l.quantity} × ${name} (added by Hack Club)`;
	if (l.quantity === 0) return `${name}: not sent (you asked for ${l.requested_quantity})`;
	return `${l.quantity} × ${name} (you asked for ${l.requested_quantity})`;
}

/** The Ship action's body: only the quantities the admin changed (0 removes a line), and a note to the requester. */
export function shipBody(lines: RequestLine[], edits: Record<string, number>, note: string) {
	return {
		lines: lines.filter((l) => l.sku in edits && edits[l.sku] !== l.quantity).map((l) => ({ sku: l.sku, quantity: edits[l.sku] })),
		note: note.trim()
	};
}
