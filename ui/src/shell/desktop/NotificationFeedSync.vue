<!--
	Keeps the Notification Center in step with the server's persisted
	notification feed (service/notificationFeed.js): loads it once signed
	in, adds new entries from the socket, and reloads when this user's read
	state changed elsewhere or the socket reconnected. Renders nothing;
	mounted once by WindowManager, so the desktop tray and the phone's
	Alerts screen both see it.
-->
<script>
import { notificationFeed } from '@/service/notifications'
import { FEED_EVENTS } from '@/service/notificationFeed'

export default {
	name: 'notification-feed-sync',
	render() {
		return null
	},
	created() {
		this.unwatch = this.$store.watch(
			state => state.access_token,
			(token, old) => {
				if (token && token !== old) notificationFeed.load()
				else if (!token) notificationFeed.reset()
			}
		)
		if (this.hasToken()) notificationFeed.load()
	},
	beforeDestroy() {
		if (this.unwatch) this.unwatch()
	},
	methods: {
		hasToken() {
			try {
				return !!(this.$store.state.access_token || localStorage.getItem('access_token'))
			} catch (e) {
				return !!this.$store.state.access_token
			}
		}
	},
	sockets: {
		connect() {
			notificationFeed.onReconnect()
		},
		[FEED_EVENTS.CREATED](res) {
			notificationFeed.onCreated(res && (res.Properties || res.properties))
		},
		[FEED_EVENTS.STATE](res) {
			notificationFeed.onState(res && (res.Properties || res.properties))
		}
	}
}
</script>
