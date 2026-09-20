import Vue       from 'vue'
import VueRouter from 'vue-router'
import api       from '@/service/api'
import store     from '@/store'
import route     from './route.js'

Vue.use(VueRouter)

const routes = route

const router = new VueRouter({
	mode: 'hash',
	base: process.env.BASE_URL,
	routes
})

const originalPush = VueRouter.prototype.push
VueRouter.prototype.push = function push(location) {
	return originalPush.call(this, location).catch((err) => err)
}

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
			if (!data.initialized) {
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
	const accessToken = localStorage.getItem("access_token");
	const version = localStorage.getItem("version");
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

					default:
						if (version == null) {
							localStorage.removeItem("access_token");
							next('/login');
							return;
						}
						break;
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
