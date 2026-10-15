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
		// The app's WebView: only the Smooth (WebRTC) viewer, token from
		// window.__nivaroosToken (see views/HostDesktopEmbed.vue).
		path: '/host-desktop/embed',
		name: 'HostDesktopEmbed',
		hidden: true,
		component: () => import('@/views/HostDesktopEmbed.vue'),
		meta: {
			requireAuth: false,
			showBackground: false,
			embed: true
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