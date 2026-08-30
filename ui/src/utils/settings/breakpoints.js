const NAV_COLLAPSE_THRESHOLD = 736
const ROW_STACK_THRESHOLD = 544

export function classifyWidth(width) {
	return {
		navCollapsed: width < NAV_COLLAPSE_THRESHOLD,
		rowsStacked: width < ROW_STACK_THRESHOLD,
	}
}
