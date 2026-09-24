<!--
	Clipboard popover for the VM console and Host Desktop toolbars: paste
	the browser clipboard into the remote machine, send or type any text,
	and a history of everything sent to ("to") and copied on ("from") any
	remote machine. The history is shared by all consoles (see
	service/remoteClipboard.js).

	The parent owns the connection: it gets `send` (put text on the remote
	clipboard) and `type` (type it as key presses) and records what the
	remote copies itself.
-->
<template>
	<div
		ref="panel"
		class="rclip"
		role="dialog"
		:aria-label="$t('Clipboard')"
		tabindex="-1"
		@keydown.esc.stop="$emit('close')"
	>
		<div class="rclip-head">
			<span class="rclip-title">{{ $t('Clipboard') }}</span>
			<button type="button" class="rclip-icon-btn" :aria-label="$t('Close')" @click="$emit('close')">
				<b-icon icon="close" custom-size="mdi-16px"></b-icon>
			</button>
		</div>

		<button type="button" class="rclip-btn is-primary rclip-paste" :disabled="!connected" @click="pasteBrowserClipboard">
			<b-icon icon="content-paste" custom-size="mdi-16px"></b-icon>
			<span>{{ $t('Paste my clipboard into {target}', { target: targetLabel }) }}</span>
		</button>

		<p v-if="syncHint" class="rclip-note" role="note">
			<b-icon icon="information-outline" custom-size="mdi-14px"></b-icon>
			<span>{{ syncHint }}</span>
		</p>

		<label class="rclip-label" :for="inputId">{{ $t('Or type the text to send') }}</label>
		<textarea
			:id="inputId"
			ref="input"
			v-model="draft"
			class="rclip-input"
			rows="3"
			:placeholder="$t('Paste or type text here...')"
			@keydown.enter.ctrl.exact.prevent="sendDraft"
			@keydown.enter.meta.exact.prevent="sendDraft"
		></textarea>
		<p v-if="readBlocked" class="rclip-note is-warning" role="status">
			{{ $t("This browser didn't allow reading your clipboard here. Press Ctrl+V in the box above, then send it.") }}
		</p>
		<div class="rclip-actions">
			<button
				type="button"
				class="rclip-btn"
				:disabled="!connected || !draft"
				:title="$t('Types the text as key presses - works on login screens and without guest tools')"
				@click="typeDraft"
			>
				<b-icon icon="keyboard-outline" custom-size="mdi-16px"></b-icon>
				<span>{{ $t('Type it') }}</span>
			</button>
			<button type="button" class="rclip-btn is-primary" :disabled="!connected || !draft" @click="sendDraft">
				<b-icon icon="send-outline" custom-size="mdi-16px"></b-icon>
				<span>{{ $t('Send to clipboard') }}</span>
			</button>
		</div>

		<div class="rclip-history-head">
			<span class="rclip-subtitle" :id="historyId">{{ $t('History') }}</span>
			<div class="rclip-filter" role="group" :aria-label="$t('Show')">
				<button
					v-for="f in filters"
					:key="f.key"
					type="button"
					class="rclip-chip"
					:class="{ active: filter === f.key }"
					:aria-pressed="filter === f.key ? 'true' : 'false'"
					@click="filter = f.key"
				>{{ f.label }}</button>
			</div>
		</div>

		<ul v-if="visible.length" class="rclip-list" :aria-labelledby="historyId">
			<li v-for="item in visible" :key="item.id" class="rclip-item">
				<div class="rclip-item-meta">
					<b-icon
						:icon="item.direction === 'to' ? 'arrow-top-right' : 'arrow-bottom-left'"
						custom-size="mdi-14px"
						:class="item.direction === 'to' ? 'is-to' : 'is-from'"
					></b-icon>
					<span class="rclip-dir">{{ directionLabel(item) }}</span>
					<time class="rclip-time" :datetime="new Date(item.at).toISOString()" :title="new Date(item.at).toLocaleString()">{{ ago(item.at) }}</time>
					<div class="rclip-item-actions">
						<button type="button" class="rclip-mini" :disabled="!connected" :aria-label="$t('Send to {target} clipboard', { target: targetLabel })" :title="$t('Send to {target} clipboard', { target: targetLabel })" @click="send(item.text)">
							<b-icon icon="send-outline" custom-size="mdi-14px"></b-icon>
						</button>
						<button type="button" class="rclip-mini" :disabled="!connected" :aria-label="$t('Type it')" :title="$t('Type it')" @click="type(item.text)">
							<b-icon icon="keyboard-outline" custom-size="mdi-14px"></b-icon>
						</button>
						<button type="button" class="rclip-mini" :aria-label="$t('Copy to this device')" :title="$t('Copy to this device')" @click="copyLocal(item)">
							<b-icon :icon="copiedId === item.id ? 'check' : 'content-copy'" custom-size="mdi-14px"></b-icon>
						</button>
						<button type="button" class="rclip-mini" :aria-label="$t('Remove')" :title="$t('Remove')" @click="removeItem(item.id)">
							<b-icon icon="delete-outline" custom-size="mdi-14px"></b-icon>
						</button>
					</div>
				</div>
				<button type="button" class="rclip-text" :title="$t('Edit before sending')" @click="useAsDraft(item)">
					{{ preview(item.text) }}
				</button>
			</li>
		</ul>
		<p v-else class="rclip-empty">
			{{ history.length ? $t('Nothing here for this filter.') : $t('Nothing yet. Text you send, and text copied on {target}, shows up here.', { target: targetLabel }) }}
		</p>

		<div class="rclip-foot">
			<span class="rclip-foot-note">{{ $t('Kept only until this browser tab is closed.') }}</span>
			<button v-if="history.length" type="button" class="rclip-link" @click="clearAll">{{ $t('Clear history') }}</button>
		</div>

		<span class="sr-only" aria-live="polite">{{ announcement }}</span>
	</div>
