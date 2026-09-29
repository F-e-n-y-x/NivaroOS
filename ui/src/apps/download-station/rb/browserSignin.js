// Opening the "Sign in with the browser" window (RbSigninWindow) from
// anywhere - Online Accounts uses it for TeraBox, whose login is a cookie.
import { downloadSidecar } from '@/api/downloadSidecar'

// { available, hint } - whether one-click sign-in can be offered here, and
// if not, what to tell the user.
export async function browserSigninAvailability(t) {
	let st
	try {
		st = await downloadSidecar.rbStatus()
	} catch (e) {
		return { available: false, hint: t('Install Download Station (Settings > Apps) to sign in with a browser instead of pasting a cookie.') }
	}
	if (st && st.available) return { available: true, hint: '' }
	if (st && st.reason === 'low_memory') return { available: false, hint: t('This box has too little memory for the browser sign-in - paste the cookie instead.') }
	return { available: false, hint: t('Turn on Download Station\'s full browser (Download Station > Browser > Install full browser) to sign in with a browser instead of pasting a cookie.') }
}

export function openBrowserSignin(store, { provider, purpose, title, onConnected }) {
	const id = 'rb-signin-' + Date.now()
	store.commit('OPEN_WINDOW', {
		id,
		title,
		component: 'RbSigninWindow',
		props: { winId: id, provider, purpose: purpose || 'add', onConnected },
		width: 980,
		height: 760
	})
	return id
}
