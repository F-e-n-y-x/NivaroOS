<template>
	<span class="user-avatar-circle" :style="{ width: size, height: size, fontSize: `calc(${size} * 0.4)` }" aria-hidden="true">
		<img v-if="shownSrc" :src="shownSrc" alt="" draggable="false" @error="failed = shownSrc" />
		<template v-else>{{ letters }}</template>
	</span>
</template>

<script>
import { avatarUrl, initials } from '@/utils/avatar'

// A user's picture, or their initials on the brand blue. Decorative: the
// name is always written next to it.
export default {
	name: 'user-avatar',
	props: {
		username: { type: String, default: '' },
		// avatar_version: '' = none, undefined = unknown (tries the server)
		version: { type: String, default: undefined },
		// a picture to show as-is (a data: URL, e.g. on the login screen)
		src: { type: String, default: '' },
		size: { type: String, default: '2.5rem' }
	},
	data() {
		return { failed: '' }
	},
	computed: {
		url() {
			if (this.src) return this.src
			const token = this.$store.state.access_token || localStorage.getItem('access_token')
			return avatarUrl(this.username, this.version, token)
		},
		shownSrc() {
			return this.url && this.url !== this.failed ? this.url : ''
		},
		letters() {
			return initials(this.username)
		}
	}
}
</script>

<style scoped>
.user-avatar-circle {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	flex-shrink: 0;
	border-radius: 50%;
	overflow: hidden;
	/* white on #2563EB is 5.2:1 */
	background: #2563eb;
	color: #ffffff;
	font-weight: 600;
	line-height: 1;
	user-select: none;
}

.user-avatar-circle img {
	width: 100%;
	height: 100%;
	object-fit: cover;
}
</style>
