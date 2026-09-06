import 'intersection-observer'
import Vue from 'vue'
import App from '@/App.vue'
import router from '@/router'
import store from '@/store'
import i18n from '@/plugins/i18n'
import api from '@/service/api.js'
import openAPI from '@/service/index.js'
import Buefy from 'buefy'
import VueFullscreen from 'vue-fullscreen'
import Vue2TouchEvents from 'vue2-touch-events'
import VueSocialSharing from 'vue-social-sharing'
import VueSocketIOExt from 'vue-socket.io-extended';
import messageBus from '@/events/index.js'
import VueDOMPurifyHTML from 'vue-dompurify-html'


// Import Styles
import '@/assets/scss/app.scss'
import VAnimateCss from 'v-animate-css';

// The NivaroOS mobile app's VM console screen (mobile/lib/screens/
// vm_console_screen.dart) is the one place that app embeds a webview
// instead of a native screen - a genuine remote-display protocol isn't
// worth reimplementing natively there when this page already renders it
// correctly. That webview is its own separate browser context though (its
// own localStorage, entirely unrelated to the native app's own already-
// authenticated session), so without this it would just bounce to the
// login screen. Handing off the already-obtained tokens via one-time query
// params - written to localStorage exactly like a normal login would, then
// stripped from the address bar immediately - lets it land already signed
// in. Harmless (a no-op) for every normal browser visit, which never has
// these params to begin with.
(function handleMobileAppTokenHandoff() {
	const params = new URLSearchParams(window.location.search)
	const token = params.get('token')
	const refresh = params.get('refresh')
	if (!token) return
	localStorage.setItem('access_token', token)
	if (refresh) localStorage.setItem('refresh_token', refresh)
	params.delete('token')
	params.delete('refresh')
	const cleaned = params.toString()
	const newUrl = window.location.pathname + (cleaned ? `?${cleaned}` : '') + window.location.hash
	window.history.replaceState({}, document.title, newUrl)
})()

const io = require("socket.io-client");

const isDev = process.env.NODE_ENV === 'dev';
const protocol = document.location.protocol
const wsProtocol = protocol === 'https:' ? 'wss:' : 'ws:'
const devIp = process.env.VUE_APP_DEV_IP
const devPort = process.env.VUE_APP_DEV_PORT
const localhost = document.location.host
const localhostName = document.location.hostname
const baseIp = isDev ? `${devIp}` : `${localhostName}`
const baseURL = isDev ? `${devIp}:${devPort}` : `${localhost}`
const wsURL = `${wsProtocol}//${baseURL}`

const socket = io( {
	transports: ['websocket', 'polling'],
	path: '/v2/message_bus/socket.io/',
});

Vue.use(Buefy)
Vue.use(VueFullscreen)
Vue.use(VAnimateCss, { animateCSSPath: '/css/animate.min.css' });
Vue.use(Vue2TouchEvents)
Vue.use(VueSocketIOExt, socket);
Vue.use(VueSocialSharing);
Vue.use(VueDOMPurifyHTML, {
	default: {
		ALLOWED_ATTR: ['target', 'href']
	}
});

Vue.config.productionTip = false
Vue.prototype.$api = api;
Vue.prototype.$openAPI = openAPI;
Vue.prototype.$baseIp = baseIp;
Vue.prototype.$baseURL = baseURL;
Vue.prototype.$protocol = protocol;
Vue.prototype.$wsProtocol = wsProtocol;


// Create an EventBus
Vue.prototype.$EventBus = new Vue();
Vue.prototype.$messageBus = messageBus;

new Vue({
	router,
	i18n,
	store,
	render: h => h(App)
}).$mount('#app')





