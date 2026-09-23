// The one paste path. Toolbar Paste, Ctrl+V, right-click Paste / Paste
// into folder, the sidebar menu and drag-and-drop "Copy/Move here" all
// used to call the API separately - with no error handling on some, blind
// reload timers on others, and nothing stopping a double click from
// starting two overwrite jobs. They all go through here now.
import transfers from '@/service/transfers'
import { escapeHtml } from '@/utils/escapeHtml'

/**
 * @param vm      any component (for $store / $buefy / $t)
 * @param {{type:'copy'|'move', from:string[], to:string}} op
 * @param {{clearClipboardOnMove?: boolean}} opts
 * @returns {Promise<object|null>} the job, or null if it couldn't start
 */
export async function startTransfer(vm, op, opts = {}) {
	try {
		const job = await transfers.submit(op)
		if (op.type === 'move' && opts.clearClipboardOnMove) vm.$store.commit('SET_OPERATE_OBJECT', null)
		return job
	} catch (e) {
		vm.$buefy.toast.open({ message: escapeHtml(e.message), type: 'is-danger', duration: 5000 })
		return null
	}
}

// Paste whatever is on the Files clipboard into `dest`.
export function pasteClipboard(vm, dest) {
	const clip = vm.$store.state.operateObject
	if (!clip || !clip.item || !clip.item.length || !dest) return Promise.resolve(null)
	return startTransfer(vm, { type: clip.type, from: clip.item.map((i) => i.from), to: dest }, { clearClipboardOnMove: true })
}
