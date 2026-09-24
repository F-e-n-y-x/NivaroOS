// The web UI's one notification feed (see notificationFeed.js), wired to
// the shared axios instance (JWT, 401 refresh), activityService and the
// app's i18n. Started by shell/desktop/NotificationFeedSync.vue.
import { instance } from './service.js'
import activityService from './activity'
import { createNotificationFeed } from './notificationFeed'
import i18n from '@/plugins/i18n'
import { ice_i18n } from '@/mixins/base/common-i18n'
import { renderMessage } from '@/apps/backup/messages'
import { backupWindow } from '@/apps/backup/windows'

function currentUserId() {
	try {
		const user = JSON.parse(localStorage.getItem('user') || 'null')
		return user && user.id !== undefined && user.id !== null ? user.id : null
	} catch (e) {
		return null
	}
}

const t = (key, args) => i18n.t(key, args)

export const notificationFeed = createNotificationFeed({
	transport: instance,
	activity: activityService,
	currentUserId,
	deps: () => ({
		t,
		te: key => i18n.te(key),
		renderBackupMessage: renderMessage,
		backupWindow,
		pickI18n: ice_i18n
	})
})

export default notificationFeed
