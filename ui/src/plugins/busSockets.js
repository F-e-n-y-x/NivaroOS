// `sockets: { 'event:name'(payload) {...} }` in a component: those handlers
// get message-bus events (service/messageBusSocket.js) while the component
// lives. `this.$socket` is the bus itself (on/off/connected).
export default {
	install(app, bus) {
		const target = (app.config && app.config.globalProperties) || app.prototype
		target.$socket = bus
		function unbind() {
			;(this._busHandlers || []).forEach(([name, h]) => bus.off(name, h))
			this._busHandlers = null
		}
		app.mixin({
			created() {
				const sockets = this.$options.sockets
				if (!sockets) return
				this._busHandlers = Object.entries(sockets).map(([name, fn]) => {
					const h = fn.bind(this)
					bus.on(name, h)
					return [name, h]
				})
			},
			beforeDestroy: unbind,
			beforeUnmount: unbind,
		})
	},
}
