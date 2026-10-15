import { createApp } from 'vue'
import { assetUrl } from '@/utils/assetUrl'
import { watchForUpdates } from '@/utils/updateWatcher'
import App from '@/App.vue'
import router from '@/router'
import store from '@/store'
import i18n, { setLocale } from '@/plugins/i18n'
import api from '@/service/api.js'
import openAPI from '@/service/index.js'
import { instance } from '@/service/service.js'
import { createMessageBusSocket, EVENT_TYPES_PATH, sourcesOf } from '@/service/messageBusSocket'
import Buefy from '@ntohq/buefy-next'
import A11yLabels from '@/plugins/a11yLabels'
import BusSockets from '@/plugins/busSockets'
import animateCss from '@/plugins/animateCss'
import VueDOMPurifyHTML from 'vue-dompurify-html'
import { purifyConfig } from '@/utils/purifyConfig'
import { createEventBus } from '@/utils/eventBus'
import ConfirmWindow from '@/shared/basicComponents/ConfirmWindow.vue'

// Import Styles
import '@/assets/scss/app.scss'

const isDev = import.meta.env.MODE === 'dev';
const protocol = document.location.protocol
const wsProtocol = protocol === 'https:' ? 'wss:' : 'ws:'
const devIp = import.meta.env.VUE_APP_DEV_IP
const devPort = import.meta.env.VUE_APP_DEV_PORT
const localhost = document.location.host
const localhostName = document.location.hostname
const baseIp = isDev ? `${devIp}` : `${localhostName}`
const baseURL = isDev ? `${devIp}:${devPort}` : `${localhost}`
const wsURL = `${wsProtocol}//${baseURL}`

// The message bus requires the access token on every subscription:
// messageBusSocket.js sends it, lists the event sources through the API
// client (which refreshes an expired token), and stays disconnected while
// logged out.
const readAccessToken = () => {
	try {
		return localStorage.getItem('access_token') || store.state.access_token || ''
	} catch (e) {
		return store.state.access_token || ''
	}
}
const socket = createMessageBusSocket({
	getToken: readAccessToken,
	listSources: () => instance.get(EVENT_TYPES_PATH).then((res) => sourcesOf(res.data)),
	wsBase: wsURL,
	watchToken: (cb) => {
		store.watch((state) => state.access_token, (token) => cb(token || ''))
		// /logout clears localStorage only (the store keeps the old token).
		router.afterEach(() => {
			let stored = ''
			try {
				stored = localStorage.getItem('access_token') || ''
			} catch (e) {
				return
			}
			if (!stored) cb('')
		})
	},
});

const app = createApp(App)
app.use(router)
app.use(store)
app.use(i18n)
app.use(Buefy)
app.use(A11yLabels)
app.component('ConfirmWindow', ConfirmWindow)
app.directive('animate-css', animateCss)
app.use(BusSockets, socket)
app.use(VueDOMPurifyHTML, purifyConfig)

Object.assign(app.config.globalProperties, {
	$api: api,
	$openAPI: openAPI,
	$baseIp: baseIp,
	$baseURL: baseURL,
	$protocol: protocol,
	$wsProtocol: wsProtocol,
	$EventBus: createEventBus(),
	// URL of an image under src/assets (see utils/assetUrl.js).
	$assetUrl: assetUrl,
})
// The saved locale's chunk first, so the first paint is already translated.
await setLocale(localStorage.getItem('lang') || 'en_us')
const vm = app.mount('#nivaroos')

// After an update the open tab still runs the old build (see
// utils/updateWatcher.js): offer the reload instead of leaving parts of
// the desktop that can't load any more.
watchForUpdates({
	router,
	socket,
	onUpdate() {
		vm.$buefy.snackbar.open({
			message: i18n.global.t('NivaroOS was updated. Reload to use the new version.'),
			actionText: i18n.global.t('Reload'),
			indefinite: true,
			position: 'is-bottom-right',
			onAction: () => window.location.reload(),
		})
	},
})





