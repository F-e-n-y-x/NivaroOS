<template>
	<div class="session-list" :class="{ 'is-compact': compact }">
		<div v-if="!hideHeader" class="sl-header">
			<span class="sl-heading">{{ heading || $t('Sessions') }}</span>
			<button type="button" class="sl-icon-btn" :class="{ 'is-spinning': loading }" :title="$t('Refresh')" :aria-label="$t('Refresh sessions')" @click="$emit('refresh')">
				<i class="mdi mdi-refresh" aria-hidden="true"></i>
			</button>
			<button v-if="showClose" type="button" class="sl-icon-btn" :title="$t('Hide sessions')" :aria-label="$t('Hide sessions')" @click="$emit('close')">
				<i class="mdi mdi-close" aria-hidden="true"></i>
			</button>
		</div>

		<button v-if="newLabel" type="button" class="sl-new" @click="$emit('new')">
			<i class="mdi mdi-plus" aria-hidden="true"></i><span>{{ newLabel }}</span>
		</button>

		<p v-if="error" class="sl-note is-error" role="alert">{{ error }}</p>
		<p v-else-if="!sessions.length && !loading" class="sl-note">{{ emptyText || $t('No terminal sessions are running.') }}</p>

		<div v-for="group in groups" :key="group.key" class="sl-group">
			<p v-if="groups.length > 1 || group.showLabel" class="sl-group-label">
				<i :class="['mdi', group.icon]" aria-hidden="true"></i>{{ group.label }}
			</p>
			<ul class="sl-rows">
				<li v-for="s in group.sessions" :key="keyOf(s)" class="sl-row"
					:class="{ 'is-current': keyOf(s) === currentKey, 'is-open': isOpenHere(s), 'is-exited': s.state === 'exited' }">
					<template v-if="confirmKey === keyOf(s)">
						<div class="sl-confirm" role="group" :aria-label="$t('End session')">
							<span class="sl-confirm-text">{{ confirmText(s) }}</span>
							<button type="button" class="sl-btn is-danger" @click="doKill(s)">{{ $t('End') }}</button>
							<button ref="confirmCancel" type="button" class="sl-btn" @click="confirmKey = ''">{{ $t('Cancel') }}</button>
						</div>
					</template>
					<template v-else-if="renameKey === keyOf(s)">
						<form class="sl-rename" @submit.prevent="commitRename(s)">
							<input ref="renameInput" v-model="renameValue" type="text" maxlength="80" class="sl-rename-input" :aria-label="$t('Session name')"
								@keydown.esc.prevent.stop="renameKey = ''" @blur="commitRename(s)">
						</form>
					</template>
					<template v-else>
						<button type="button" class="sl-main" :title="rowTitle(s)" @click="$emit('open', s)">
							<span class="sl-dot" :class="dotClass(s)" aria-hidden="true"></span>
							<span class="sl-text">
								<span class="sl-title">{{ s.title }}</span>
								<span class="sl-sub">{{ subtitle(s) }}</span>
							</span>
							<span class="sl-meta">{{ meta(s) }}</span>
						</button>
						<div class="sl-actions">
							<button v-if="s.state !== 'exited'" type="button" class="sl-icon-btn" :title="$t('Rename')" :aria-label="$t('Rename {name}', { name: s.title })" @click="startRename(s)">
								<i class="mdi mdi-pencil-outline" aria-hidden="true"></i>
							</button>
							<button type="button" class="sl-icon-btn is-danger"
								:title="s.state === 'exited' ? $t('Dismiss') : $t('End session')"
								:aria-label="(s.state === 'exited' ? $t('Dismiss') : $t('End session')) + ': ' + s.title"
								@click="askKill(s)">
								<i :class="['mdi', s.state === 'exited' ? 'mdi-close' : 'mdi-close-circle-outline']" aria-hidden="true"></i>
							</button>
						</div>
					</template>
				</li>
			</ul>
		</div>

		<p v-if="footnote" class="sl-footnote">{{ footnote }}</p>
	</div>
</template>

<script>
import { sessionKey, sessionSubtitle, relativeTime, exitSummary, isIdleShell } from './termSessions.js'

