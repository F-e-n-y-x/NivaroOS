export default [
	{
		path: '/login',
		name: 'Login',
		hidden: true,
		component: () => import('@/views/Login.vue'),
		meta: {
			requireAuth: false,
			showBackground: true
		}
	},
	{
		path: '/welcome',
		name: 'Welcome',
		hidden: true,
		component: () => import('@/views/Welcome.vue'),
		meta: {
			requireAuth: false,
			showBackground: true
		}
	},
	{
		path: '/',
		name: 'Home',
		hidden: true,
		component: () => import('@/views/Home.vue'),
		meta: {
			requireAuth: true,
			showBackground: true,
			showWindows: true
		}
	},
	{
		path: '/vm-console/:name',
		name: 'VmConsoleStandalone',
		hidden: true,
		component: () => import('@/views/VmConsoleStandalone.vue'),
		props: true,
		meta: {
			requireAuth: true,
			showBackground: false
		}
	},
	{
		path: '/host-desktop',
		name: 'HostDesktopStandalone',
		hidden: true,
		component: () => import('@/views/HostDesktopStandalone.vue'),
		meta: {
			requireAuth: true,
			showBackground: false
		}
	},
	{
		path: '/launch',
		name: 'AppLauncherCheck',
		hidden: true,
		component: () => import('@/views/AppLauncherCheck.vue'),
		meta: {
			requireAuth: false,
			showBackground: false
		}
	},
]