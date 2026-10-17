import DOMPurify from 'dompurify';
import { marked } from 'marked';

// Admins' internal notes are GitHub-flavored Markdown. Single line breaks
// stay breaks (they're typed like chat), and links open in a new tab.
const md = marked.setOptions({ gfm: true, breaks: true, async: false });

DOMPurify.addHook('afterSanitizeAttributes', (node) => {
	if (node.tagName === 'A' && node.getAttribute('href')) {
		node.setAttribute('target', '_blank');
		node.setAttribute('rel', 'noopener noreferrer');
	}
});

export function renderMarkdown(src: string): string {
	if (!src.trim()) return '';
	return DOMPurify.sanitize(md.parse(src) as string);
}

const escape = (s: string) => s.replace(/[&<>"']/g, (c) => `&${{ '&': 'amp', '<': 'lt', '>': 'gt', '"': 'quot', "'": '#39' }[c]};`);

/** What a requester typed, as HTML: escaped, with its http(s) links clickable.
 * It isn't Markdown (they never saw a preview); show it with `white-space: pre-wrap`. */
export function linkify(text: string): string {
	return text
		.split(/(https?:\/\/[^\s<>"']+)/)
		.map((part, i) => {
			if (i % 2 === 0) return escape(part);
			const url = part.replace(/[.,;:!?)\]]+$/, '');
			const href = escape(url);
			return `<a href="${href}" target="_blank" rel="noopener noreferrer">${href}</a>${escape(part.slice(url.length))}`;
		})
		.join('');
}
