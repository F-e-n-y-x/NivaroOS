/*
 * Per-app display overrides, available for every app type (system,
 * v1/v2, container, link) - rename, custom icon (URL or uploaded+
 * compressed image) with roundness, and (container apps only) a custom
 * URL to open on click. Purely display metadata stored in per-user
 * custom storage, keyed by app name. Never touches app-management/
 * containers - "Import to NivaroOS" is a separate, explicit action.
 */
const overridesConfig = 'legacy_app_overrides'

export default {
	methods: {
		// Throws when the read fails - saveLegacyAppOverride() must never
		// write back an empty map in place of overrides it couldn't read.
		async getLegacyAppOverrides() {
			const res = await this.$api.users.getCustomStorage(overridesConfig)
			const data = res && res.data && res.data.data
			if (data === undefined || data === null || data === '') return {}
			if (typeof data !== 'object' || Array.isArray(data)) {
				throw new Error('Unexpected app override data')
			}
			return data
		},

		async getLegacyAppOverride(appName) {
			const overrides = await this.getLegacyAppOverrides()
			return overrides[appName] || null
		},

		async saveLegacyAppOverride(appName, override) {
			const overrides = await this.getLegacyAppOverrides()
			overrides[appName] = override
			return this.$api.users.setCustomStorage(overridesConfig, overrides)
		}
	}
}