export default {
	name: 'terminal-session-list',
	props: {
		sessions: { type: Array, default: () => [] },
		loading: { type: Boolean, default: false },
		error: { type: String, default: '' },
		// Session keys ("host:<id>") shown in this window, and the one on screen.
		openKeys: { type: Array, default: () => [] },
		currentKey: { type: String, default: '' },
		heading: { type: String, default: '' },
		newLabel: { type: String, default: '' },
		emptyText: { type: String, default: '' },
		footnote: { type: String, default: '' },
		hideHeader: { type: Boolean, default: false },
		compact: { type: Boolean, default: false },
		showClose: { type: Boolean, default: false },
		// Group container sessions by container (the Terminal app); off in a
		// single container's console.
		groupByContainer: { type: Boolean, default: true },
	},
	data() {
		return {
			now: Date.now(),
			confirmKey: '',
			renameKey: '',
			renameValue: '',
		}
	},
	computed: {
		groups() {
			if (!this.groupByContainer) {
				return [{ key: 'all', label: '', icon: '', sessions: this.sessions }]
			}
			const host = this.sessions.filter((s) => s.kind !== 'container')
			const byContainer = new Map()
			for (const s of this.sessions) {
				if (s.kind !== 'container') continue
				const name = s.container || (s.container_id || '').slice(0, 12) || this.$t('Container')
				if (!byContainer.has(name)) byContainer.set(name, [])
				byContainer.get(name).push(s)
			}
			const groups = []
			if (host.length) groups.push({ key: 'host', label: this.$t('This server'), icon: 'mdi-server', sessions: host })
			for (const [name, list] of byContainer) {
				groups.push({ key: 'c:' + name, label: name, icon: 'mdi-docker', sessions: list, showLabel: true })
			}
			return groups
		},
	},
	mounted() {
		this.clock = setInterval(() => { this.now = Date.now() }, 30000)
	},
	beforeDestroy() {
		clearInterval(this.clock)
	},
	methods: {
		keyOf(s) {
			return sessionKey(s)
		},
		isOpenHere(s) {
			return this.openKeys.includes(sessionKey(s))
		},
		dotClass(s) {
			if (s.state === 'exited') return 'is-exited'
			if (s.clients > 0) return 'is-attached'
			return 'is-detached'
		},
		subtitle(s) {
			if (s.state === 'exited') {
				return this.$t(exitSummary({ code: s.exit_code, reason: s.exit_reason }), { code: s.exit_code })
			}
			return sessionSubtitle(s)
		},
		meta(s) {
			if (s.state === 'exited') return this.$t(relativeTime(s.exited_at, this.now))
			if (this.isOpenHere(s)) return this.$t('open')
			if (s.clients > 1) return this.$t('{n} viewers', { n: s.clients })
			if (s.clients === 1) return this.$t('open elsewhere')
			return this.$t(relativeTime(s.last_activity_at || s.created_at, this.now))
		},
		rowTitle(s) {
			const bits = [s.title]
			if (s.cwd) bits.push(s.cwd)
			if (s.legacy) bits.push(this.$t('opened by an older app'))
			return bits.join('\n')
		},
		confirmText(s) {
			if (s.state === 'exited') return this.$t('Dismiss?')
			if (!isIdleShell(s)) return this.$t('{cmd} is running. End it?', { cmd: String(s.command).split('/').pop() })
			return this.$t('End this session?')
		},
		askKill(s) {
			// An exited session is only a list entry: dismiss at once.
			if (s.state === 'exited') {
				this.$emit('kill', s)
				return
			}
			this.renameKey = ''
			this.confirmKey = sessionKey(s)
			this.$nextTick(() => {
				const b = this.$refs.confirmCancel
				const el = Array.isArray(b) ? b[0] : b
				if (el) el.focus()
			})
		},
		doKill(s) {
			this.confirmKey = ''
			this.$emit('kill', s)
		},
		startRename(s) {
			this.confirmKey = ''
			this.renameKey = sessionKey(s)
			this.renameValue = s.title
			this.$nextTick(() => {
				const r = this.$refs.renameInput
				const el = Array.isArray(r) ? r[0] : r
				if (el) {
					el.focus()
					el.select()
				}
			})
		},
		commitRename(s) {
			if (this.renameKey !== sessionKey(s)) return
			this.renameKey = ''
			const title = this.renameValue.trim()
			if (title && title !== s.title) this.$emit('rename', s, title)
		},
	},
}
</script>

<style lang="scss" scoped>
.session-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	min-height: 0;
	color: #e4e4e7;
	font-size: var(--font-xs);
}

.sl-header {
	display: flex;
	align-items: center;
	gap: 2px;
}

.sl-heading {
	flex: 1 1 auto;
	font-weight: 600;
	font-size: var(--font-sm);
	color: #f4f4f5;
}

.sl-icon-btn {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 1.6rem;
	height: 1.6rem;
	padding: 0;
	border: none;
	border-radius: var(--radius-sm);
	background: transparent;
	color: #a1a1aa;
	cursor: pointer;

	&:hover {
		background: rgba(255, 255, 255, 0.1);
		color: #fff;
	}

	&.is-danger:hover {
		color: #fca5a5;
	}

	&.is-spinning .mdi {
		animation: sl-spin 0.9s linear infinite;
	}

	&:focus-visible {
		outline: 2px solid var(--console-accent);
		outline-offset: 1px;
	}
}

