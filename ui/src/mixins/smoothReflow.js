// Animates an element's height/width when a re-render changes it (replaces vue-smooth-reflow,
// same call: this.$smoothReflow({ el: '.selector', property: ['height'] }), same .5s ease).
// Skipped entirely under prefers-reduced-motion.
const reducedMotion = () => typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches

function find(vm, sel) {
	const root = vm.$el
	if (!(root instanceof Element)) return null
	return root.matches(sel) ? root : root.querySelector(sel)
}

// Overflow hidden while measuring stops margin collapse from skewing the size.
function measure(el) {
	el.style.overflow = 'hidden'
	const r = el.getBoundingClientRect()
	el.style.overflow = ''
	return r
}

function stop(s) {
	if (!s.el) return
	for (const p of s.props) s.el.style[p] = ''
	s.el.style.overflow = ''
	s.el.style.transition = ''
	s.el.removeEventListener('transitionend', s.onEnd)
	clearTimeout(s.timer)
	s.active = false
}

export default {
	methods: {
		$smoothReflow({ el, property = 'height' }) {
			const s = { sel: el, props: [].concat(property), el: null, before: null, active: false }
			s.onEnd = (e) => { if (e.target === s.el && s.props.includes(e.propertyName)) stop(s) }
			;(this._smoothReflow ||= []).push(s)
		},
	},
	beforeUpdate() {
		for (const s of this._smoothReflow || []) {
			const el = find(this, s.sel)
			s.before = el && !reducedMotion() ? measure(el) : null
			if (s.active) stop(s) // restart from the in-flight size
			s.el = el
		}
	},
	updated() {
		this.$nextTick(() => {
			for (const s of this._smoothReflow || []) {
				const el = find(this, s.sel)
				if (!el || !s.before || s.active) continue
				const after = measure(el)
				if (s.props.every((p) => s.before[p] === after[p])) continue
				s.el = el
				s.active = true
				el.style.overflow = 'hidden'
				for (const p of s.props) el.style[p] = s.before[p] + 'px'
				void el.offsetHeight // commit the start size before transitioning
				el.style.transition = [getComputedStyle(el).transition, ...s.props.map((p) => `${p} .5s`)].filter(Boolean).join(',')
				for (const p of s.props) el.style[p] = after[p] + 'px'
				el.addEventListener('transitionend', s.onEnd)
				s.timer = setTimeout(() => stop(s), 700) // no transitionend (hidden, or cancelled): never leave a fixed size behind
			}
		})
	},
	beforeUnmount() {
		for (const s of this._smoothReflow || []) stop(s)
	},
}
