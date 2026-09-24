import { apiErrorHtml } from './apiError'

export default {
	methods: {
		openAppToNewWindow(appInfo) {
			this.hasNewTag(appInfo.name) ? this.firstOpenThirdApp(appInfo) : this.openThirdApp(appInfo, true);
		},
		openThirdApp(appInfo, isNewWindows) {
			this.$messageBus('apps_open', appInfo.name);
			if (appInfo.hostname !== "" || appInfo.port !== "" || appInfo.index !== "") {
				const hostIp = appInfo.hostname || this.$baseIp
				const scheme = appInfo.scheme || 'http'
				const port = appInfo.port ? `:${appInfo.port}` : ''
				const url = `${scheme}://${hostIp}${port}${appInfo.index}`

				if (isNewWindows) {
					window.open(url);
				} else {
					let html = document.createElement('a');
					html.href = url;
					html.rel = 'noreferrer';
					document.getElementById('app').appendChild(html)
					html.click();
				}
			}
		},
		async openThirdContainerByAppInfo(appInfo) {
			try {
				const allinfo = await this.$openAPI.appManagement.compose.myComposeApp(appInfo.id).then(res => {
					return res.data.data
				})

				const compose = (allinfo && allinfo.compose) || {}
				const services = compose.services || {}
				const containerInfoV2 = (allinfo && allinfo.store_info) || {}
				// The main service key is whatever x-casaos.main names - not
				// necessarily the app id - so fall back to the first service.
				const mainName = (compose['x-casaos'] && compose['x-casaos'].main) || containerInfoV2.main
				const mainService = services[mainName] || services[appInfo.id] || services[Object.keys(services)[0]] || {}
				const app = {
					"id": appInfo.id,
					"name": appInfo.id,
					scheme: containerInfoV2.scheme,
					hostname: containerInfoV2.hostname || this.$baseIp,
					port: containerInfoV2.port_map,
					index: containerInfoV2.index,
					image: mainService.image || '',
				}

				const status = String((allinfo && allinfo.status) || '')
				if (status.indexOf('running') === -1) {
					await this.$openAPI.appManagement.compose.setComposeAppStatus(compose.name || appInfo.id, 'start')
					this.firstOpenThirdApp(app)
				} else {
					this.openAppToNewWindow(app)
				}
			} catch (e) {
				console.error(e);
				if (this.$buefy && this.$buefy.toast) {
					this.$buefy.toast.open({
						message: apiErrorHtml(e, this.$t('Unable to open the app')),
						type: 'is-danger',
						position: 'is-top',
						duration: 5000
					})
				}
			}

		},
		firstOpenThirdApp(appInfo) {
			this.removeIdFromSessionStorage(appInfo.name);
			let routeUrl = this.$router.resolve({
				name: 'AppLauncherCheck',
				path: '/launch',
				query: {
					appDetailData: JSON.stringify(appInfo)
				}
			});
			window.open(routeUrl.href, '_blank');
		}
	}
}