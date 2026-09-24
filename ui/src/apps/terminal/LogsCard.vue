<template>
	<div class="logs scrollbars" tabindex="0" role="log" :aria-label="$t('NivaroOS logs')" @scroll="onScroll">
		<p v-if="error" class="logs-error" role="alert">{{ error }}</p>
		<!-- Plain text on purpose: log lines are untrusted (they contain
		     request paths, container output...), so they are never parsed as
		     HTML, and the pane is read-only. -->
		<pre class="content">{{ data }}</pre>
	</div>
</template>

<script>
// Within this many px of the bottom counts as "following" the log.
const FOLLOW_SLACK_PX = 48

export default {
	name: "logs-card",
	props: {
		data: String,
		error: String,
	},
	data() {
		return {
			state: true,
			follow: true,
		}
	},
	watch: {
		data() {
			if (this.follow && this.state) this.srcollToBottom()
		}
	},
	methods: {
		onScroll() {
			const el = this.$el
			this.follow = el.scrollHeight - el.scrollTop - el.clientHeight <= FOLLOW_SLACK_PX
		},
		active(state) {
			this.state = state;
			if (state && this.follow) {
				this.srcollToBottom();
			}
		},
		srcollToBottom() {
			// Scoped to this component's own root: every Terminal window has
			// its own Logs pane.
			this.$nextTick(() => {
				if (this.$el) this.$el.scrollTop = this.$el.scrollHeight
			})
		}
	},
}
</script>

<style lang="scss" scoped>
.logs {
	width: 100%;
	height: 100%;
	color: #f4f4f5;
	padding: var(--space-2) var(--space-3);
	overflow-y: auto;
	overflow-x: hidden;
	box-sizing: border-box;
	background: #1e1e1e;

	&:focus-visible {
		outline: 2px solid #93c5fd;
		outline-offset: -2px;
	}
}

.content {
	margin: 0;
	padding: 0;
	background: transparent;
	color: inherit;
	white-space: pre-wrap;
	word-break: break-word;
	font-size: 13px;
	font-family: 'Monaco', 'Consolas', monospace !important;
	line-height: 1.5em;
}

.logs-error {
	margin-bottom: var(--space-2);
	color: #fca5a5;
	font-size: var(--font-xs);
}
</style>
