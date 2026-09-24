<!-- A run's log (spec §12.5, §10.5): role=log with aria-live off (the
     run window announces steps itself). Lines are i18n keys rendered in
     the viewer's language; "Show technical output" adds the engine's raw
     lines. It follows the end unless the user scrolled up, then offers
     "Jump to latest". The log isn't on the message bus: while the run is
     active and the log is open, it's read by byte offset every 2 s. -->
<template>
	<div class="bk-runlog">
		<div class="bk-runlog-bar">
			<label class="bk-runlog-raw">
				<input v-model="showRaw" type="checkbox" />
				<span>{{ $t('backup.log.show_raw') }}</span>
			</label>
			<span class="bk-runlog-spacer"></span>
			<button type="button" class="bk-btn is-small" :disabled="!lines.length" @click="copy">
				<b-icon icon="content-copy" pack="mdi" custom-size="mdi-16px" aria-hidden="true"></b-icon>
				<span>{{ $t('backup.log.copy') }}</span>
			</button>
			<button type="button" class="bk-btn is-small" :disabled="downloading || !lines.length" @click="download">
				<b-icon icon="download" pack="mdi" custom-size="mdi-16px" aria-hidden="true"></b-icon>
				<span>{{ $t('backup.log.download') }}</span>
			</button>
		</div>
		<p v-if="error" class="bk-runlog-error" role="alert">{{ errText(error) }}</p>
		<p v-if="trimmed" class="bk-muted bk-runlog-note">{{ $t('backup.log.trimmed', { count: fmt.number(maxLines) }) }}</p>
		<div ref="box" class="bk-code bk-runlog-box" role="log" aria-live="off" :aria-label="$t('backup.log.label')" tabindex="0" @scroll="onScroll">
			<p v-if="!shown.length" class="bk-muted">{{ loading ? $t('backup.loading') : $t('backup.log.empty') }}</p>
			<div v-for="(l, i) in shown" :key="i" class="bk-runlog-line" :class="'is-' + (l.lvl || 'info')">
				<time :datetime="l.t">{{ fmt.time(l.t) }}</time>
				<span class="bk-runlog-lvl">{{ lvlLabel(l.lvl) }}</span>
				<span class="bk-runlog-text">{{ text(l) }}</span>
			</div>
		</div>
		<button v-if="!following" type="button" class="bk-chip bk-runlog-jump" @click="jumpToLatest">
			<b-icon icon="arrow-down" pack="mdi" custom-size="mdi-14px" aria-hidden="true"></b-icon>
			<span>{{ $t('backup.log.jump') }}</span>
		</button>
	</div>
</template>

<script>
import { backupMixin } from '../backupMixin'

const PAGE = 500
const POLL_MS = 2000
// Keep the page responsive on a huge log; Download still gets everything.
const MAX_LINES = 5000

