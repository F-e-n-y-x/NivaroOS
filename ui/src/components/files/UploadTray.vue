<!-- src/components/files/UploadTray.vue -->
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
		<div v-show="visible" class="upload-tray">
			<div class="upload-tray-header">
				<b-icon icon="tray-arrow-up" custom-size="mdi-18px" class="header-icon"></b-icon>
				<span class="header-title">{{ headerText }}</span>
				<span v-if="status === 'uploading' && totalSpeed > 0" class="total-speed">{{ formatSize(totalSpeed) }}/s</span>
				<b-icon icon="close" class="is-clickable dismiss-icon" custom-size="mdi-16px" @click.native="dismiss"></b-icon>
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
							<span class="file-name" :title="file.name">{{ file.name }}</span>
							<span v-if="file.status === 'error'" class="status-text is-error" :title="file.message">{{ $t('Error') }}</span>
							<span v-else-if="file.status === 'success'" class="status-text is-success">{{ $t('Done') }}</span>
							<template v-else-if="file.progress === 0">
								<span class="status-text is-waiting">{{ $t('Waiting') }}</span>
								<b-icon icon="close" custom-size="mdi-14px" class="is-clickable cancel-icon" :title="$t('Cancel')" @click.native="cancelFile(file)"></b-icon>
							</template>
							<template v-else>
								<span class="percentage">{{ file.progress }}%</span>
								<b-icon icon="close" custom-size="mdi-14px" class="is-clickable cancel-icon" :title="$t('Cancel')" @click.native="cancelFile(file)"></b-icon>
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
	</div>
</template>

<script>
import Uploader from 'simple-uploader.js'
import { formatSize } from '@/utils/formatSize'

