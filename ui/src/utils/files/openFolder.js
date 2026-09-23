// Opens a Files window already navigated to `path`.
//
// FilesApp seeds its first tab from $store.state.currentPath when it's
// created, so re-focusing the existing 'files' window (the old approach of
// several callers) silently ignored the path. A new window always lands
// where it was asked to.
export function openFolderWindow(store, path, title = 'Files') {
	if (path) store.commit('SET_CURRENT_PATH', path)
	store.commit('OPEN_WINDOW', {
		id: 'files-' + Date.now(),
		title,
		component: 'FilesApp',
		width: 960,
		height: 620,
	})
}
