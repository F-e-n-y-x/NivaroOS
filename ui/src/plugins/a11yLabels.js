// Gives Buefy switches, checkboxes and selects an accessible name.
//
// Most sit in a settings row next to a visible title but aren't tied to it,
// so screen readers announced "checkbox" with no name (the audit found 204
// unlabelled controls and 12 unnamed selects). On mount, a control without
// its own label takes an explicit aria-label if one was passed, otherwise
// the title of the row it sits in.
function rowTitle(el) {
	const row = el.closest('.setting-row, .field, .form-field-group, tr, li')
	if (!row) return ''
	const t = row.querySelector('.setting-title, .row-label, .label, label:not(.switch):not(.b-checkbox), th')
	return t ? (t.innerText || '').trim().split('\n')[0].slice(0, 120) : ''
}

function nameControl(vm, selector) {
	const root = vm.$el
	if (!root || !root.querySelector) return
	const input = root.matches && root.matches(selector) ? root : root.querySelector(selector)
	if (!input || input.getAttribute('aria-label') || input.getAttribute('aria-labelledby')) return
	// A checkbox with its own visible text is already named.
	if (input.type === 'checkbox' && (root.innerText || '').trim()) return
	const name = vm.$attrs['aria-label'] || rowTitle(root)
	if (name) input.setAttribute('aria-label', name)
}

export default {
	install(Vue) {
		const patch = (id, selector) => {
			const C = Vue.component(id)
			if (!C) return
			const opts = C.options
			opts.mounted = [].concat(opts.mounted || [], function () {
				this.$nextTick(() => nameControl(this, selector))
			})
		}
		patch('BSwitch', 'input[type=checkbox]')
		patch('BCheckbox', 'input[type=checkbox]')
		patch('BSelect', 'select')
	},
}
