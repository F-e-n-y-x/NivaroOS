// The app's UI kit. Replaces the archived Buefy fork one component at a time
// (docs/specs/2026-10-16-buefy-migration.md): Buefy is still installed for
// the components not moved yet, and ours are registered after it under the
// same global names, so no template changes.
import Buefy from '@ntohq/buefy-next'
import notices from './notices'

export default {
	install(app) {
		app.use(Buefy)
		// this.$buefy.toast/snackbar/dialog keep working for every caller.
		Object.assign(app.config.globalProperties.$buefy, notices)
	},
}
