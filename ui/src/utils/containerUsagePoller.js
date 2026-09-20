import container from '@/service/container.js'

// Cpu.vue and Ram.vue each used to run their own 1s setInterval calling
// getHardwareUsage() while expanded - identical "stats for every running
// container" data fetched twice per second whenever both widgets were
// expanded at once, doubling load on the docker-stats endpoint for no
// reason. This is a single shared poller both subscribe to instead:
// reference-counted so the request only runs while at least one widget
// wants it, and every subscriber gets the same fetched response.
const POLL_INTERVAL_MS = 1000

let timer = null
const subscribers = new Set()

function tick() {
	container.getHardwareUsage().then((res) => {
		for (const callback of subscribers) {
			callback(res)
		}
	})
}

export function subscribeContainerUsage(callback) {
	subscribers.add(callback)
	if (!timer) {
		tick()
		timer = setInterval(tick, POLL_INTERVAL_MS)
	}
	return function unsubscribe() {
		subscribers.delete(callback)
		if (subscribers.size === 0 && timer) {
			clearInterval(timer)
			timer = null
		}
	}
}
