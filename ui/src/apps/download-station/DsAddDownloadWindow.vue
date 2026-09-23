<!-- src/apps/download-station/DsAddDownloadWindow.vue -->
<!-- "Add Download" - a real desktop window (OPEN_WINDOW), so it never
     blocks Download Station or anything else on the desktop. Opened blank
     (Add URL), with pasted/dropped links, or pre-filled from a link the
     lite browser captured (the sidecar keeps that link's cookies/referer
     and applies them itself; they never pass through here). -->
<template>
	<div class="ds-add-window" ref="root">
		<div class="add-body scrollbars-light">
			<label class="ds-field-label">{{ isBatch ? $t('Links (one per line)') : $t('Link') }}</label>
			<textarea v-if="isBatch || !captureId" ref="urlInput" v-model="urlText" class="ds-input url-area" :rows="isBatch ? 5 : 2"
				spellcheck="false" :placeholder="'https://example.com/file.zip'" @input="onUrlInput"></textarea>
			<div v-else class="captured-link">
				<b-icon icon="web" custom-size="mdi-16px"></b-icon>
				<span class="one-line" :title="urlText">{{ urlText }}</span>
				<span v-if="capture && capture.has_cookies" class="cookie-pill" :title="$t('Sent with the browser page\'s cookies')">
					<b-icon icon="lock-outline" custom-size="mdi-12px"></b-icon>{{ $t('Cookies') }}
				</span>
			</div>

			<div v-if="!isBatch" class="probe-card" :class="{ 'is-error': probeError }">
				<div class="probe-icon" :class="'cat-' + category">
					<b-icon v-if="probing" icon="loading" custom-class="mdi-spin" custom-size="mdi-22px"></b-icon>
					<b-icon v-else :icon="icon" custom-size="mdi-22px"></b-icon>
				</div>
				<div class="probe-info">
					<template v-if="probeError">
						<div class="probe-title">{{ $t('Could not check the link') }}</div>
						<div class="ds-error-text">{{ probeError }}</div>
					</template>
					<template v-else-if="probe">
						<div class="probe-title">{{ formatBytes(probe.size) }}<span v-if="probe.content_type" class="probe-type">{{ shortType(probe.content_type) }}</span></div>
						<div class="probe-resume" :class="{ ok: probe.resumable }">
							<b-icon :icon="probe.resumable ? 'check-circle-outline' : 'alert-circle-outline'" custom-size="mdi-14px"></b-icon>
							{{ probe.resumable ? $t('Resumable - multi-connection download') : $t('Server has no resume support - single connection') }}
						</div>
					</template>
					<div v-else class="ds-hint">{{ probing ? $t('Checking link...') : $t('Enter a link to see its size and whether it can be resumed.') }}</div>
				</div>
			</div>

			<div v-if="!isBatch" class="field">
				<label class="ds-field-label">{{ $t('Save as') }}</label>
				<input v-model="filename" class="ds-input" spellcheck="false" @input="filenameTouched = true" />
			</div>

			<div class="field">
				<label class="ds-field-label">{{ $t('Save to') }}</label>
				<div class="folder-row">
					<input v-model="dir" class="ds-input" spellcheck="false" />
					<button class="ds-secondary-btn" @click="browseFolder">
						<b-icon icon="folder-outline" custom-size="mdi-18px"></b-icon><span>{{ $t('Browse') }}</span>
					</button>
				</div>
			</div>

			<div class="field">
				<label class="ds-field-label">
					{{ $t('Connections') }} <span class="conn-value">{{ connections }}</span>
				</label>
				<div class="slider-row">
					<span class="slider-hint">1</span>
					<input v-model.number="connections" class="pretty-range" type="range" min="1" max="32" step="1" :style="rangeStyle" />
					<span class="slider-hint">32</span>
				</div>
				<p class="ds-hint">{{ $t('More connections usually means faster downloads; some servers limit how many they allow.') }}</p>
			</div>

			<p v-if="error" class="ds-error-text">{{ error }}</p>
		</div>

		<div class="add-foot">
			<button class="ds-secondary-btn" @click="close">{{ $t('Cancel') }}</button>
			<div class="foot-right">
				<button class="ds-secondary-btn" :disabled="!canSubmit || submitting" @click="submit(false)">{{ $t('Download later') }}</button>
				<button class="ds-primary-btn" :disabled="!canSubmit || submitting" @click="submit(true)">
					<b-icon :icon="submitting ? 'loading' : 'download'" :custom-class="submitting ? 'mdi-spin' : ''" custom-size="mdi-18px"></b-icon>
					<span>{{ isBatch ? $t('Download') + ' ' + urls.length : $t('Start Download') }}</span>
				</button>
			</div>
		</div>
	</div>
