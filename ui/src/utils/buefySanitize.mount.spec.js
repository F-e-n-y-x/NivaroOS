// @vitest-environment jsdom
import { afterEach, expect, test } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import Buefy from '@ntohq/buefy-next'
import { sanitizeBuefyMessages } from './buefySanitize'

afterEach(() => {
	document.body.innerHTML = ''
})

// L6: a toast showing a server error that named an attacker's file ran its
// script.
test('toasts and dialogs never run markup from their message', async () => {
	const app = createApp({ render: () => h('div') })
	app.use(Buefy)
	sanitizeBuefyMessages(app.config.globalProperties.$buefy)
	app.mount(document.body.appendChild(document.createElement('div')))
	const $buefy = app.config.globalProperties.$buefy

	$buefy.toast.open({ message: 'Failed: <img src=x onerror="window.pwned=1"><b>evil.txt</b> &lt;kept&gt; <i class="mdi"></i>', duration: 60000 })
	$buefy.dialog.alert({ message: '<svg onload="window.pwned=1"></svg>Done' })
	await nextTick()
	const html = document.body.innerHTML
	expect(html).not.toMatch(/onerror|onload|src=/)
	expect(html).toContain('<b>evil.txt</b>')
	expect(html).toContain('&lt;kept&gt; <i class="mdi"></i>')
	app.unmount()
})
