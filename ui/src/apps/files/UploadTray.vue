<!-- src/apps/files/UploadTray.vue -->
<template>
	<div class="upload-tray-root">
		<!-- Backs the toolbar's "Upload" button (browse()) - equivalent to the
			legacy uploaderInstance.assignBrowse() wiring, without needing a
			second live DOM node registered with the uploader instance. Kept
			OUTSIDE the v-show="visible" tray below: an ancestor with
			display:none can stop some browsers from honoring a programmatic
			.click() on a file input, even though display:none on the input
			itself is fine. -->
		<input ref="fileInput" type="file" multiple style="display: none" @change="onFileInputChange" />
		<input ref="folderInput" type="file" webkitdirectory directory multiple style="display: none" @change="onFileInputChange" />
		<transition name="tray-pop">
			<div v-show="visible" class="upload-tray" role="status" aria-live="polite" :aria-label="$t('Upload progress')">
				<div class="upload-tray-header">
					<b-icon icon="tray-arrow-up" custom-size="mdi-18px" class="header-icon"></b-icon>
					<span class="header-title">{{ headerText }}</span>
					<span v-if="status === 'uploading' && totalSpeed > 0" class="total-speed">{{ formatSize(totalSpeed) }}/s</span>
					<button type="button" class="icon-btn close-icon" :aria-label="$t('Close')" @click="dismiss">
						<b-icon icon="close" custom-size="mdi-16px"></b-icon>
					</button>
				</div>
				<ul class="upload-tray-list">
					<li v-for="file in trackedFiles" :key="file.uid" class="upload-tray-item" :class="'is-' + file.status">
						<div class="item-icon-badge">
							<b-icon
								class="item-icon"
								custom-size="mdi-18px"
								:icon="file.status === 'error' ? 'alert-circle' : file.status === 'success' ? 'check-circle' : 'file-outline'"
							></b-icon>
						</div>
						<div class="item-body">
							<div class="item-row">
								<span class="upload-file-name" :title="file.name">{{ file.name }}</span>
								<span v-if="file.status === 'error'" class="status-text is-error" :title="file.message">{{ $t('Error') }}</span>
								<span v-else-if="file.status === 'success'" class="status-text is-success">{{ $t('Done') }}</span>
								<template v-else-if="file.progress === 0">
									<span class="status-text is-waiting">{{ $t('Waiting') }}</span>
									<button type="button" class="icon-btn cancel-icon" :aria-label="$t('Cancel')" @click="cancelFile(file)">
										<b-icon icon="close" custom-size="mdi-14px"></b-icon>
									</button>
								</template>
								<template v-else>
									<span class="percentage">{{ file.progress }}%</span>
									<button type="button" class="icon-btn cancel-icon" :aria-label="$t('Cancel')" @click="cancelFile(file)">
										<b-icon icon="close" custom-size="mdi-14px"></b-icon>
									</button>
								</template>
							</div>
							<div class="item-subrow">
								<span class="file-size">{{ formatSize(file.size) }}</span>
								<span v-if="file.status === 'uploading' && file.speed > 0" class="file-speed">{{ formatSize(file.speed) }}/s</span>
							</div>
							<div v-if="file.status === 'uploading'" class="progress-track">
								<div class="progress-fill" :class="{ 'is-indeterminate': file.progress === 0 }" :style="file.progress > 0 ? { width: file.progress + '%' } : {}"></div>
							</div>
						</div>
					</li>
				</ul>
			</div>
		</transition>
	</div>
</template>

<script>
import { formatSize } from '@/utils/formatSize'
import { createUploader } from './chunkUploader'
import events from '@/events/events'