</template>

<script>
import { record, remove, clear, items, MAX_TYPED_CHARS } from '@/service/remoteClipboard'

let uid = 0

export default {
	name: 'remote-clipboard-panel',
	props: {
		// Who the text goes to: { kind: 'vm'|'host', name }
		target: { type: Object, required: true },
		targetLabel: { type: String, required: true },
		connected: { type: Boolean, default: false },
		// Shown under the paste button: when copy/paste with the remote
		// may not work, why and what to do instead.
		syncHint: { type: String, default: '' },
	},
	data() {
		uid++
		return {
			draft: '',
			filter: 'all',
			readBlocked: false,
			copiedId: null,
			announcement: '',
			now: Date.now(),
			inputId: `rclip-input-${uid}`,
			historyId: `rclip-history-${uid}`,
		}
	},
	computed: {
		history() {
			return items()
		},
		filters() {
			return [
				{ key: 'all', label: this.$t('All') },
				{ key: 'here', label: this.targetLabel },
				{ key: 'to', label: this.$t('Sent') },
				{ key: 'from', label: this.$t('Copied') },
			]
		},
		visible() {
			const t = this.target
			switch (this.filter) {
				case 'here':
					return this.history.filter((i) => i.target.kind === t.kind && i.target.name === t.name)
				case 'to':
				case 'from':
					return this.history.filter((i) => i.direction === this.filter)
				default:
					return this.history
			}
		},
	},
	mounted() {
		// Relative times ("2 min ago") stay current while the panel is open.
		this.clock = setInterval(() => (this.now = Date.now()), 30000)
		document.addEventListener('pointerdown', this.onOutside, true)
		this.$nextTick(() => this.$refs.panel && this.$refs.panel.focus())
	},
	beforeDestroy() {
		clearInterval(this.clock)
		clearTimeout(this.copiedTimer)
		document.removeEventListener('pointerdown', this.onOutside, true)
	},
	methods: {
		onOutside(e) {
			const panel = this.$refs.panel
			if (!panel || panel.contains(e.target)) return
			// The toggle button closes it itself; don't reopen on the same click.
			if (e.target.closest && e.target.closest('[data-rclip-toggle]')) return
			this.$emit('close')
		},
		announce(msg) {
			this.announcement = ''
			this.$nextTick(() => (this.announcement = msg))
		},
		async pasteBrowserClipboard() {
			let text = ''
			try {
				if (navigator.clipboard && navigator.clipboard.readText) text = await navigator.clipboard.readText()
			} catch (e) {
				// Blocked (permission denied, or plain http:// - the
				// Clipboard API needs a secure context).
			}
			if (text) {
				this.readBlocked = false
				this.send(text)
				return
			}
			this.readBlocked = true
			this.$nextTick(() => this.$refs.input && this.$refs.input.focus())
		},
		send(text) {
			if (!this.connected || !text) return
			this.$emit('send', text)
			record({ text, direction: 'to', target: this.target })
			this.announce(this.$t('Sent to {target} clipboard', { target: this.targetLabel }))
		},
		type(text) {
			if (!this.connected || !text) return
			this.$emit('type', text)
			record({ text, direction: 'to', target: this.target })
			this.announce(
				text.length > MAX_TYPED_CHARS
					? this.$t('Typed the first {n} characters', { n: MAX_TYPED_CHARS })
					: this.$t('Typed into {target}', { target: this.targetLabel })
			)
		},
		sendDraft() {
			if (!this.draft) return
			this.send(this.draft)
			this.draft = ''
			this.readBlocked = false
		},
		typeDraft() {
			if (!this.draft) return
			this.type(this.draft)
			this.draft = ''
			this.readBlocked = false
		},
		useAsDraft(item) {
			this.draft = item.text
			this.$nextTick(() => this.$refs.input && this.$refs.input.focus())
		},
		async copyLocal(item) {
			let ok = false
			try {
				if (navigator.clipboard && navigator.clipboard.writeText) {
					await navigator.clipboard.writeText(item.text)
					ok = true
				}
			} catch (e) {}
			if (!ok) {
				// No Clipboard API (plain http://): the old execCommand path
				// still works from a click.
				const ta = document.createElement('textarea')
				ta.value = item.text
				ta.setAttribute('readonly', '')
				ta.style.cssText = 'position:fixed;opacity:0;pointer-events:none'
				document.body.appendChild(ta)
				ta.select()
				try {
					ok = document.execCommand('copy')
				} catch (e) {}
				document.body.removeChild(ta)
			}
			if (ok) {
				this.copiedId = item.id
				clearTimeout(this.copiedTimer)
				this.copiedTimer = setTimeout(() => (this.copiedId = null), 1500)
				this.announce(this.$t('Copied to this device'))
			} else {
				this.useAsDraft(item)
				this.announce(this.$t("Couldn't copy automatically - the text is in the box, press Ctrl+C."))
			}
		},
		removeItem(id) {
			remove(id)
		},
		clearAll() {
			clear()
			this.announce(this.$t('History cleared'))
		},
		directionLabel(item) {
			const name = item.target.kind === 'host' ? this.$t('Host') : item.target.name
			return item.direction === 'to' ? this.$t('To {name}', { name }) : this.$t('From {name}', { name })
		},
		preview(text) {
			const flat = text.replace(/\s+/g, ' ').trim()
			return flat.length > 160 ? flat.slice(0, 160) + '…' : flat || this.$t('(whitespace)')
		},
		// Compact and localized ("5m", "3h", "2d"); the full date is the
		// tooltip, so the machine name keeps the room in the row.
		ago(at) {
			const s = Math.max(0, Math.round((this.now - at) / 1000))
			if (s < 45) return this.$t('now')
			const [value, unit] = s < 3600 ? [Math.round(s / 60), 'minute'] : s < 86400 ? [Math.round(s / 3600), 'hour'] : [Math.round(s / 86400), 'day']
			try {
				return new Intl.NumberFormat(this.$i18n.locale.replace('_', '-'), { style: 'unit', unit, unitDisplay: 'narrow' }).format(value)
			} catch (e) {
				return value + unit[0]
			}
		},
	},
}
</script>

