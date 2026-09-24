/*
 * Which apps (built-ins and third-party apps) show up as launcher icons
 * in the bottom dock - a plain list of app names in per-user custom storage.
 * Default: Files and App Store only.
 */
const pinsConfig = 'dock_pinned_apps'

export const DEFAULT_PINS = ['Files', 'App Store']

export const SYSTEM_NAME_MAP = {
	files: 'Files',
	appstore: 'App Store',
	terminal: 'Terminal',
	vms: 'VMs',
	settings: 'Settings',
	'host-desktop': 'Host Desktop',
	'download-station': 'Download Station',
	'Download Station': 'Download Station',
	'Host Desktop': 'Host Desktop',
	'Files': 'Files',
	'App Store': 'App Store',
	'Terminal': 'Terminal',
	'VMs': 'VMs',
	'Settings': 'Settings'
}


// Shared read cache for the many desktop AppCards that each want to know
// whether they're pinned: concurrent callers share one in-flight request and
// a result is reused for a short TTL, so N cards (created together, or all
// reacting to one RELOAD_APP_LIST) cost one request instead of N.
// getDockPins() itself stays uncached (the Dock writes pins directly and
// must always read fresh); it just refreshes the cache as a side effect.
const PINS_CACHE_TTL = 3000
let pinsCache = null // { value, at }
let pinsInFlight = null

export function invalidateDockPinsCache() {
	pinsCache = null
}

export default {
	methods: {
		getDockPinsCached() {
			if (pinsCache && Date.now() - pinsCache.at < PINS_CACHE_TTL) {
				return Promise.resolve(pinsCache.value.slice())
			}
			if (!pinsInFlight) {
				pinsInFlight = this.getDockPins().finally(() => {
					pinsInFlight = null
				})
			}
			return pinsInFlight.then(pins => pins.slice())
		},

		async getDockPins() {
			try {
				const res = await this.$api.users.getCustomStorage(pinsConfig)
				const pins = (res.data && Array.isArray(res.data.data)) ? res.data.data : [...DEFAULT_PINS]
				pinsCache = { value: pins.slice(), at: Date.now() }
				return pins
			} catch (e) {
				return [...DEFAULT_PINS]
			}
		},

		async setDockPinned(name, pinned) {
			const normalizedName = SYSTEM_NAME_MAP[name] || name
			const currentPins = await this.getDockPins()
			const withoutName = currentPins.filter(n => {
				const norm = SYSTEM_NAME_MAP[n] || n
				return norm !== normalizedName && n !== name
			})
			const next = pinned ? withoutName.concat(normalizedName) : withoutName
			invalidateDockPinsCache()
			try {
				return await this.$api.users.setCustomStorage(pinsConfig, next)
			} finally {
				invalidateDockPinsCache()
			}
		}
	}
}