export default {
	name: 'RunLog',
	mixins: [backupMixin],
	props: {
		runId: { type: String, required: true },
		// true while the run can still write to its log
		active: { type: Boolean, default: false },
		visible: { type: Boolean, default: true }
	},
	data() {
		return { lines: [], offset: 0, done: false, loading: false, error: null, showRaw: false, following: true, trimmed: false, downloading: false, maxLines: MAX_LINES }
	},
	computed: {
		shown() {
			return this.showRaw ? this.lines : this.lines.filter(l => l.msg_key)
		}
	},
	watch: {
		shown() {
			if (this.following) this.$nextTick(this.scrollToEnd)
		},
		active(a) {
			// The run just ended: read what it wrote last.
			if (!a) this.fetch()
		}
	},
	created() {
		this.fetch()
		this.timer = setInterval(() => {
			if (this.active && this.visible && !this.loading) this.fetch()
		}, POLL_MS)
	},
	beforeDestroy() {
		clearInterval(this.timer)
		this.stopped = true
	},
	methods: {
		async fetch() {
			if (this.loading || this.stopped) return
			this.loading = true
			try {
				// Read to the end of what's there (a finished log in one go).
				for (let guard = 0; guard < 50; guard++) {
					const page = await this.bkApi.getRunLog(this.runId, { after: this.offset, limit: PAGE })
					if (this.stopped) return
					const got = page.lines || []
					if (got.length) this.append(got)
					const advanced = page.next_offset > this.offset
					this.offset = page.next_offset || this.offset
					this.done = !!page.done
					this.error = null
					if (this.done || !got.length || !advanced) break
				}
			} catch (e) {
				this.error = e
			} finally {
				this.loading = false
			}
		},
		append(got) {
			const all = this.lines.concat(got)
			if (all.length > MAX_LINES) {
				this.trimmed = true
				this.lines = all.slice(all.length - MAX_LINES)
			} else this.lines = all
		},
		text(l, raw = this.showRaw) {
			const rendered = l.msg_key ? this.msg({ key: l.msg_key, args: l.args }) : ''
			if (!raw || !l.raw) return rendered || l.raw || ''
			return rendered ? `${rendered} — ${l.raw}` : l.raw
		},
		lvlLabel(lvl) {
			return this.$t('backup.log.lvl_' + (['warn', 'error'].includes(lvl) ? lvl : 'info'))
		},
		onScroll() {
			const box = this.$refs.box
			if (!box) return
			this.following = box.scrollHeight - box.scrollTop - box.clientHeight < 24
		},
		scrollToEnd() {
			const box = this.$refs.box
			if (box) box.scrollTop = box.scrollHeight
		},
		jumpToLatest() {
			this.following = true
			this.$nextTick(this.scrollToEnd)
		},
		plainText(lines, raw = this.showRaw) {
			return lines.map(l => `${l.t} ${(l.lvl || 'info').toUpperCase()} ${this.text(l, raw)}`).join('\n')
		},
		async copy() {
			try {
				await navigator.clipboard.writeText(this.plainText(this.shown))
				this.toast(this.$t('backup.log.copied_toast'))
			} catch (e) {
				this.toast(this.$t('backup.log.copy_failed'), 'is-danger')
			}
		},
		// The whole log, technical lines included, as a text file.
		async download() {
			this.downloading = true
			try {
				const all = []
				let after = 0
				for (let guard = 0; guard < 10000; guard++) {
					const page = await this.bkApi.getRunLog(this.runId, { after, limit: 2000 })
					all.push(...(page.lines || []))
					if (page.done || !(page.lines || []).length || !(page.next_offset > after)) break
					after = page.next_offset
				}
				const text = this.plainText(all, true)
				const url = URL.createObjectURL(new Blob([text + '\n'], { type: 'text/plain;charset=utf-8' }))
				const a = document.createElement('a')
				a.href = url
				a.download = `${this.runId}.log`
				document.body.appendChild(a)
				a.click()
				a.remove()
				setTimeout(() => URL.revokeObjectURL(url), 1000)
			} catch (e) {
				this.toastError(e)
			} finally {
				this.downloading = false
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-runlog {
	position: relative;
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	min-height: 0;
	p {
		margin: 0;
	}
}
.bk-runlog-bar {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--space-2);
}
.bk-runlog-spacer {
	flex: 1 1 0;
}
.bk-runlog-raw {
	display: inline-flex;
	align-items: center;
	gap: var(--space-2);
	font-size: var(--font-sm);
	color: var(--theme-text-secondary);
	cursor: pointer;
	input {
		width: 1rem;
		height: 1rem;
	}
}
.bk-runlog-box {
	height: 12rem;
	overflow: auto;
	padding: var(--space-2) var(--space-3);
	line-height: 1.5;
}
.bk-runlog-line {
	display: flex;
	gap: var(--space-2);
	white-space: pre-wrap;
	overflow-wrap: anywhere;
	time {
		flex-shrink: 0;
		color: var(--theme-text-secondary);
	}
	&.is-warn .bk-runlog-lvl {
		color: var(--status-warn-fg);
	}
	&.is-error .bk-runlog-lvl {
		color: var(--status-danger-fg);
	}
}
.bk-runlog-lvl {
	flex-shrink: 0;
	min-width: 3.5rem;
	color: var(--theme-text-secondary);
}
.bk-runlog-jump {
	position: absolute;
	right: var(--space-3);
	bottom: var(--space-3);
	box-shadow: var(--shadow-sm);
}
.bk-runlog-error {
	font-size: var(--font-sm);
	color: var(--color-danger-fg);
}
.bk-runlog-note {
	font-size: var(--font-xs);
}
</style>
