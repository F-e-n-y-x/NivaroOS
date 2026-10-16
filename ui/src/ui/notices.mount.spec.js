// @vitest-environment jsdom
import { afterEach, expect, test, vi } from 'vitest'
import { toast, snackbar, dialog } from './notices'

afterEach(() => {
	document.body.innerHTML = ''
})

const key = (k) => document.dispatchEvent(new KeyboardEvent('keydown', { key: k }))

// L6: a toast showing a server error that named an attacker's file ran its
// script.
test('toasts and dialogs never run markup from their message', () => {
	toast.open({ message: 'Failed: <img src=x onerror="window.pwned=1"><b>evil.txt</b> &lt;kept&gt; <i class="mdi"></i>', duration: 60000 })
	dialog.alert({ message: '<svg onload="window.pwned=1"></svg>Done' })
	snackbar.open({ message: '<img src=x onerror="window.pwned=1">x', indefinite: true })
	const html = document.body.innerHTML
	expect(html).not.toMatch(/onerror|onload|src=/)
	expect(html).toContain('<b>evil.txt</b>')
	expect(html).toContain('&lt;kept&gt; <i class="mdi"></i>')
})

test('toast: Buefy DOM, queue replaces, queue false stacks, closes after duration', () => {
	vi.useFakeTimers()
	toast.open({ message: 'a', type: 'is-success' })
	toast.open({ message: 'b', type: 'is-danger', position: 'is-top' })
	expect(document.querySelectorAll('body > .notices.is-top > .toast').length).toBe(1)
	expect(document.querySelector('.toast').className).toContain('toast is-danger is-top')
	toast.open({ message: 'c', queue: false })
	expect(document.querySelectorAll('.toast').length).toBe(2)
	vi.advanceTimersByTime(2200)
	expect(document.querySelectorAll('.toast').length).toBe(0)
	vi.useRealTimers()
})

test('snackbar action runs and closes; indefinite stays', () => {
	vi.useFakeTimers()
	const onAction = vi.fn()
	snackbar.open({ message: 'updated', actionText: 'Reload', indefinite: true, position: 'is-bottom-right', onAction })
	vi.advanceTimersByTime(60000)
	const bar = document.querySelector('body > .notices.is-bottom > .snackbar.is-success.is-bottom-right')
	expect(bar.getAttribute('role')).toBe('alertdialog')
	bar.querySelector('.action button').click()
	expect(onAction).toHaveBeenCalledOnce()
	vi.advanceTimersByTime(200)
	expect(document.querySelector('.snackbar')).toBeNull()
	vi.useRealTimers()
})

test('confirm: confirm, cancel button, escape, and canCancel limits', () => {
	const onConfirm = vi.fn()
	const onCancel = vi.fn()
	dialog.confirm({ title: '<b>T</b>', message: 'm', confirmText: 'Go', cancelText: 'No', type: 'is-danger', onConfirm, onCancel })
	const d = document.querySelector('.dialog.modal.is-active')
	expect(d.querySelector('.modal-card-title').textContent).toBe('<b>T</b>')
	const [no, go] = d.querySelectorAll('.modal-card-foot button')
	expect(no.textContent).toBe('No')
	expect(go.className).toBe('button is-danger')
	expect(document.activeElement).toBe(go)
	go.click()
	expect(onConfirm).toHaveBeenCalledOnce()
	expect(document.querySelector('.dialog')).toBeNull()

	dialog.confirm({ message: 'm', onCancel })
	key('Escape')
	expect(onCancel).toHaveBeenLastCalledWith('escape')
	expect(document.querySelector('.dialog')).toBeNull()

	dialog.confirm({ message: 'm', canCancel: ['button'], onCancel })
	key('Escape')
	document.querySelector('.modal-background').click()
	expect(document.querySelector('.dialog')).not.toBeNull()
	document.querySelector('.modal-card-foot button').click()
	expect(onCancel).toHaveBeenLastCalledWith('button')
	expect(onCancel).toHaveBeenCalledTimes(2)

	dialog.alert('done')
	expect(document.querySelectorAll('.modal-card-foot button').length).toBe(1)
})
