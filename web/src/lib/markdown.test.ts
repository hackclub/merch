import { describe, expect, it } from 'vitest';
import { renderMarkdown } from './markdown';

describe('renderMarkdown', () => {
	it('renders GitHub-flavored Markdown', () => {
		const html = renderMarkdown('**Asked** for more\n\n- [ ] ship\n- one\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n`Sti/A`');
		expect(html).toContain('<strong>Asked</strong>');
		expect(html).toContain('<li>one</li>');
		expect(html).toContain('<table>');
		expect(html).toContain('<code>Sti/A</code>');
		expect(html).toContain('type="checkbox"');
	});

	it('keeps single line breaks, like a typed note', () => {
		expect(renderMarkdown('line one\nline two')).toContain('line one<br>line two');
	});

	it('links bare URLs and opens links in a new tab', () => {
		const html = renderMarkdown('see https://example.com/t and [doc](https://example.com/d)');
		expect(html).toContain('<a href="https://example.com/t" target="_blank" rel="noopener noreferrer">');
		expect(html).toContain('<a href="https://example.com/d" target="_blank" rel="noopener noreferrer">doc</a>');
	});

	it('strips scripts and javascript: links', () => {
		const html = renderMarkdown('<script>alert(1)</script><img src=x onerror="alert(1)">\n\n[x](javascript:alert(1))');
		expect(html).not.toContain('<script');
		expect(html).not.toContain('onerror');
		expect(html).not.toContain('javascript:');
	});

	it('is empty for an empty note', () => {
		expect(renderMarkdown('  ')).toBe('');
	});
});
