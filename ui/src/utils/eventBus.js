// The desktop-wide event bus (this.$EventBus): Vue 2's instance events
// ($on/$off/$once/$emit), which Vue 3 dropped.
export function createEventBus() {
	const all = new Map()
	const names = n => (Array.isArray(n) ? n : [n])
	const bus = {
		$on(name, fn) {
			names(name).forEach(n => all.set(n, [...(all.get(n) || []), fn]))
			return bus
		},
		$off(name, fn) {
			if (!name) all.clear()
			else names(name).forEach(n => (fn ? all.set(n, (all.get(n) || []).filter(h => h !== fn && h.fn !== fn)) : all.delete(n)))
			return bus
		},
		$once(name, fn) {
			const once = (...args) => {
				bus.$off(name, once)
				fn(...args)
			}
			once.fn = fn
			return bus.$on(name, once)
		},
		$emit(name, ...args) {
			;(all.get(name) || []).slice().forEach(fn => fn(...args))
			return bus
		},
	}
	return bus
}
