// The app's UI kit. Replaces the archived Buefy fork one component at a time
// (docs/specs/2026-10-16-buefy-migration.md): each global b-* name comes
// from Buefy until our own component takes it over, so no template changes.
import {
	BAutocomplete, BDropdown, BDropdownItem,
	BNumberinput, BTabItem, BTabs, BUpload,
	ConfigProgrammatic,
} from '@ntohq/buefy-next'
import notices from './notices'
import Icon from './Icon.vue'
import Button from './Button.vue'
import Field from './Field.vue'
import Input from './Input.vue'
import Switch from './Switch.vue'
import Checkbox from './Checkbox.vue'
import Select from './Select.vue'
import Tooltip from './Tooltip.vue'
import Image from './Image.vue'
import Loading from './Loading.vue'
import Progress from './Progress.vue'
import Notification from './Notification.vue'

const components = {
	// Still Buefy's.
	BAutocomplete, BDropdown, BDropdownItem,
	BNumberinput, BTabItem, BTabs, BUpload,
	// Ours.
	BIcon: Icon,
	BButton: Button,
	BField: Field,
	BInput: Input,
	BSwitch: Switch,
	BCheckbox: Checkbox,
	BSelect: Select,
	BTooltip: Tooltip,
	BImage: Image,
	BLoading: Loading,
	BProgress: Progress,
	BNotification: Notification,
}

export default {
	install(app) {
		// this.$buefy.toast/snackbar/dialog keep working for every caller;
		// config sets the icon packs of the Buefy components left.
		app.config.globalProperties.$buefy = { ...notices, config: ConfigProgrammatic }
		for (const [name, c] of Object.entries(components)) app.component(name, c)
	},
}
