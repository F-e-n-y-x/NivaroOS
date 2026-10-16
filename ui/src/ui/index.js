// The app's UI kit. Replaces the archived Buefy fork one component at a time
// (docs/specs/2026-10-16-buefy-migration.md): each global b-* name comes
// from Buefy until our own component takes it over, so no template changes.
import {
	BAutocomplete, BCheckbox, BDropdown, BDropdownItem, BImage, BLoading,
	BNotification, BNumberinput, BProgress, BSelect, BSwitch, BTabItem, BTabs, BTooltip, BUpload,
	ConfigProgrammatic,
} from '@ntohq/buefy-next'
import notices from './notices'
import Icon from './Icon.vue'
import Button from './Button.vue'
import Field from './Field.vue'
import Input from './Input.vue'

const components = {
	// Still Buefy's.
	BAutocomplete, BCheckbox, BDropdown, BDropdownItem, BImage, BLoading,
	BNotification, BNumberinput, BProgress, BSelect, BSwitch, BTabItem, BTabs, BTooltip, BUpload,
	// Ours.
	BIcon: Icon,
	BButton: Button,
	BField: Field,
	BInput: Input,
}

export default {
	install(app) {
		// this.$buefy.toast/snackbar/dialog keep working for every caller;
		// config sets the icon packs of the Buefy components left.
		app.config.globalProperties.$buefy = { ...notices, config: ConfigProgrammatic }
		for (const [name, c] of Object.entries(components)) app.component(name, c)
	},
}
