import { reactive } from 'vue'
import users from '@/service/users.js'
import WidgetFieldsMenu from '@/shared/widgets/WidgetFieldsMenu.vue'

// Which fields each dashboard widget hides, per user (server custom
// storage, so it follows the user to any browser): { cpu: ['temp'], ... }.
// Stored as the hidden keys, so a field added to a widget later shows by
// default.
export const STORAGE_KEY = 'widget_fields'
export const hiddenFields = reactive({})
let loading = null

export function loadHiddenFields() {
	loading ||= users.getCustomStorage(STORAGE_KEY).then(res => {
		const saved = (res && res.data && res.data.data) || {}
		// A toggle made while this was in flight wins over the stale copy.
		for (const [w, keys] of Object.entries(saved)) {
			if (!(w in hiddenFields) && Array.isArray(keys)) hiddenFields[w] = keys
		}
	}).catch(() => { loading = null })
	return loading
}

export const showsField = (widget, key) => !(hiddenFields[widget] || []).includes(key)

export function toggleField(widget, key) {
	const off = hiddenFields[widget] || []
	hiddenFields[widget] = off.includes(key) ? off.filter(k => k !== key) : off.concat([key])
	return users.setCustomStorage(STORAGE_KEY, { ...hiddenFields }).catch(err => {
		hiddenFields[widget] = off
		throw err
	})
}

// For widgets: declare `fields: [{ key, label }]` next to name/title, then
// use shows('key') in the template and <widget-fields-menu> in the header.
export const widgetFieldsMixin = {
	components: { WidgetFieldsMenu },
	created() { loadHiddenFields() },
	computed: {
		fieldItems() {
			return (this.$options.fields || []).map(f => ({ ...f, on: showsField(this.$options.name, f.key) }))
		},
	},
	methods: {
		shows(key) { return showsField(this.$options.name, key) },
		toggleField(key) {
			toggleField(this.$options.name, key).catch(() => {
				this.$buefy.toast.open({ message: this.$t("Couldn't save the widget setting"), type: 'is-danger' })
			})
		},
	},
}
