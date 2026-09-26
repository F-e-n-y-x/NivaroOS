// "Free up memory" (POST /v1/sys/memory/clear): the pure parts the window
// shows - sizes, the headline and the before -> after rows.

// "2.1 GB": binary units, one decimal (none for bytes).
export function formatBytes(bytes) {
	const n = Number(bytes) || 0
	if (n <= 0) return "0 B"
	const units = ["B", "KB", "MB", "GB", "TB", "PB"]
	const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
	return i === 0 ? `${n} B` : `${(n / 1024 ** i).toFixed(1)} ${units[i]}`
}

function snapshot(s) {
	s = s || {}
	return {
		free: Number(s.mem_free) || 0,
		available: Number(s.mem_available) || 0,
		cached: (Number(s.cached) || 0) + (Number(s.buffers) || 0),
		swapUsed: Number(s.swap_used) || 0,
	}
}

// The server's answer as what the window shows: a headline and rows of
// [label, before, after]. Swap only gets a row when it changed.
export function describeClear(data) {
	const d = data || {}
	const before = snapshot(d.before)
	const after = snapshot(d.after)
	const freed = Number(d.freed) || 0
	const rows = [
		{ key: "free", label: "Free", before: formatBytes(before.free), after: formatBytes(after.free) },
		{ key: "cache", label: "Cache and buffers", before: formatBytes(before.cached), after: formatBytes(after.cached) },
	]
	if (d.swap_reclaimed || before.swapUsed !== after.swapUsed) {
		rows.push({ key: "swap", label: "Swap in use", before: formatBytes(before.swapUsed), after: formatBytes(after.swapUsed) })
	}
	return {
		freed,
		alreadyClear: freed < 1024 * 1024,
		freedText: formatBytes(freed),
		rows,
		note: typeof d.note === "string" ? d.note : "",
	}
}
