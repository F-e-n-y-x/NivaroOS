// Helpers for Settings > HTTPS on your network (LanHttpsPanel.vue).

// The dashboard's https address on the same host the browser used.
export function httpsUrl(loc, port) {
	const p = String(port || '443')
	return `https://${loc.hostname}${p === '443' ? '' : ':' + p}/`
}

// The redirect switch is offered only on an https page served by the box's
// own listener (proof this browser trusts the certificate) - not on the
// tailnet's ts.net address, which uses a different certificate.
export function canEnableRedirect(loc, status) {
	return !!(status && status.enabled && status.running) && loc.protocol === 'https:' && !/\.ts\.net$/i.test(loc.hostname)
}