// Files' upload queue: tracks each queued file for the tray. The uploader's
// own entries (File, chunk state, XHRs) stay off Vue's reactive data; the
// tray keeps a small reactive copy per file, updated from its callbacks.
export default {
	emits: ['uploaded'],
	name: 'upload-tray',
	props: {
		currentPath: {
			type: String,
			required: true,
		},
	},
	data() {
		return {
			trackedFiles: [],
			visible: false,
			status: 'uploading',
			totalSpeed: 0,
		}
	},
	computed: {
		headerText() {
			return this.status === 'completed' ? this.$t('Completed') : this.$t('Uploading')
		},
	},
	created() {
		this.rawFileMap = {}
		const tracked = (e) => this.trackedFiles.find((f) => f.uid === e.uniqueIdentifier)
		this.uploader = createUploader({
			target: `${this.$protocol}//${this.$baseURL}/v2/casaos/file/upload`,
			// Resume: before sending a chunk, ask whether the server already
			// has it (a network drop or retry doesn't restart the whole file).
			testChunks: true,
			// 8 MB chunks: few requests for big files, cheap to retry.
			chunkSize: 8 * 1024 * 1024,
			simultaneous: 3,
			// A transient failure (Wi-Fi blip, server busy) retries the chunk
			// instead of failing the file; 400s (bad path) fail right away.
			retries: 4,
			retryInterval: 1500,
			permanentErrors: [400, 404, 409, 415, 501],
			headers: () => ({ Authorization: this.$store.state.access_token || localStorage.getItem('access_token') }),
			fields: (e) => ({ path: e.targetPath }),
			onProgress: (e) => {
				const t = tracked(e)
				if (t) {
					t.progress = Math.floor(e.progress * 100)
					t.speed = e.speed
				}
				this.totalSpeed = this.uploader.files.reduce((sum, f) => sum + (f.status === 'uploading' ? f.speed : 0), 0)
			},
			onSuccess: (e) => {
				const t = tracked(e)
				if (t) {
					t.status = 'success'
					t.progress = 100
				}
				this.$emit('uploaded')
				this.$EventBus.$emit(events.RELOAD_FILE_LIST)
			},
			onError: (e, message) => {
				let parsed = message
				try {
					parsed = JSON.parse(message).message
				} catch (err) {
					// not JSON - keep the raw text
				}
				const t = tracked(e)
				if (t) {
					t.status = 'error'
					t.message = parsed
				}
			},
			onComplete: () => {
				// The tray stays up ("Completed", each file's Done/Error) until closed.
				this.status = 'completed'
				this.totalSpeed = 0
				this.$emit('uploaded')
				this.$EventBus.$emit(events.RELOAD_FILE_LIST)
			},
		})
	},
	// No beforeUnmount: closing the Files window lets queued uploads finish,
	// as before.
	methods: {
		// Dropped (ContentView walks folders first) or picked files.
		addFiles(files) {
			const list = Array.from(files)
			if (!list.length) return
			// Shown the instant a file is queued ("Waiting" + indeterminate bar
			// until its first bytes go out).
			this.visible = true
			this.status = 'uploading'
			// Uploader callbacks are XHR events (async), so tracking right after add() is in time.
			const added = this.uploader.add(list, { targetPath: this.currentPath })
			for (const e of added) {
				this.rawFileMap[e.uniqueIdentifier] = e
				this.trackedFiles.push({ uid: e.uniqueIdentifier, name: e.name, size: e.size, progress: 0, status: 'uploading', message: '', speed: 0 })
			}
		},
		browse() {
			this.$refs.fileInput.click()
		},
		browseFolder() {
			this.$refs.folderInput.click()
		},
		onFileInputChange(event) {
			if (event.target.files && event.target.files.length) {
				this.addFiles(event.target.files)
			}
			event.target.value = ''
		},
		dismiss() {
			this.visible = false
			this.trackedFiles = []
			this.rawFileMap = {}
		},
		cancelFile(trackedFile) {
			const raw = this.rawFileMap[trackedFile.uid]
			if (raw) this.uploader.cancel(raw)
			delete this.rawFileMap[trackedFile.uid]
			const index = this.trackedFiles.findIndex((f) => f.uid === trackedFile.uid)
			if (index > -1) this.trackedFiles.splice(index, 1)
			// Cancelling everything leaves no empty tray behind.
			if (!this.trackedFiles.length) this.visible = false
		},
		formatSize,
	},
}
</script>

