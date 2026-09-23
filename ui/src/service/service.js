import axios from 'axios'
import router from '@/router'
import store from '@/store'
import { makeUnauthorizedHandler } from './authRefresh'
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
		config.headers.common["Language"] = getInitLang()
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

function logout() {
	store.commit("SET_ACCESS_TOKEN", "");
	store.commit("SET_REFRESH_TOKEN", "");
	router.replace({ //Jump to the logout page
		path: '/logout'
	}).catch(() => {})
}

// One token refresh for however many requests got a 401, then retry them;
// if it fails, they are all rejected and the user is logged out (see
// authRefresh.js for what the old queue got wrong).
const handleUnauthorized = makeUnauthorizedHandler({
	refresh: () => instance.post("/v1/users/refresh", {
		refresh_token: localStorage.getItem("refresh_token"),
	}).then(tokenRes => {
		if (!(tokenRes.data && tokenRes.data.success == 200)) throw new Error("refresh refused")
		const d = tokenRes.data.data
		localStorage.setItem("access_token", d.access_token);
		localStorage.setItem("refresh_token", d.refresh_token);
		localStorage.setItem("expires_at", d.expires_at);
		store.commit("SET_ACCESS_TOKEN", d.access_token);
		store.commit("SET_REFRESH_TOKEN", d.refresh_token);
		instance.defaults.headers.Authorization = d.access_token
		return d.access_token
	}),
	retry: (config) => instance(config),
	logout,
})

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
	put(url, data) {
		url = testVisionNum(url)
		return instance.put(url, data)
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
export { api, instance }
