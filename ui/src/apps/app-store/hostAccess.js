import { escapeHtml } from '@/utils/escapeHtml'

// Before installing an app that gets root access to the server - host
// networking (the host's loopback, which NivaroOS's APIs trust) or
// privileged mode - app-management answers 428 with the reasons in
// data.host_access (audit H1). call(options) sends the request; on a 428
// this asks the admin in vm's confirm window and, on yes, sends it again
// with ?allow_host_access=true. Resolves to [response, options] - pass the
// options on to a follow-up request for the same app. update: the request
// is a store update whose new version wants more access than the
// installed one (the server holds such updates until the admin's OK).
export async function withHostAccessConfirm(vm, call, { update = false } = {}) {
	try {
		return [await call({}), {}]
	} catch (e) {
		const why = e && e.response && e.response.status === 428 && e.response.data && e.response.data.data && e.response.data.data.host_access
		if (!Array.isArray(why) || !why.length) throw e
		const ok = await new Promise((resolve) => vm.confirmWindow({
			title: vm.$t('Give this app full access to the server?'),
			message: `${escapeHtml(update
				? vm.$t('The new version of this app asks for full (root) access to the server: it could read and change everything on it. Until you approve, the current version keeps running.')
				: vm.$t('This app gets full (root) access to the server: it can read and change everything on it. Install it only if you trust it.'))}<br><br>${why.map(escapeHtml).join('<br>')}`,
			type: 'is-danger',
			confirmText: vm.$t(update ? 'Update anyway' : 'Install anyway'),
			cancelText: vm.$t('Cancel'),
			height: 300,
			onConfirm: () => resolve(true),
			onCancel: () => resolve(false),
		}))
		if (!ok) throw new Error(vm.$t(update ? 'Not updated' : 'Not installed'))
		const options = { params: { allow_host_access: true } }
		return [await call(options), options]
	}
}