@keyframes sl-spin {
	to {
		transform: rotate(360deg);
	}
}

.sl-new {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	height: 2rem;
	padding: 0 var(--space-3);
	border: 1px dashed rgba(255, 255, 255, 0.22);
	border-radius: var(--radius-sm);
	background: transparent;
	color: #e4e4e7;
	font: inherit;
	cursor: pointer;

	&:hover {
		background: rgba(255, 255, 255, 0.06);
		border-color: rgba(255, 255, 255, 0.35);
	}

	&:focus-visible {
		outline: 2px solid var(--console-accent);
		outline-offset: 1px;
	}
}

.sl-note {
	color: #a1a1aa;
	line-height: 1.4;

	&.is-error {
		color: #fca5a5;
	}
}

.sl-group-label {
	display: flex;
	align-items: center;
	gap: var(--space-1);
	margin-bottom: 2px;
	color: #a1a1aa;
	font-size: 0.68rem;
	font-weight: 600;
	letter-spacing: 0.04em;
	text-transform: uppercase;
}

.sl-rows {
	display: flex;
	flex-direction: column;
	gap: 2px;
	margin: 0;
	padding: 0;
	list-style: none;
}

.sl-row {
	position: relative;
	display: flex;
	align-items: center;
	min-height: 2.6rem;
	border-radius: var(--radius-sm);

	&:hover,
	&:focus-within {
		background: rgba(255, 255, 255, 0.06);

		.sl-actions {
			opacity: 1;
		}
	}

	&.is-current {
		background: rgba(125, 249, 197, 0.14);
	}

	&.is-exited .sl-title {
		color: #a1a1aa;
	}
}

.sl-main {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-1) var(--space-2);
	border: none;
	border-radius: var(--radius-sm);
	background: transparent;
	color: inherit;
	font: inherit;
	text-align: left;
	cursor: pointer;

	&:focus-visible {
		outline: 2px solid var(--console-accent);
		outline-offset: -2px;
	}
}

.sl-dot {
	flex-shrink: 0;
	width: 0.5rem;
	height: 0.5rem;
	border-radius: 50%;

	&.is-attached {
		background: #22c55e;
	}

	&.is-detached {
		background: transparent;
		border: 2px solid var(--console-accent);
	}

	&.is-exited {
		background: #52525b;
	}
}

.sl-text {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
}

.sl-title,
.sl-sub {
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.sl-title {
	color: #f4f4f5;
	font-weight: 500;
}

.sl-sub {
	color: #a1a1aa;
	font-family: "Fira Code", "JetBrains Mono", Menlo, Consolas, monospace;
	font-size: 0.68rem;
}

.sl-meta {
	flex-shrink: 0;
	color: #71717a;
	font-size: 0.68rem;
	white-space: nowrap;
}

.sl-actions {
	flex-shrink: 0;
	display: flex;
	padding-right: 2px;
	opacity: 0;
	transition: opacity 0.12s ease;
}

@media (hover: none) {
	.sl-actions {
		opacity: 1;
	}
}

.sl-confirm,
.sl-rename {
	flex: 1 1 auto;
	display: flex;
	align-items: center;
	gap: var(--space-1);
	padding: var(--space-1) var(--space-2);
}

.sl-confirm-text {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.sl-btn {
	flex-shrink: 0;
	height: 1.6rem;
	padding: 0 var(--space-2);
	border: none;
	border-radius: var(--radius-sm);
	background: rgba(255, 255, 255, 0.1);
	color: #f4f4f5;
	font: inherit;
	font-weight: 600;
	cursor: pointer;

	&:hover {
		background: rgba(255, 255, 255, 0.18);
	}

	&.is-danger {
		background: #b91c1c;

		&:hover {
			background: #991b1b;
		}
	}

	&:focus-visible {
		outline: 2px solid var(--console-accent);
		outline-offset: 1px;
	}
}

.sl-rename-input {
	flex: 1 1 auto;
	min-width: 0;
	height: 1.8rem;
	padding: 0 var(--space-2);
	border: 1px solid var(--console-accent);
	border-radius: var(--radius-sm);
	outline: none;
	background: var(--console-bg);
	color: #f4f4f5;
	font: inherit;
}

.sl-footnote {
	margin-top: auto;
	padding-top: var(--space-2);
	color: #71717a;
	font-size: 0.68rem;
	line-height: 1.4;
}
</style>
