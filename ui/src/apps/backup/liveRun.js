// Live updates for Backup & Sync (spec §10.3): message-bus events over
// the shared socket.io client (this.$socket.client, path
// /v2/message_bus/socket.io/), with GET polling only as a fallback - while
// the socket is disconnected AND the window is visible. This replaces the
// old full-list refetch every 2 s.
//
// Pure of Vue: the socket, client and timers are passed in, so the logic
// is tested in __tests__/liveRun.spec.js with fakes.
import { BACKUP_EVENTS } from './events'
import { liveFromEvent, isFinalStatus } from './state'

const RUN_EVENTS = [BACKUP_EVENTS.RUN_BEGIN, BACKUP_EVENTS.RUN_PROGRESS, BACKUP_EVENTS.RUN_END, BACKUP_EVENTS.RUN_WAITING]

const realTimers = {
	setInterval: (fn, ms) => setInterval(fn, ms),
	clearInterval: id => clearInterval(id)
}

function propsOf(payload) {
	return (payload && payload.Properties) || {}
}

// subscribe adds handlers for bus events and returns a function that
// removes them all. A missing socket (tests, standalone pages) is a no-op.
export function subscribe(socket, handlers) {
	if (!socket || typeof socket.on !== 'function') return () => {}
	const bound = Object.entries(handlers).map(([name, fn]) => {
		const h = payload => fn(propsOf(payload), payload)
		socket.on(name, h)
		return [name, h]
	})
	return () => bound.forEach(([name, h]) => socket.off(name, h))
}

// socketConnected reports whether bus events can arrive.
export function socketConnected(socket) {
	return !!(socket && socket.connected)
}

// watchRun follows one run for the run window.
//
//   const w = watchRun({ runId, client, socket, isVisible, onRun, onLive, onError })
//   ...
//   w.stop()
//
// onRun(run) gets every full RunDetail (first load, step/phase changes,
// the end, polls); onLive(live) gets socket progress between them.
// refresh() forces a refetch (after cancel, decide...).
export function watchRun({ runId, client, socket, isVisible = () => true, onRun, onLive = () => {}, onError = () => {}, pollMs = 3000, timers = realTimers }) {
	let stopped = false
	let final = false
	let inFlight = null
	let again = false
	let lastPhase = ''
	let lastStatus = ''
	let poller = null

	function apply(run) {
		if (stopped || !run) return
		lastPhase = run.phase || ''
		lastStatus = run.status || ''
		final = isFinalStatus(run.status)
		onRun(run)
		if (final) timers.clearInterval(poller)
	}

	// One request at a time; a request made meanwhile runs once after it.
	function refresh() {
		if (stopped) return Promise.resolve()
		if (inFlight) {
			again = true
			return inFlight
		}
		inFlight = client
			.getRun(runId)
			.then(apply)
			.catch(err => {
				if (!stopped) onError(err)
			})
			.finally(() => {
				inFlight = null
				if (again && !stopped) {
					again = false
					refresh()
				}
			})
		return inFlight
	}

	const handlers = {}
	for (const name of RUN_EVENTS) {
		handlers[name] = props => {
			if (stopped || props.run_id !== runId) return
			if (name === BACKUP_EVENTS.RUN_PROGRESS) {
				onLive(liveFromEvent(props), props)
				// A new phase changes the step list: fetch it.
				if ((props.phase && props.phase !== lastPhase) || (props.status && props.status !== lastStatus)) refresh()
				return
			}
			refresh()
		}
	}
	// Events missed while disconnected: catch up once on reconnect.
	handlers.connect = () => {
		if (!final) refresh()
	}
	const unsubscribe = subscribe(socket, handlers)

	poller = timers.setInterval(() => {
		if (stopped || final || socketConnected(socket) || !isVisible()) return
		refresh()
	}, pollMs)

	refresh()

	return {
		refresh,
		stop() {
			stopped = true
			unsubscribe()
			timers.clearInterval(poller)
		},
		get final() {
			return final
		}
	}
}

// watchJobs keeps the app's job list fresh: any job or run event asks for
// a (debounced, by the caller) reload; progress events are handed over
// as live stats keyed by run id. Polling applies only while the socket
// is down and the app is visible.
export function watchJobs({ socket, isVisible = () => true, onChange, onLive = () => {}, pollMs = 10000, timers = realTimers }) {
	let stopped = false
	const handlers = {
		[BACKUP_EVENTS.JOB_CHANGED]: props => onChange('job', props),
		[BACKUP_EVENTS.RUN_BEGIN]: props => onChange('run', props),
		[BACKUP_EVENTS.RUN_END]: props => onChange('run', props),
		[BACKUP_EVENTS.RUN_WAITING]: props => onChange('run', props),
		[BACKUP_EVENTS.RUN_PROGRESS]: props => props.run_id && onLive(props.run_id, liveFromEvent(props), props),
		connect: () => onChange('reconnect', {})
	}
	const unsubscribe = subscribe(socket, handlers)
	const poller = timers.setInterval(() => {
		if (!stopped && !socketConnected(socket) && isVisible()) onChange('poll', {})
	}, pollMs)
	return {
		stop() {
			stopped = true
			unsubscribe()
			timers.clearInterval(poller)
		}
	}
}
