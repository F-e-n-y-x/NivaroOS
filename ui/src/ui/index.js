// The app's UI kit, which replaced the archived Buefy fork
// (docs/specs/2026-10-16-buefy-migration.md): the same global b-* names,
// props and DOM, so templates and styles didn't change.
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
import Dropdown from './Dropdown.vue'
import DropdownItem from './DropdownItem.vue'
import Tabs from './Tabs.vue'
import TabItem from './TabItem.vue'
import Upload from './Upload.vue'
import Numberinput from './Numberinput.vue'
import Autocomplete from './Autocomplete.vue'

const components = {
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
	BDropdown: Dropdown,
	BDropdownItem: DropdownItem,
	BTabs: Tabs,
	BTabItem: TabItem,
	BUpload: Upload,
	BNumberinput: Numberinput,
	BAutocomplete: Autocomplete,
}

export default {
	install(app) {
		// this.$buefy.toast/snackbar/dialog keep working for every caller.
		app.config.globalProperties.$buefy = notices
		for (const [name, c] of Object.entries(components)) app.component(name, c)
	},
}
