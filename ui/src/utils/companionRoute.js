// How the server reaches a companion phone's files right now - the device
// list's `route` (S-04). Labels are $t keys.
//
//   lan         direct, on the home network
//   tailscale   direct, over Tailscale (away from home, full speed)
//   tunnel      through the phone's own connection to the server (anywhere, slower)
//   tunnel_list an older app: folders list, files don't open
//   ''          online, but files can't be reached right now
export function companionRouteInfo(dev) {
	if (!dev || !dev.is_online || dev.connection === 'offline') return null
	switch (dev.route) {
		case 'lan':
			return { key: 'lan', icon: 'lan-connect', tone: 'ok', label: 'Direct · home network', hint: '' }
		case 'tailscale':
			return { key: 'tailscale', icon: 'vpn', tone: 'ok', label: 'Direct · Tailscale', hint: '' }
		case 'tunnel':
			return {
				key: 'tunnel',
				icon: 'swap-vertical',
				tone: 'info',
				label: 'Through the server tunnel · slower',
				hint: 'Away from home without Tailscale - files open and copy through the phone\'s connection to the server, more slowly. Turn on Tailscale on the phone for full speed.',
			}
		case 'tunnel_list':
			return {
				key: 'tunnel_list',
				icon: 'folder-alert-outline',
				tone: 'warn',
				label: 'Folders only',
				hint: 'The NivaroOS app on this phone is too old to send files away from home - update it, or turn on Tailscale on the phone.',
			}
		default:
			return {
				key: 'none',
				icon: 'lan-disconnect',
				tone: 'warn',
				label: 'Files unavailable',
				hint: 'The phone is online, but its files can\'t be reached right now - open the NivaroOS app on it and turn on file sharing.',
			}
	}
}
