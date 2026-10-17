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
