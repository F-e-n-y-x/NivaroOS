// "Sign-in didn't stick?" - turning what a sign-in window noticed (the
// server's diag: cookie names/domains seen, cookies the browser refused,
// how often the provider rejected the sign-in) into a plain explanation and
// a copyable report. Never contains a cookie value: the server never sends one.

export function isStuck(diag) {
	return !!(diag && diag.stuck)
}

// One sentence on the most likely cause, most specific first.
export function stuckReason(diag, provider, t) {
	const d = diag || {}
	const blocked = d.blocked || []
	if (blocked.some(b => /ThirdParty|SameSite|Partition/i.test(b))) {
		return t('The browser refused some of the cookies {provider} sets while signing in, so the login page starts over.', { provider })
	}
	if (blocked.length) return t('The browser refused some of the cookies {provider} sets while signing in.', { provider })
	if (d.lost) return t('{provider} signed this window out again right after signing in.', { provider })
	if ((d.rejected || 0) >= 2) return t('{provider} did not accept the sign-in when it was checked.', { provider })
	return t('The {provider} login page keeps reloading without signing in.', { provider })
}

// A report to paste into a bug report: counts, domains and cookie names only.
export function diagReport(diag, provider) {
	const d = diag || {}
	const lines = [
		`${provider} sign-in diagnostics`,
		`page loads: ${d.loads || 0}, seconds: ${d.seconds || 0}, rejected by ${provider}: ${d.rejected || 0}, signed out again: ${d.lost ? 'yes' : 'no'}`,
		'refused cookies:'
	]
	for (const b of d.blocked || []) lines.push('  ' + b)
	if (!(d.blocked || []).length) lines.push('  (none)')
	lines.push('cookies in the window (names only):')
	for (const s of d.seen || []) lines.push('  ' + s)
	if (!(d.seen || []).length) lines.push('  (none)')
	return lines.join('\n')
}
