// Toast, snackbar and confirm/alert dialog: the calls this app made on
// Buefy's $buefy (still reachable as this.$buefy.*, see ui/index.js), with
// the same options and the same DOM, so Bulma and our SCSS style them as
// before.
//
// `message` is HTML (callers put <b> and <i class="mdi"> in it). It is
// sanitized here, the only place it enters the page (L6): a file, container
// or task named <img onerror=...> must never run script. Text callers
// escaped themselves stays escaped.
import DOMPurify from 'dompurify'
import { purifyConfig } from '@/utils/purifyConfig'
import { trapTab } from './focus'

const clean = (html) => DOMPurify.sanitize(String(html ?? ''), purifyConfig.default)
const el = (tag, className, parent) => {
	const e = document.createElement(tag)
	if (className) e.className = className
	if (parent) parent.appendChild(e)
	return e
}
const fade = (e, cls) => {
	e.classList.add(cls)
	e.addEventListener('animationend', () => e.classList.remove(cls), { once: true })
}

// body > .notices.is-top / .notices.is-bottom, as Buefy laid them out.
function parent(position) {
	const where = position.startsWith('is-top') ? 'is-top' : 'is-bottom'
	let p = document.querySelector(`body > .notices.${where}`)
	if (!p) {
		p = el('div', `notices ${where}`, document.body)
	}
	return p
}

function notice(build, o, defaults) {
	o = { ...defaults, ...(typeof o === 'string' ? { message: o } : o) }
	const box = parent(o.position)
	// queue (the default) replaces what is showing; queue: false stacks.
	if (o.queue !== false) box.replaceChildren()
	const n = build(o)
	box.insertAdjacentElement('afterbegin', n)
	fade(n, o.position.startsWith('is-top') ? 'fadeInDown' : 'fadeInUp')
	let timer
	const close = () => {
		clearTimeout(timer)
		fade(n, 'fadeOut')
		setTimeout(() => n.remove(), 150)
		if (typeof o.onClose === 'function') o.onClose()
	}
	if (!o.indefinite) timer = setTimeout(close, o.duration)
	return { close }
}

export const toast = {
	open: (o) =>
		notice(
			(o) => {
				const n = el('div', `toast ${o.type} ${o.position}`)
				n.setAttribute('role', 'alert')
				el('div', '', n).innerHTML = clean(o.message)
				return n
			},
			o,
			{ type: 'is-dark', position: 'is-top', duration: 2000 },
		),
}

export const snackbar = {
	open(o) {
		const handle = notice(
			(o) => {
				const n = el('div', `snackbar ${o.type} ${o.position}`)
				n.setAttribute('role', o.actionText ? 'alertdialog' : 'alert')
				el('div', 'text', n).innerHTML = clean(o.message)
				const action = (cls, text, fn) => {
					const a = el('div', cls, n)
					el('button', 'button', a).textContent = text
					a.addEventListener('click', () => {
						if (fn) fn()
						handle.close()
					})
				}
				if (o.cancelText) action('action is-light is-cancel', o.cancelText)
				if (o.actionText) action(`action ${o.type}`, o.actionText, o.onAction)
				return n
			},
			o,
			{ type: 'is-success', position: 'is-bottom-right', duration: 3500, actionText: 'OK' },
		)
		return handle
	},
}

let dialogs = 0
const ALL_CANCEL = ['escape', 'x', 'outside', 'button']

function openDialog(o) {
	o = { type: 'is-primary', confirmText: 'OK', cancelText: 'Cancel', canCancel: true, ...o }
	const can = o.canCancel === true ? ALL_CANCEL : o.canCancel || []
	const opener = document.activeElement
	const root = el('div', 'dialog modal is-active', document.body)
	root.setAttribute('role', 'dialog')
	root.setAttribute('aria-modal', 'true')
	const bg = el('div', 'modal-background', root)
	const card = el('div', 'modal-card animation-content', root)
	if (o.title) {
		const head = el('header', 'modal-card-head', card)
		const t = el('p', 'modal-card-title', head)
		t.id = `dialog-title-${++dialogs}`
		t.textContent = o.title
		root.setAttribute('aria-labelledby', t.id)
	}
	const body = el('section', `modal-card-body${o.title ? '' : ' is-titleless'}`, card)
	el('div', '', el('p', '', el('div', 'media-content', el('div', 'media', body)))).innerHTML = clean(o.message)
	const foot = el('footer', 'modal-card-foot', card)
	const button = (cls, text) => {
		const b = el('button', cls, foot)
		b.type = 'button'
		el('span', '', b).textContent = text
		return b
	}

	const close = () => {
		root.remove()
		document.removeEventListener('keydown', onKey)
		if (opener && opener.focus) opener.focus()
	}
	const cancel = (method) => {
		if (!can.includes(method)) return
		close()
		if (o.onCancel) o.onCancel(method)
	}
	const onKey = (e) => {
		if (e.key === 'Escape' || e.key === 'Esc') return cancel('escape')
		trapTab(e, foot)
	}
	document.addEventListener('keydown', onKey)
	bg.addEventListener('click', () => cancel('outside'))
	if (can.includes('button')) button('button', o.cancelText).addEventListener('click', () => cancel('button'))
	const ok = button(`button ${o.type}`, o.confirmText)
	ok.addEventListener('click', () => {
		close()
		if (o.onConfirm) o.onConfirm('')
	})
	ok.focus()
	return { close }
}

export const dialog = {
	confirm: openDialog,
	alert: (o) => openDialog({ canCancel: false, ...(typeof o === 'string' ? { message: o } : o) }),
}

export default { toast, snackbar, dialog }