<style lang="scss" scoped>
// Matches the console chrome, which is dark in both themes.
.rclip {
	position: absolute;
	top: calc(100% + 0.45rem);
	right: 0;
	z-index: 1000;
	width: 23rem;
	max-width: calc(100vw - 1.5rem);
	max-height: min(34rem, 75vh);
	overflow-y: auto;
	overscroll-behavior: contain;
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: var(--space-3);
	background: #1e1e24;
	color: #e4e4e7;
	border: 1px solid rgba(255, 255, 255, 0.14);
	border-radius: var(--radius-card);
	box-shadow: var(--shadow-lg);
	color-scheme: dark;
	scrollbar-color: rgba(255, 255, 255, 0.25) transparent;
	text-align: left;
	font-size: var(--font-sm);
	cursor: default;

	&:focus {
		outline: none;
	}
}

.rclip-head,
.rclip-history-head,
.rclip-foot,
.rclip-actions,
.rclip-item-meta {
	display: flex;
	align-items: center;
	gap: var(--space-2);
}

.rclip-head,
.rclip-history-head,
.rclip-foot {
	justify-content: space-between;
}

.rclip-title {
	font-weight: 600;
	font-size: var(--font-base);
	color: #fafafa;
}

.rclip-subtitle {
	font-weight: 600;
	color: #fafafa;
}

.rclip-label {
	color: #a1a1aa;
	font-size: var(--font-xs);
	margin-top: var(--space-1);
}

.rclip-input {
	width: 100%;
	resize: vertical;
	min-height: 4rem;
	padding: var(--space-2);
	border-radius: var(--radius-control);
	border: 1px solid rgba(255, 255, 255, 0.18);
	background: #111114;
	color: #fafafa;
	font: inherit;

	&::placeholder {
		color: #8b8b94;
	}

	&:focus {
		outline: 2px solid #60a5fa;
		outline-offset: 0;
		border-color: transparent;
	}
}

.rclip-actions {
	justify-content: flex-end;
}

