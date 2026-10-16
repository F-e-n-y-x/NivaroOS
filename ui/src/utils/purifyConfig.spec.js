// @vitest-environment jsdom
import { describe, expect, test } from 'vitest'
import { createApp, h, resolveDirective, withDirectives } from 'vue'
import VueDOMPurifyHTML from 'vue-dompurify-html'
import { marked } from 'marked'
import { purifyConfig } from './purifyConfig'

// Same pipeline as TipEditorModal's preview: marked -> v-dompurify-html.
function render(md) {
	const host = document.createElement('div')
	createApp({
		render: () => withDirectives(h('div'), [[resolveDirective('dompurify-html'), marked.parse(md)]]),
	}).use(VueDOMPurifyHTML, purifyConfig).mount(host)
	return host.firstElementChild
}

describe('markdown preview sanitizing', () => {
	test('strips scripts, event handlers and javascript: links', () => {
		const el = render([
			'# Tips',
			'<img src="x" onerror="alert(1)">',
			'<script>alert(2)</script>',
			'<a href="javascript:alert(3)">bad</a> [ok](https://example.com)',
			'<svg><animate onbegin="alert(4)"/></svg>',
		].join('\n\n'))
		const html = el.innerHTML
		expect(el.querySelector('h1').textContent).toBe('Tips')
		expect(el.querySelector('script')).toBeNull()
		expect(html).not.toMatch(/onerror|onbegin|alert\(|javascript:/i)
		expect(el.querySelector('a[href="https://example.com"]')).not.toBeNull()
	})

	test('existing tips render as they did with marked 4', () => {
		const md = "# Tips\n\nDefault login: **admin** / `password`, mail me@example.com\n\n1. Open http://host:8080\n2. Go to *Settings*\n   - nested\n\n> restart after changes\n\n| Port | Use |\n|---|---|\n| 80 | web |\n\n```yaml\nimage: nginx\n```\n\nLine one  \nline two\n\n- [x] done\n\n~~old~~"
		// Output of marked 4.3 + the same sanitizing (whitespace between tags aside).
		expect(render(md).innerHTML.replace(/>\s+</g, '><').trim()).toBe('<h1>Tips</h1><p>Default login: <strong>admin</strong> / <code>password</code>, mail <a href="mailto:me@example.com">me@example.com</a></p><ol><li>Open <a href="http://host:8080">http://host:8080</a></li><li>Go to <em>Settings</em><ul><li>nested</li></ul></li></ol><blockquote><p>restart after changes</p></blockquote><table><thead><tr><th>Port</th><th>Use</th></tr></thead><tbody><tr><td>80</td><td>web</td></tr></tbody></table><pre><code class="language-yaml">image: nginx\n</code></pre><p>Line one<br>line two</p><ul><li><input> done</li></ul><p><del>old</del></p>')
	})
})
