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
})