.rclip-btn {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	gap: var(--space-1);
	padding: var(--space-1) var(--space-3);
	min-height: 2rem;
	border-radius: var(--radius-control);
	border: 1px solid rgba(255, 255, 255, 0.18);
	background: rgba(255, 255, 255, 0.06);
	color: #fafafa;
	font: inherit;
	font-weight: 500;
	cursor: pointer;

	&:hover:not(:disabled) {
		background: rgba(255, 255, 255, 0.12);
	}

	&.is-primary {
		background: #2563eb;
		border-color: #2563eb;
		color: #ffffff;

		&:hover:not(:disabled) {
			background: #1d4ed8;
		}
	}

	&:disabled {
		opacity: 0.45;
		cursor: not-allowed;
	}
}

.rclip-paste {
	width: 100%;
	min-height: 2.25rem;
}

.rclip-note {
	display: flex;
	gap: var(--space-1);
	align-items: flex-start;
	color: #a1a1aa;
	font-size: var(--font-xs);
	line-height: 1.4;

	&.is-warning {
		color: #fbbf24;
	}
}

.rclip-history-head {
	margin-top: var(--space-2);
	padding-top: var(--space-2);
	border-top: 1px solid rgba(255, 255, 255, 0.1);
	flex-wrap: wrap;
}

.rclip-filter {
	display: flex;
	gap: 0.25rem;
	flex-wrap: wrap;
}

.rclip-chip {
	padding: 0.125rem var(--space-2);
	border-radius: var(--radius-pill);
	border: 1px solid rgba(255, 255, 255, 0.16);
	background: transparent;
	color: #c4c4cc;
	font: inherit;
	font-size: var(--font-xs);
	cursor: pointer;
	max-width: 8rem;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;

	&.active {
		background: rgba(96, 165, 250, 0.18);
		border-color: rgba(96, 165, 250, 0.5);
		color: #bfdbfe;
	}
}

.rclip-list {
	list-style: none;
	margin: 0;
	padding: 0;
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.rclip-item {
	padding: var(--space-2);
	border-radius: var(--radius-control);
	background: rgba(255, 255, 255, 0.04);
	border: 1px solid rgba(255, 255, 255, 0.08);
}

.rclip-item-meta {
	font-size: var(--font-xs);
	color: #a1a1aa;

	.is-to {
		color: #60a5fa;
	}

	.is-from {
		color: #34d399;
	}
}

.rclip-dir {
	flex: 1;
	color: #d4d4d8;
	font-weight: 500;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.rclip-time {
	margin-left: auto;
	white-space: nowrap;
}

.rclip-item-meta .rclip-item-actions {
	margin-left: var(--space-1);
}

.rclip-text {
	all: unset;
	display: -webkit-box;
	-webkit-line-clamp: 2;
	-webkit-box-orient: vertical;
	overflow: hidden;
	margin-top: 0.25rem;
	color: #f4f4f5;
	word-break: break-word;
	cursor: pointer;
	font-size: var(--font-sm);
	line-height: 1.4;
}

.rclip-item-actions {
	display: flex;
	gap: 0.25rem;
	justify-content: flex-end;
}

.rclip-mini,
.rclip-icon-btn {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 1.75rem;
	height: 1.75rem;
	border-radius: var(--radius-control);
	border: none;
	background: transparent;
	color: #c4c4cc;
	cursor: pointer;

	&:hover:not(:disabled) {
		background: rgba(255, 255, 255, 0.1);
		color: #ffffff;
	}

	&:disabled {
		opacity: 0.4;
		cursor: not-allowed;
	}
}

.rclip-empty {
	color: #a1a1aa;
	font-size: var(--font-xs);
	padding: var(--space-2) 0;
	line-height: 1.4;
}

.rclip-foot {
	padding-top: var(--space-2);
	border-top: 1px solid rgba(255, 255, 255, 0.1);
	font-size: var(--font-xs);
}

.rclip-foot-note {
	color: #8b8b94;
}

.rclip-link {
	all: unset;
	color: #93c5fd;
	cursor: pointer;
	white-space: nowrap;

	&:hover {
		text-decoration: underline;
	}
}

.rclip button:focus-visible,
.rclip-text:focus-visible,
.rclip-link:focus-visible {
	outline: 2px solid #60a5fa;
	outline-offset: 1px;
}

.sr-only {
	position: absolute;
	width: 1px;
	height: 1px;
	margin: -1px;
	overflow: hidden;
	clip: rect(0, 0, 0, 0);
	white-space: nowrap;
}

@media (max-width: 480px) {
	.rclip {
		position: fixed;
		top: auto;
		bottom: var(--space-3);
		left: var(--space-3);
		right: var(--space-3);
		width: auto;
		max-width: none;
		max-height: 70vh;
	}
}
</style>
