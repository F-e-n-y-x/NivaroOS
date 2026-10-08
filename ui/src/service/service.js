import axios from 'axios'
import router from '@/router'
import store from '@/store'
import { makeUnauthorizedHandler } from './authRefresh'
import { createRefreshCoordinator, AUTH_CHANNEL, TOKEN_KEYS } from './tokenCoordinator'
// import { ToastProgrammatic as Toast } from 'buefy'


const axiosBaseURL = ``

//Create a axios instance, And set timeout to 30s
const instance = axios.create({
	baseURL: axiosBaseURL,
	timeout: 60000,
	headers: {
		"Content-Type": "application/json",
	},
	withCredentials: false,
});

const getLangFromBrowser = () => {
	let lang = navigator.language || navigator.userLanguage;
	lang = lang.toLowerCase().replace("-", "_");
	return lang
}

const getInitLang = () => {
	const lang = localStorage.getItem('lang') || getLangFromBrowser()
	return lang
}


// Interception before request initiation
instance.interceptors.request.use(
	(config) => {
		// axios 1.x: headers are already flattened here (no .common).
		config.headers.Language = getInitLang()
		const token = localStorage.getItem("access_token")
		const rtoken = localStorage.getItem("refresh_token")
		if (token) {
			config.headers.Authorization = token
			store.commit("SET_ACCESS_TOKEN", token);
			store.commit("SET_REFRESH_TOKEN", rtoken);
		}
		return config;
	}, (error) => {
		// Do something with request error
		return Promise.reject(error)
	}
)

// Response interception

function logout(why) {
	if (why && why.reason) console.warn('[auth] signed out:', why.reason)
	store.commit("SET_ACCESS_TOKEN", "");
	store.commit("SET_REFRESH_TOKEN", "");
	router.replace({ //Jump to the logout page
		path: '/logout'
	}).catch(() => {})
}

// Tokens another tab stored (its refresh): keep this tab's copy current.
const adoptStoredTokens = () => {
	try {
		const access = localStorage.getItem(TOKEN_KEYS.access)
		if (access) {
			store.commit("SET_ACCESS_TOKEN", access);
			store.commit("SET_REFRESH_TOKEN", localStorage.getItem(TOKEN_KEYS.refresh) || "");
		}
	} catch (e) {}
}
let authChannel = null
try {
	if (typeof BroadcastChannel !== 'undefined') {
		authChannel = new BroadcastChannel(AUTH_CHANNEL)
		authChannel.onmessage = (e) => { if (e && e.data && e.data.type === 'tokens') adoptStoredTokens() }
	}
} catch (e) {
	authChannel = null
}
if (typeof window !== 'undefined') {
	window.addEventListener('storage', (e) => { if (e.key === TOKEN_KEYS.access) adoptStoredTokens() })
}

// One refresh for the whole browser (every tab), always with the newest
// stored tokens; see tokenCoordinator.js for why one tab's refused
// refresh used to sign every tab out.
const coordinator = createRefreshCoordinator({
	storage: localStorage,
	// validateStatus: every HTTP answer resolves - a 401 here is judged by
	// the coordinator, not by the 401 interceptor below.
	post: (refreshToken) => instance.post("/v1/users/refresh", { refresh_token: refreshToken }, { validateStatus: () => true }),
	locks: (typeof navigator !== 'undefined' && navigator.locks) || null,
	channel: authChannel,
	onTokens: (access, refresh) => {
		store.commit("SET_ACCESS_TOKEN", access);
		store.commit("SET_REFRESH_TOKEN", refresh);
	},
})

// One token refresh for however many requests got a 401, then retry them;
// if the session ended they are all rejected and the user is logged out
// (see authRefresh.js for what the old queue got wrong).
const handleUnauthorized = makeUnauthorizedHandler({
	refresh: (sentToken) => coordinator.refresh(sentToken),
	retry: (config) => instance(config),
	logout,
})

// Refresh the access token now (single-flight with the 401 handler above);
// resolves to the new token. Used by the message-bus socket
// (messageBusSocket.js) when its handshake is refused.
const refreshAccessToken = (sentToken) => handleUnauthorized.refreshNow(sentToken)

instance.interceptors.response.use(
	(response) => response,
	(error) => {
		const config = error && error.config
		if (config && config.url !== "/users/register" && error.response && error.response.status === 401) {
			return handleUnauthorized(error)
		}
		return Promise.reject(error)
	}
)

const testVisionNum = (prefix) => {
	// default version number is /v1
	if (/^http/.test(prefix) || /^\/v[2-9]/.test(prefix)) {
		return prefix
	} else {
		return `/v1${prefix}`
	}
}

const CancelToken = axios.CancelToken;
// Wrapping of axios by request type
const api = {

	get(url, data, _this) {
		url = testVisionNum(url)
		if (_this) {
			return instance.get(url, {
				params: data,
				cancelToken: new CancelToken(function executor(c) {
					_this.cancelRequest = c
				})
			})
		} else {
			return instance.get(url, {
				params: data
			})
		}

	},
	post(url, data, config) {
		url = testVisionNum(url)
		return instance.post(url, data, config)
	},
	put(url, data, config) {
		url = testVisionNum(url)
		return instance.put(url, data, config)
	},
	delete(url, data) {
		url = testVisionNum(url)
		return instance.delete(url, { data: data })
	},
	patch(url, data) {
		url = testVisionNum(url)
		return instance.patch(url, data)
	},
}
export { api, instance, refreshAccessToken }
