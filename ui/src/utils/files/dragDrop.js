// src/utils/files/dragDrop.js
// Shared HTML5 drag-and-drop payload helpers for moving/copying files and
// folders around the Files app - between rows/tiles, across tabs, across
// windows, into the sidebar, and onto the desktop. Everything lives in one
// page (windows/tabs are just divs, not real separate browser windows), so
// a plain custom dataTransfer type carries the payload anywhere a drop can
// happen - no Vuex/EventBus plumbing needed for the payload itself.

export const FILES_DRAG_TYPE = 'application/x-casaos-files'

export function setFilesDragData(event, payload) {
	event.dataTransfer.effectAllowed = 'copyMove'
	event.dataTransfer.setData(FILES_DRAG_TYPE, JSON.stringify(payload))
}

// dataTransfer.getData() only returns real data on the `drop` event itself
// (browsers withhold it during dragover/dragenter for security) - `types`
// is readable throughout, so that's what dragover/dragenter handlers use
// to decide whether to show drop-target affordances at all.
export function isFilesDragEvent(event) {
	return !!(event.dataTransfer && Array.from(event.dataTransfer.types || []).includes(FILES_DRAG_TYPE))
}

export function getFilesDragData(event) {
	try {
		const raw = event.dataTransfer.getData(FILES_DRAG_TYPE)
		return raw ? JSON.parse(raw) : null
	} catch (e) {
		return null
	}
}
