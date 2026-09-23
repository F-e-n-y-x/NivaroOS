// The one paste path. Toolbar Paste, Ctrl+V, right-click Paste / Paste
// into folder, the sidebar menu and drag-and-drop "Copy/Move here" all
// used to call the API separately - with no error handling on some, blind
// reload timers on others, and nothing stopping a double click from
// starting two overwrite jobs. They all go through here now.
import transfers from '@/service/transfers'
import { escapeHtml } from '@/utils/escapeHtml'
import folderApi from '@/service/folder'

const baseName = (p) => p.replace(/\/+$/, '').split('/').pop()
const parentOf = (p) => p.replace(/\/+$/, '').split('/').slice(0, -1).join('/') || '/'

// Names in `from` that already exist in `to` (pasting into an item's own
// folder isn't a conflict - the engine keeps both automatically).
async function findConflicts(from, to) {
	const dest = to.replace(/\/+$/, '') || '/'
	const candidates = from.filter((p) => parentOf(p) !== dest)
	if (!candidates.length) return []
	let res
	try {
		res = await folderApi.getList(dest)
	} catch (e) {
		return [] // can't tell - the engine's default still applies
	}
	const content = (res.data && res.data.data && res.data.data.content) || []
	const existing = new Set(content.map((i) => i.name))
	return candidates.map(baseName).filter((n) => existing.has(n))
}

// Ask Replace / Keep both / Skip in a desktop window. Resolves to a
// conflict style, or null for Cancel.
function askConflict(vm, names, to) {
	return new Promise((resolve) => {
		const id = 'transfer-conflict-' + Date.now()
		vm.$store.commit('OPEN_WINDOW', {
			id,
			title: vm.$t('Replace or keep both?'),
			component: 'TransferConflictWindow',
			props: { winId: id, isDialog: true, names, destName: baseName(to) || to, onChoose: resolve },
			width: 480,
			height: names.length > 1 ? 330 : 220,
		})
	})
}

/**
 * @param vm      any component (for $store / $buefy / $t)
 * @param {{type:'copy'|'move', from:string[], to:string}} op
 * @param {{clearClipboardOnMove?: boolean}} opts
 * @returns {Promise<object|null>} the job, or null if it couldn't start
 */
export async function startTransfer(vm, op, opts = {}) {
	try {
		if (!op.style) {
			const conflicts = await findConflicts(op.from, op.to)
			if (conflicts.length) {
				const style = await askConflict(vm, conflicts, op.to)
				if (!style) return null
				op = { ...op, style }
			}
		}
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
