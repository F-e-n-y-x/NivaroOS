// v-animate-css="{ classes: 'fadeIn', duration: 500, delay: 0 }" (or just
// "fadeIn"): plays an animate.css animation (public/css/animate.min.css)
// once, when the element is mounted.
export default {
	beforeMount(el, { value }) {
		const v = typeof value === 'string' ? { classes: value } : value || {}
		const names = String(v.classes || '').split(/\s+/).filter(Boolean)
		if (v.duration) el.style.animationDuration = `${v.duration}ms`
		if (v.delay) el.style.animationDelay = `${v.delay}ms`
		el.classList.add('animated', ...names)
		el.addEventListener('animationend', function end(e) {
			if (e.target !== el) return
			el.classList.remove(...names)
			el.removeEventListener('animationend', end)
		})
	},
}
