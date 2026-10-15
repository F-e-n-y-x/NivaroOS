import { createRouter, createWebHashHistory } from 'vue-router'
import api       from '@/service/api'
import store     from '@/store'
import route     from './route.js'

const router = createRouter({
	history: createWebHashHistory(),
	routes: route
})

let hasCheckedInit = false

const needInit = async () => {
	if (store.state.needInitialization) {
		return true
	}
	if (hasCheckedInit) {
		return false
	}
	try {
		let userStatusRes = await api.users.getUserStatus();
		if (userStatusRes.data.success === 200) {
			const data = userStatusRes.data.data
			if (data.initialized === false) {
				store.commit('SET_NEED_INITIALIZATION', true)
				store.commit('SET_INIT_KEY', data.key)
				localStorage.removeItem("access_token");
				localStorage.removeItem("refresh_token");
				return true
			}
			hasCheckedInit = true
			return false
		} else {
			return false
		}
	} catch (error) {
		console.error(error)
		return false
	}
}


router.beforeEach(async (to, from, next) => {
	// Embedded viewers bring their own token and have no login page to go to.
	if (to.meta.embed) return next();
	const accessToken = localStorage.getItem("access_token");
	const requireAuth = to.matched.some(record => record.meta.requireAuth);

	// 判断是否需要初始化
	let needInitRes = await needInit();

	if (to.path !== '/welcome') {
		if (needInitRes) {
			next('/welcome');
		} else {
			if (requireAuth && !accessToken) {
				next('/login');
			} else {
				switch (to.path) {
					case "/login":
						if (accessToken) {
							next('/');
							return;
						}
						break;

					case "/logout":
						hasCheckedInit = false;
						localStorage.removeItem("access_token");
						localStorage.removeItem("refresh_token");
						localStorage.removeItem("user");
						next('/login');
						return;
				}
				next();
			}
		}
	} else {
		if (needInitRes) {
			next();
		} else {
			next("/login");
		}
	}
});


export default router
