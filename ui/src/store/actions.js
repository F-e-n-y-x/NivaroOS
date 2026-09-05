import $api from "@/service/api.js";

const actions = {
	// GET_HARDWARE_INFO(context, val) {
	//     context.commit("GET_HARDWARE_INFO",val)
	// },
	// set shortcut data
	async SET_SHORTCUT_DATA(context, val) {
		try {
			const items = Array.isArray(val)
				? val.map((item) => ({
						name: item.name,
						path: item.path,
						icon: item.icon || 'folder-outline',
						pack: item.pack || 'casa',
						visible: true,
						selected: true,
						extensions: null,
				  }))
				: []
			context.commit("SET_SHORTCUT_DATA", items)
			try {
				const res = await $api.users.saveShutcutDetail(items)
				if (res && res.data && Array.isArray(res.data.data)) {
					context.commit("SET_SHORTCUT_DATA", res.data.data)
				}
			} catch (err) {
				console.warn("Could not persist shortcuts to backend:", err)
			}
		} catch (e) {
			console.error("SET_SHORTCUT_DATA error:", e)
		}
	},

	//get shortcut data
	async GET_SHORTCUT_DATA(context, val) {
		try {
			let data = await $api.users.getShutcutDetail(val).then((v) => v.data?.data)
			if (!data || !Array.isArray(data)) {
				data = []
			}
			context.commit("SET_SHORTCUT_DATA", data)
		} catch (e) {
			console.warn("GET_SHORTCUT_DATA warning:", e)
		}
	}

}
export default actions
