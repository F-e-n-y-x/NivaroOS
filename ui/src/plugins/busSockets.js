// `sockets: { 'event:name'(payload) {...} }` in a component: those handlers
// get message-bus events (service/messageBusSocket.js) while the component
// lives. `this.$socket` is the bus itself (on/off/connected).
export default {
	install(app, bus) {
		app.config.globalProperties.$socket = bus
		const bound = new WeakMap()
		app.mixin({
			created() {
				const sockets = this.$options.sockets
				if (!sockets) return
				bound.set(this, Object.entries(sockets).map(([name, fn]) => {
					const h = fn.bind(this)
					bus.on(name, h)
					return [name, h]
				}))
			},
			beforeUnmount() {
				;(bound.get(this) || []).forEach(([name, h]) => bus.off(name, h))
				bound.delete(this)
			},
		})
	},
}
