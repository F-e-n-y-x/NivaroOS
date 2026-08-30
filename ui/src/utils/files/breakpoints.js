const SIDEBAR_TOOLBAR_THRESHOLD = 560
const SINGLE_COLUMN_THRESHOLD = 420

export function classifyWidth(width) {
	const collapsed = width < SIDEBAR_TOOLBAR_THRESHOLD
	return {
		sidebarCollapsed: collapsed,
		toolbarCollapsed: collapsed,
		singleColumnGrid: width < SINGLE_COLUMN_THRESHOLD,
	}
}