// Ported from src/components/filebrowser/FilePanel.vue:728-791
// (getTargetUrl/setUploaderOpts) - see the task-15 report for the specific
// discrepancies between that legacy code and this component:
//   - FilePanel.vue itself only ever registers 'fileAdded'/'dragover'/
//     'uploadStart'/'complete' handlers on the raw uploader instance; the
//     per-file 'fileError' toast it relies on actually comes from the
//     generic uploader.vue wrapper's own internal `fileErrorHandle`
//     (registered in that wrapper's created() hook), not from FilePanel.vue.
//     Since this component talks to simple-uploader.js directly instead of
//     going through that wrapper, it registers its own 'fileError' (and
//     'fileProgress'/'fileSuccess') handlers to drive the per-file
//     progress/error UI this task's brief asks for.
//   - The wrapper's filesSubmitted() is what actually calls
//     `uploader.upload()` to kick the queue off (after an unrelated
//     `$api.sys.getVersion()` health probe) - simple-uploader.js's Uploader
//     does NOT auto-start uploads on addFiles()/addFile(). This component
//     calls `.upload()` directly on 'filesSubmitted', without the version
//     probe (not needed for a from-scratch wrapper).
//   - The wrapper's fileError handler calls `this.uploader.pause(file)`,
//     but `pause()` takes no argument and unconditionally pauses the WHOLE
//     uploader (all in-flight files), not just the one that errored - this
//     looks like a legacy bug (one failed file freezes the entire batch).
//     This component deliberately does not replicate that: an errored file
//     is just marked and left alone; the rest of the queue keeps going.
export default {
	name: 'upload-tray',
	props: {
		currentPath: {
			type: String,
			required: true,
		},
	},
	data() {
		return {
			uploaderInstance: null,
			trackedFiles: [],
			visible: false,
			status: 'uploading',
			// simple-uploader.js tracks speed on its own plain (non-reactive)
			// File objects, refreshed on its own interval - summed into this
			// reactive property whenever a 'fileProgress' event fires, rather
			// than read directly from those objects on every render.
			totalSpeed: 0,
		}
	},
	computed: {
		headerText() {
			return this.status === 'completed' ? this.$t('Completed') : this.$t('Uploading')
		},
	},
	created() {
		// Raw simple-uploader.js File handles, keyed by uid - kept off Vue's
		// reactive data() on purpose. These are complex objects with their
		// own internal chunk/state machinery the library mutates constantly;
		// Vue recursively wrapping every property in getters/setters would
		// be pure overhead for something only ever used to call
		// .removeFile() on, never rendered.
		this.rawFileMap = {}
		this.uploaderInstance = new Uploader({
			target: `${this.$protocol}//${this.$baseURL}/v2/casaos/file/upload`,
			testChunks: false,
			uploadMethod: 'POST',
			successStatuses: [200, 201, 202, 2002],
			permanentErrors: [404, 409, 415, 500, 501],
			allowDuplicateUploads: true,
			headers: {
				Authorization: this.$store.state.access_token || localStorage.getItem('access_token'),
			},
			query: (file) => {
				return { path: file.targetPath }
			},
		})

		this.uploaderInstance.on('fileAdded', (file) => {
			file.targetPath = this.currentPath
			this.rawFileMap[file.uniqueIdentifier] = file
			this.trackedFiles.push({
				uid: file.uniqueIdentifier,
				name: file.name,
				size: file.size,
				progress: 0,
				status: 'uploading',
				message: '',
				speed: 0,
			})
		})

		this.uploaderInstance.on('filesSubmitted', () => {
			this.uploaderInstance.upload()
		})

		this.uploaderInstance.on('uploadStart', () => {
			this.visible = true
			this.status = 'uploading'
		})

		this.uploaderInstance.on('fileProgress', (rootFile, file) => {
			const tracked = this.trackedFiles.find((f) => f.uid === file.uniqueIdentifier)
			if (tracked) {
				tracked.progress = Math.floor(file.progress() * 100)
				tracked.speed = file.averageSpeed
			}
			// file.averageSpeed lives on simple-uploader.js's own plain File
			// objects (not Vue-reactive), refreshed on the library's own
			// interval - re-summed here, on every progress tick, into a
			// reactive property instead.
			this.totalSpeed = this.uploaderInstance.files.reduce((sum, f) => sum + (f.isComplete() ? 0 : f.averageSpeed), 0)
		})

		this.uploaderInstance.on('fileSuccess', (rootFile, file) => {
			const tracked = this.trackedFiles.find((f) => f.uid === file.uniqueIdentifier)
			if (tracked) {
				tracked.status = 'success'
				tracked.progress = 100
			}
		})

		this.uploaderInstance.on('fileError', (rootFile, file, message) => {
			const tracked = this.trackedFiles.find((f) => f.uid === file.uniqueIdentifier)
			let parsedMessage = message
			try {
				parsedMessage = JSON.parse(message).message
			} catch (e) {
				// message wasn't JSON - fall back to the raw string
			}
			if (tracked) {
				tracked.status = 'error'
				tracked.message = parsedMessage
			}
		})

		this.uploaderInstance.on('complete', () => {
			this.status = 'completed'
			this.totalSpeed = 0
			// eslint-disable-next-line no-console
			console.log('[DEBUG UploadTray complete] emitting uploaded, currentPath =', this.currentPath)
			this.$emit('uploaded')
			const hasError = this.trackedFiles.some((file) => file.status === 'error')
			if (!hasError) {
				setTimeout(() => {
					this.visible = false
					this.trackedFiles = []
					this.rawFileMap = {}
				}, 2000)
			}
		})
	},
	beforeDestroy() {
		this.uploaderInstance.off('fileAdded')
		this.uploaderInstance.off('filesSubmitted')
		this.uploaderInstance.off('uploadStart')
		this.uploaderInstance.off('fileProgress')
		this.uploaderInstance.off('fileSuccess')
		this.uploaderInstance.off('fileError')
		this.uploaderInstance.off('complete')
	},
	watch: {
		'$store.state.access_token'(val) {
			this.uploaderInstance.opts.headers.Authorization = val
		},
	},
	methods: {
		// Feed dropped/selected files straight into the uploader, per the
		// task-15 brief's "feed event.dataTransfer.files directly to
		// uploaderInstance.addFiles" option (used instead of assignDrop()
		// since ContentView already owns the dragover/drop DOM listeners).
		addFiles(files) {
			this.uploaderInstance.addFiles(files)
		},
		browse() {
			this.$refs.fileInput.click()
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
			const rawFile = this.rawFileMap[trackedFile.uid]
			if (rawFile) this.uploaderInstance.removeFile(rawFile)
			delete this.rawFileMap[trackedFile.uid]
			const index = this.trackedFiles.findIndex((f) => f.uid === trackedFile.uid)
			if (index > -1) this.trackedFiles.splice(index, 1)
			// removeFile() on the last remaining file doesn't necessarily fire
			// the uploader's own 'complete' event, which is what normally
			// hides the tray - without this, cancelling everything would
			// leave an empty tray floating on screen.
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
	background: #fff;
	border-radius: 12px;
	border: 1px solid rgb(228 233 237);
	box-shadow: 0 10px 28px rgba(0, 0, 0, 0.14);
	overflow: hidden;
}
.upload-tray-header {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: 0.5rem;
	padding: 0.6rem 0.85rem;
	font-weight: 600;
	font-size: 0.85rem;
	border-bottom: 1px solid rgb(228 233 237);
	background: rgba(0, 0, 0, 0.015);
}
.header-icon {
	color: #3273dc;
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
	padding: 0.4rem;
	list-style: none;
}
.total-speed {
	flex-shrink: 0;
	font-weight: 400;
	font-size: 0.75rem;
	color: rgba(0, 0, 0, 0.45);
}
.dismiss-icon {
	flex-shrink: 0;
	color: rgba(0, 0, 0, 0.4);
	&:hover { color: rgba(0, 0, 0, 0.7); }
}
.cancel-icon {
	flex-shrink: 0;
	color: rgba(0, 0, 0, 0.35);
	&:hover { color: #cc0f35; }
}
.upload-tray-item {
	display: flex;
	align-items: flex-start;
	gap: 0.65rem;
	padding: 0.5rem 0.6rem;
	margin-bottom: 0.3rem;
	border-radius: 10px;
	font-size: 0.8rem;
	background: rgba(0, 0, 0, 0.02);
	border: 1px solid rgba(0, 0, 0, 0.04);

	&:last-child {
		margin-bottom: 0;
	}
	&:hover {
		background: rgba(0, 0, 0, 0.04);
	}
	&.is-success {
		background: rgba(72, 199, 116, 0.06);
		border-color: rgba(72, 199, 116, 0.15);
	}
	&.is-error {
		background: rgba(255, 56, 96, 0.06);
		border-color: rgba(255, 56, 96, 0.15);
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
	background: rgba(50, 115, 220, 0.1);

	.is-success & {
		background: rgba(72, 199, 116, 0.15);
	}
	.is-error & {
		background: rgba(255, 56, 96, 0.15);
	}
}
.item-icon {
	color: #3273dc;

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
	gap: 0.5rem;
}
.item-subrow {
	display: flex;
	align-items: baseline;
	gap: 0.5rem;
	margin-top: 0.1rem;
}
.file-size {
	flex-shrink: 0;
	font-size: 0.7rem;
	color: rgba(0, 0, 0, 0.4);
}
.file-speed {
	flex-shrink: 0;
	font-size: 0.7rem;
	color: rgba(0, 0, 0, 0.4);
}
.file-name {
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
	color: #3273dc;
}
.status-text {
	flex-shrink: 0;
	font-weight: 600;
	&.is-success { color: #257942; }
	&.is-error { color: #cc0f35; }
	&.is-waiting { color: rgba(0, 0, 0, 0.4); font-weight: 400; }
}
.progress-track {
	margin-top: 0.4rem;
	height: 4px;
	border-radius: 999px;
	background: rgba(50, 115, 220, 0.12);
	overflow: hidden;
}
.progress-fill {
	height: 100%;
	border-radius: 999px;
	background: #3273dc;
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
</style>
