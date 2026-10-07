// Pure helpers for the desktop Network widget (shell/widgets/Network.vue).

// A rate in KB/s -> { value, unit }, 1024-based like the rest of the UI.
export function formatRate(kb) {
	const bytes = (parseFloat(kb) || 0) * 1024;
	if (!(bytes >= 1024)) return { value: bytes > 0 ? String(Math.round(bytes)) : '0', unit: 'B/s' };
	const units = ['B/s', 'KB/s', 'MB/s', 'GB/s', 'TB/s'];
	const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
	return { value: String(parseFloat((bytes / 1024 ** i).toFixed(1))), unit: units[i] };
}

// Kernel operstate -> 'up' | 'down' | 'unknown'.
export function stateClass(state) {
	const st = String(state || '').trim().toLowerCase();
	if (st === 'up') return 'up';
	if (st === 'down' || st === 'lowerlayerdown' || st === 'notpresent') return 'down';
	return 'unknown';
}

// The interface to show when the stored one is missing: first with link up.
export function defaultInterface(list) {
	return list.find(n => stateClass(n.state) === 'up') || list[0] || null;
}

// IPv4 of `name` from GET /v1/sys/network-interfaces ([{interface, ip}]).
export function ipOf(interfaces, name) {
	const hit = (interfaces || []).find(i => i && i.interface === name);
	return (hit && hit.ip) || '';
}

// Line + closed-area SVG paths for `values` scaled to `max` in a w x h box.
export function sparkPath(values, max, w, h) {
	const n = values.length;
	if (n < 2 || !max) return { line: `M0 ${h - 1} L${w} ${h - 1}`, area: '' };
	const step = w / (n - 1);
	let line = '';
	for (let i = 0; i < n; i++) {
		const y = (h - 1 - (values[i] / max) * (h - 4)).toFixed(1);
		line += (i ? ' L' : 'M') + (step * i).toFixed(1) + ' ' + y;
	}
	return { line, area: `${line} L${w} ${h} L0 ${h} Z` };
}