</template>

<script>
import { downloadSidecar, formatBytes, categoryOf, fileIcon } from '@/api/downloadSidecar'

export default {
	name: 'DsAddDownloadWindow',
	props: {
		winId: { type: String, default: '' },
		url: { type: String, default: '' },
		captureSession: { type: String, default: '' },
		captureId: { type: String, default: '' }
	},
	data() {
		return {
			urlText: this.url || '',
			filename: '',
			filenameTouched: false,
			dir: '',
			connections: 8,
			probe: null,
			probing: false,
			probeError: '',
			capture: null,
			error: '',
			submitting: false,
			probeTimer: null
		}
	},
	computed: {
		urls() {
			return this.urlText
				.split(/\s+/)
				.map(s => s.trim())
				.filter(s => /^https?:\/\//i.test(s))
		},
		isBatch() {
			return this.urls.length > 1
		},
		canSubmit() {
			return this.urls.length > 0 && this.dir.trim().startsWith('/')
		},
		category() {
			return categoryOf(this.filename)
		},
		icon() {
			return fileIcon(this.filename)
		},
		rangeStyle() {
			return { '--pct': `${((this.connections - 1) / 31) * 100}%` }
		}
	},
	async created() {
		try {
			const s = await downloadSidecar.getSettings()
			this.dir = s.default_dir
			this.connections = s.default_connections
		} catch (e) {
			this.dir = '/DATA/Downloads'
		}
		if (this.captureSession && this.captureId) {
			try {
				this.capture = await downloadSidecar.getCapture(this.captureSession, this.captureId)
				this.urlText = this.capture.url
				this.filename = this.capture.filename
			} catch (e) {
				this.error = this.$t('The captured link has expired - copy the link from the page instead.')
			}
		}
		if (this.urls.length === 1) this.runProbe()
	},
	mounted() {
		this.$nextTick(() => {
			if (!this.urlText && this.$refs.urlInput) this.$refs.urlInput.focus()
		})
	},
	beforeDestroy() {
		clearTimeout(this.probeTimer)
	},
	methods: {
		formatBytes,
		shortType(ct) {
			return (ct || '').split(';')[0]
		},
		onUrlInput() {
			clearTimeout(this.probeTimer)
			this.probe = null
			this.probeError = ''
			if (this.urls.length === 1) {
				if (!this.filenameTouched) this.filename = guessName(this.urls[0])
				this.probeTimer = setTimeout(this.runProbe, 500)
			}
		},
		async runProbe() {
			const u = this.urls[0]
			if (!u) return
			this.probing = true
			this.probeError = ''
			try {
				const res = await downloadSidecar.probe({ url: u, capture_session: this.captureSession, capture_id: this.captureId })
				if (this.urls[0] !== u) return
				this.probe = res
				if (!this.filenameTouched && res.filename) this.filename = res.filename
			} catch (e) {
				if (this.urls[0] === u) this.probeError = e.message
			} finally {
				this.probing = false
			}
		},
		browseFolder() {
			const id = 'ds-folder-' + Date.now()
			this.$store.commit('OPEN_WINDOW', {
				id,
				title: this.$t('Choose Folder'),
				component: 'DsFolderPickerWindow',
				props: { winId: id, startPath: this.dir || '/DATA', onSelect: path => (this.dir = path) },
				width: 460,
				height: 440
			})
		},
		async submit(start) {
			this.error = ''
			this.submitting = true
			try {
				if (this.isBatch) {
					for (const u of this.urls) {
						await downloadSidecar.addDownload({ url: u, dir: this.dir, connections: this.connections, start })
					}
				} else {
					await downloadSidecar.addDownload({
						url: this.urls[0],
						filename: this.filenameTouched ? this.filename : '',
						dir: this.dir,
						connections: this.connections,
						start,
						capture_session: this.captureSession,
						capture_id: this.captureId
					})
				}
				this.close()
			} catch (e) {
				this.error = e.message
			} finally {
				this.submitting = false
			}
		},
		close() {
			const id = this.winId || (this.$parent && this.$parent.win && this.$parent.win.id)
			if (id) this.$store.commit('CLOSE_WINDOW', id)
			this.$emit('close')
		}
	}
}

function guessName(u) {
	try {
		const p = new URL(u).pathname
		const last = decodeURIComponent(p.split('/').filter(Boolean).pop() || '')
		return last
	} catch (e) {
		return ''
	}
}
</script>

<style lang="scss" scoped>
@import './ds-common.scss';

.ds-add-window {
	display: flex;
	flex-direction: column;
	height: 100%;
	background: var(--theme-bg-window, #fff);
	color: var(--theme-text-primary, #1e293b);
}

.add-body {
	flex: 1 1 auto;
	min-height: 0;
	overflow-y: auto;
	padding: var(--space-4) var(--space-5);
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
}

.url-area {
	min-height: 3.2rem;
}

.captured-link {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-control);
	background: var(--theme-card-subtle, #f1f5f9);
	font-size: var(--font-xs);
	font-family: $family-monospace;
	color: var(--theme-text-secondary, #475569);
	min-width: 0;

	> .one-line {
		flex: 1 1 auto;
		min-width: 0;
	}
}

.cookie-pill {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	gap: 0.2rem;
	font-family: $family-sans-serif;
	font-size: var(--font-2xs);
	padding: 0.1rem 0.45rem;
	border-radius: var(--radius-pill);
	background: rgba(16, 185, 129, 0.12);
	color: #059669;
}

.probe-card {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	padding: var(--space-3);
	border-radius: var(--radius-card);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	background: var(--theme-card-bg, #fff);

	&.is-error {
		border-color: rgba(220, 38, 38, 0.25);
	}
}

.probe-icon {
	flex-shrink: 0;
	width: 2.75rem;
	height: 2.75rem;
	border-radius: var(--radius-sm);
	display: flex;
	align-items: center;
	justify-content: center;
	background: rgba(37, 99, 235, 0.1);
	color: #2563eb;
}

.probe-info {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: 0.2rem;
}

.probe-title {
	font-size: var(--font-base);
	font-weight: 600;
	font-variant-numeric: tabular-nums;
}

.probe-type {
	margin-left: var(--space-2);
	font-size: var(--font-2xs);
	font-weight: 400;
	color: var(--theme-text-muted, #94a3b8);
}

.probe-resume {
	display: flex;
	align-items: center;
	gap: 0.3rem;
	font-size: var(--font-xs);
	color: #d97706;

	&.ok {
		color: #059669;
	}
}

.folder-row {
	display: flex;
	gap: var(--space-2);

	.ds-secondary-btn {
		height: 2.1rem;
	}
}

.conn-value {
	display: inline-block;
	margin-left: var(--space-1);
	padding: 0 0.45rem;
	border-radius: var(--radius-pill);
	background: rgba(37, 99, 235, 0.1);
	color: #2563eb;
	font-weight: 600;
	font-variant-numeric: tabular-nums;
}

.slider-row {
	display: flex;
	align-items: center;
	gap: var(--space-2);

	.pretty-range {
		flex: 1 1 auto;
	}
}

.slider-hint {
	font-size: var(--font-2xs);
	color: var(--theme-text-muted, #94a3b8);
}

.add-foot {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-5);
	border-top: 1px solid var(--theme-card-border, rgb(228 233 237));
	background: var(--theme-bg-window, #fff);
}

.foot-right {
	display: flex;
	gap: var(--space-2);
}
</style>