<style lang="scss" scoped>
.upload-tray {
	position: absolute;
	bottom: 0.75rem;
	right: 0.75rem;
	// A fixed-ish width (like a real notification/download panel) instead
	// of stretching edge-to-edge - full-width read as way too dominant in
	// a small/narrow window. min() keeps a margin on the left even if the
	// window itself is narrower than the cap.
	width: min(22rem, calc(100% - 1.5rem));
	z-index: 20;
	max-height: 45%;
	display: flex;
	flex-direction: column;
	background: var(--theme-card-bg, #fff); border-radius: var(--radius-card); border: 1px solid var(--theme-card-border, rgb(228 233 237)); color: var(--theme-text-primary, #2c3e50);
	box-shadow: var(--shadow-lg);
	overflow: hidden;
}
.upload-tray-header {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-3);
	font-weight: 600;
	font-size: var(--font-base);
	border-bottom: 1px solid var(--theme-card-border, rgb(228 233 237));
	background: var(--theme-card-hover, rgba(0, 0, 0, 0.015));
}
.header-icon {
	color: var(--color-primary, #3273dc);
	flex-shrink: 0;
}
.header-title {
	flex: 1 1 auto;
}
.upload-tray-list {
	overflow-y: auto;
	margin: 0;
	// ~3 items' worth (each with padding, name+size/speed rows, progress
	// bar, and inter-item margin) + the list's own top/bottom padding -
	// a 4th+ file scrolls instead of growing the tray taller per file.
	max-height: 11.5rem;
	padding: var(--space-2);
	list-style: none;
}
.total-speed {
	flex-shrink: 0;
	font-weight: 400;
	font-size: var(--font-xs);
	color: var(--color-text-muted, #64748b);
}
// Reset for both icon-only <button>s below - a bare <button> carries the
// browser/OS's own default border and background, which must be reset
// explicitly or it shows through in both themes.
.icon-btn {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	border: none;
	background: transparent;
	padding: 2px;
	border-radius: var(--radius-xs);
	cursor: pointer;
	line-height: 1;

	&:focus-visible {
		outline: 2px solid var(--theme-focus-ring, rgba(37, 99, 235, 0.4));
		outline-offset: 1px;
	}
}
.close-icon {
	color: var(--color-text-muted, #64748b);
	&:hover { color: var(--theme-text-primary, rgba(0, 0, 0, 0.7)); }
}
.cancel-icon {
	color: var(--color-text-muted, #64748b);
	&:hover { color: var(--color-danger, #cc0f35); }
}
// No per-item border here (unlike an earlier revision) - a 1px stroke on
// every single row, nested inside the tray's own outer border, is what read
// as a stray "box stroke" in both themes. Matches OperationTray's list
// items, which distinguish state with background tint alone.
.upload-tray-item {
	display: flex;
	align-items: flex-start;
	gap: var(--space-3);
	padding: var(--space-2) var(--space-2);
	margin-bottom: var(--space-1);
	border-radius: var(--radius-card);
	font-size: var(--font-sm);
	background: transparent;

	&:last-child {
		margin-bottom: 0;
	}
	&:hover {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.03));
	}
	&.is-success {
		background: var(--theme-success-soft, rgba(72, 199, 116, 0.08));
	}
	&.is-error {
		background: rgba(255, 56, 96, 0.08);
	}
}
.item-icon-badge {
	flex-shrink: 0;
	width: 1.9rem;
	height: 1.9rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	background: var(--theme-info-soft, rgba(50, 115, 220, 0.1));

	.is-success & {
		background: var(--theme-success-soft, rgba(72, 199, 116, 0.15));
	}
	.is-error & {
		background: rgba(255, 56, 96, 0.15);
	}
}
.item-icon {
	color: var(--color-primary, #3273dc);

	.is-success & {
		color: #48c774;
	}
	.is-error & {
		color: #ff3860;
	}
}
.item-body {
	flex: 1 1 auto;
	min-width: 0;
	padding-top: 0.05rem;
}
.item-row {
	display: flex;
	align-items: baseline;
	gap: var(--space-2);
}
.item-subrow {
	display: flex;
	align-items: baseline;
	gap: var(--space-2);
	margin-top: 0.1rem;
}
.file-size {
	flex-shrink: 0;
	font-size: var(--font-2xs);
	color: var(--color-text-muted, #64748b);
}
.file-speed {
	flex-shrink: 0;
	font-size: var(--font-2xs);
	color: var(--color-text-muted, #64748b);
}
.upload-file-name {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	font-weight: 500;
}
.percentage {
	flex-shrink: 0;
	font-weight: 600;
	color: var(--color-primary, #3273dc);
}
.status-text {
	flex-shrink: 0;
	font-weight: 600;
	&.is-success { color: var(--color-success, #257942); }
	&.is-error { color: var(--color-danger, #cc0f35); }
	&.is-waiting { color: var(--color-text-muted, #64748b); font-weight: 400; }
}
.progress-track {
	margin-top: var(--space-2);
	height: 4px;
	border-radius: var(--radius-pill);
	background: var(--theme-info-soft, rgba(50, 115, 220, 0.12));
	overflow: hidden;
}
.progress-fill {
	height: 100%;
	border-radius: var(--radius-pill);
	background: var(--color-primary, #3273dc);
	transition: width 0.15s ease;

	// A file queued but not yet started has no real percentage to show -
	// a sliding indeterminate stripe reads as "about to start" instead of
	// a stuck, empty bar.
	&.is-indeterminate {
		width: 40%;
		animation: upload-indeterminate 1.2s ease-in-out infinite;
	}
}
@keyframes upload-indeterminate {
	0% { margin-left: -40%; }
	100% { margin-left: 100%; }
}
.tray-pop-enter-active,
.tray-pop-leave-active {
	transition: opacity 0.18s ease, transform 0.18s ease;
}
.tray-pop-enter-from,
.tray-pop-leave-to {
	opacity: 0;
	transform: translateY(8px);
}
</style>
