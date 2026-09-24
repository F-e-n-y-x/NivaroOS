// Per-tab back/forward history for the lite browser. It can't use the
// frame's own history: history.back() inside a frame walks the whole
// browser tab's joint history, so on a tab's first page (every typed
// address loads in a fresh frame) it took NivaroOS itself back, and with
// several tabs it stepped through the other tabs' pages. Each tab keeps its
// own list here and Back/Forward load that page instead.

export const MAX_ENTRIES = 100

export function createHistory() {
	return { entries: [], index: -1, pending: '' }
}

// Records a page the tab arrived at. A visit that comes from our own
// Back/Forward (pending) only moves the cursor; a new page drops the
// forward entries, like any browser.
export function recordVisit(h, url) {
	if (!url) return
	if (h.pending) {
		const target = h.pending
		h.pending = ''
		if (h.entries[h.index] === target) {
			// The page may have redirected; keep the address it ended up at.
			h.entries[h.index] = url
			return
		}
	}
	if (h.entries[h.index] === url) return
	// The browser's own Back/Forward can still step inside a page's frame
	// (link clicks there enter the browser's history): follow it.
	if (h.index > 0 && h.entries[h.index - 1] === url) {
		h.index--
		return
	}
	if (h.index < h.entries.length - 1 && h.entries[h.index + 1] === url) {
		h.index++
		return
	}
	h.entries = h.entries.slice(0, h.index + 1)
	h.entries.push(url)
	if (h.entries.length > MAX_ENTRIES) h.entries.splice(0, h.entries.length - MAX_ENTRIES)
	h.index = h.entries.length - 1
}

export function canGo(h, dir) {
	return dir === 'back' ? h.index > 0 : h.index >= 0 && h.index < h.entries.length - 1
}

// Moves the cursor and returns the address to load, or '' when there's
// nowhere to go.
export function go(h, dir) {
	if (!canGo(h, dir)) return ''
	h.index += dir === 'back' ? -1 : 1
	h.pending = h.entries[h.index]
	return h.pending
}
