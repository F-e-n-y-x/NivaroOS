<!-- src/components/files/DropView.vue -->
<!--
	Ground-up UI wrapper around the existing LAN-drop wire protocol
	(./Network.js, moved here unchanged from the now-deleted legacy
	src/components/filebrowser/drop/Network.js during Task 20's cutover -
	not layout code, so not rewritten). Ports the peer-list state machine
	and file-send/receive interaction from the legacy
	src/components/filebrowser/drop/DropPage.vue verbatim, dropping its
	full-page header/close-button chrome (this window already has one)
	and its isDesktop-conditional floating-circle layout in favor of a
	simple responsive peer grid, matching how Task 10's file grid
	already respects filesController.breakpoints.singleColumnGrid.
-->
<template>
	<div id="files-drop-view" class="drop-view">
		<div v-if="!peersArray.length" class="drop-empty">
			<b-icon icon="access-point" custom-size="mdi-48px" class="empty-icon"></b-icon>
			<p class="empty-title">{{ $t('Searching for devices...') }}</p>
			<p class="empty-hint">{{ $t('Open Files Drop on another device on your network to send files between them.') }}</p>
		</div>
		<div v-else class="peer-grid" :class="{ 'single-column': filesController.breakpoints.singleColumnGrid }">
			<button
				v-for="peer in peersArray"
				:key="peer.id"
				class="peer-tile"
				:class="{ disabled: peer.offline || isSelf(peer) }"
				:title="peerTitle(peer)"
				@click="sendTo(peer)"
			>
				<span class="peer-icon-wrap">
					<img :src="deviceIconSrc(peer)" class="peer-icon" :alt="peer.name.displayName" />
					<span class="peer-status" :class="{ online: !peer.offline }"></span>
				</span>
				<span class="peer-name one-line">{{ peer.name.displayName }}</span>
				<span v-if="peerProgress[peer.id]" class="peer-progress">{{ peerProgress[peer.id] }}</span>
			</button>
		</div>
		<input ref="fileInput" type="file" multiple class="hidden-file-input" @change="onFilesPicked" />
	</div>
</template>

<script>
import { ServerConnection, PeersManager } from './Network.js'
import { saveAs } from 'file-saver'
import { mixin } from '@/mixins/mixin'

export default {
	name: 'files-drop-view',
	mixins: [mixin],
	inject: ['filesController'],
	data() {
		return {
			selfId: '',
			peersArray: [],
			peerProgress: {},
			filesQueue: [],
			busy: false,
			pendingPeerId: null,
			server: null,
			peersManager: null,
		}
	},
	mounted() {
		this.selfId = localStorage.getItem('peerid')
		// Same startup delay as legacy DropPage.vue - gives the desktop
		// shell a beat to settle before opening the websocket.
		this.$nextTick(() => {
			setTimeout(() => this.initServer(), 300)
		})
	},
	beforeDestroy() {
		this.$EventBus.$off('peers', this.handlePeers)
		this.$EventBus.$off('display-name', this.handleSelfJoined)
		this.$EventBus.$off('peer-joined', this.handlePeerJoined)
		this.$EventBus.$off('peer-left', this.handlePeerLeft)
		this.$EventBus.$off('file-received', this.handleFileReceived)
		this.$EventBus.$off('notify-user', this.handleNotifyUser)
		this.$EventBus.$off('file-progress', this.handleFileProgress)
		this.$EventBus.$off('close-connection', this.handleCloseConnection)
		this.peersManager && this.peersManager.destory()
		this.peersManager = null
		this.server = null
	},
	methods: {
		initServer() {
			const access_token = localStorage.getItem('access_token')
			const url = `${this.$wsProtocol}//${this.$baseURL}/v1/file/ws?token=${access_token}&peer=${this.selfId}`
			this.server = new ServerConnection(url, this.$EventBus)
			this.peersManager = new PeersManager(this.server, this.$EventBus)
			this.$EventBus.$on('peers', this.handlePeers)
			this.$EventBus.$on('display-name', this.handleSelfJoined)
			this.$EventBus.$on('peer-joined', this.handlePeerJoined)
			this.$EventBus.$on('peer-left', this.handlePeerLeft)
			this.$EventBus.$on('file-received', this.handleFileReceived)
			this.$EventBus.$on('notify-user', this.handleNotifyUser)
			this.$EventBus.$on('file-progress', this.handleFileProgress)
			this.$EventBus.$on('close-connection', this.handleCloseConnection)
		},
		isSelf(peer) {
			return peer.id === this.selfId
		},
		peerTitle(peer) {
			if (this.isSelf(peer)) return this.$t('You are using the device')
			if (peer.offline) return this.$t('The device is offline')
			return this.$t('Click to send the file to the device.')
		},
		// Device-type icon naming/lookup ported as-is from DropItem.vue's
		// `deviceIcon` computed - `peer.name.model` is server-provided (from
		// the peer's user agent), not something this UI computes itself.
		deviceIconSrc(peer) {
			if (this.isSelf(peer)) return require('@/assets/img/drop/self.svg')
			const suffix = peer.offline ? '_offline' : '_online'
			try {
				return require(`@/assets/img/drop/${peer.name.model}${suffix}.svg`)
			} catch (e) {
				return require('@/assets/img/drop/desktop_online.svg')
			}
		},
		// Replaces DropItem.vue's drag-and-drop b-upload target with a plain
		// click-to-pick-a-file flow, per this task's "simple peer grid...
		// clicking one opens a file picker" design.
		sendTo(peer) {
			if (peer.offline || this.isSelf(peer)) return
			this.pendingPeerId = peer.id
			this.$refs.fileInput.click()
		},
		// Same send call DropItem.vue's fileDroped() makes - PeersManager
		// (Network.js) is already listening for this exact event/payload
		// shape and does the actual WebRTC transfer, unchanged.
		onFilesPicked(e) {
			const files = Array.from(e.target.files || [])
			e.target.value = ''
			if (!files.length || !this.pendingPeerId) return
			this.$messageBus('files_filesdrop_start')
			this.$EventBus.$emit('files-selected', { files, to: this.pendingPeerId, from: this.selfId })
			this.pendingPeerId = null
		},
		handleFileProgress(e) {
			const peerId = e.sender || e.recipient
			if (e.progress >= 1) {
				this.$delete(this.peerProgress, peerId)
				return
			}
			this.$set(this.peerProgress, peerId, Math.round(e.progress * 100) + '%')
		},
		handleCloseConnection() {
			this.peerProgress = {}
		},
		handlePeers(peers) {
			this.peersArray = peers
			this.$EventBus.$off('peers', this.handlePeers)
		},
		handleSelfJoined(e) {
			const message = e.message
			const uuid = message.id || localStorage.getItem('peerid')
			localStorage.setItem('peerid', uuid)
			this.selfId = uuid
			const selfPeer = {
				id: uuid,
				name: { deviceName: message.deviceName, displayName: message.displayName },
				rtcSupported: true,
			}
			if (!this.peersArray.some((p) => p.id === uuid)) {
				this.peersArray.push(selfPeer)
			}
		},
		handlePeerJoined(peer) {
			const existing = this.peersArray.find((p) => p.id === peer.id)
			if (!existing) {
				this.peersArray.push(peer)
			} else {
				Object.assign(existing, peer)
			}
		},
		handlePeerLeft(peerId) {
			this.peersArray = this.peersArray.filter((peer) => peer.id !== peerId)
		},
		handleFileReceived(file) {
			this.nextFile(file)
		},
		nextFile(next) {
			if (next) this.filesQueue.push(next)
			if (this.busy) return
			this.busy = true
			this.displayFile(this.filesQueue.shift())
		},
		dequeueFile() {
			if (!this.filesQueue.length) {
				this.busy = false
				return
			}
			setTimeout(() => {
				this.busy = false
				this.nextFile()
			}, 300)
		},
		getDeviceNameFromPeerList(deviceId) {
			const peer = this.peersArray.find((p) => p.id === deviceId)
			return peer ? peer.name.displayName : ''
		},
		displayFile(file) {
			this.$buefy.snackbar.open({
				indefinite: true,
				message: this.$t('Save {name} {size} from {device}.', {
					name: file.file.name,
					size: this.renderSize(file.file.size),
					device: this.getDeviceNameFromPeerList(file.from),
				}),
				type: 'is-file',
				cancelText: this.$t('Ignore'),
				actionText: this.$t('Save'),
				position: 'is-bottom',
				container: '#files-drop-view',
				onAction: () => {
					saveAs(file.file.blob, file.file.name)
					this.dequeueFile()
				},
			})
			const cancelBtn = document.querySelector('#files-drop-view .snackbar .is-cancel')
			cancelBtn && cancelBtn.addEventListener('click', this.onSnackbarClose)
		},
		onSnackbarClose() {
			const cancelBtn = document.querySelector('#files-drop-view .snackbar .is-cancel')
			cancelBtn && cancelBtn.removeEventListener('click', this.onSnackbarClose)
			this.dequeueFile()
		},
		handleNotifyUser(e) {
			const type = e.indexOf('lost') > -1 ? 'is-danger' : 'is-success'
			this.$buefy.toast.open({ duration: 2000, message: this.$t(e), type, container: '#files-drop-view' })
		},
	},
}
</script>

<style lang="scss" scoped>
.drop-view {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	min-height: 0;
	overflow: auto;
	padding: 1.5rem;
}
.drop-empty {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	height: 100%;
	color: rgba(0, 0, 0, 0.35);
	text-align: center;
	padding: 1rem;
}
.empty-icon {
	margin-bottom: 0.5rem;
	opacity: 0.6;
}
.empty-title {
	font-size: 0.95rem;
	font-weight: 600;
	color: rgba(0, 0, 0, 0.55);
}
.empty-hint {
	font-size: 0.8rem;
	margin-top: 0.25rem;
	max-width: 22rem;
}
.peer-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(6rem, 1fr));
	gap: 1.25rem 0.5rem;
	align-content: start;
	&.single-column {
		grid-template-columns: 1fr;
	}
}
.peer-tile {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: 0.4rem;
	border: none;
	background: none;
	cursor: pointer;
	padding: 0.75rem 0.5rem;
	border-radius: 10px;
	&:hover {
		background: rgba(0, 0, 0, 0.04);
	}
	&.disabled {
		opacity: 0.5;
		cursor: default;
		&:hover {
			background: none;
		}
	}
}
.peer-icon-wrap {
	position: relative;
	width: 3.5rem;
	height: 3.5rem;
}
.peer-icon {
	width: 100%;
	height: 100%;
	object-fit: contain;
}
.peer-status {
	position: absolute;
	right: 0;
	bottom: 0;
	width: 0.65rem;
	height: 0.65rem;
	border-radius: 50%;
	background: rgba(0, 0, 0, 0.2);
	border: 2px solid #fff;
	&.online {
		background: #3dd06a;
	}
}
.peer-name {
	font-size: 0.8rem;
	color: #2c3e50;
	max-width: 6.5rem;
	text-align: center;
}
.peer-progress {
	font-size: 0.7rem;
	color: #3273dc;
	font-weight: 600;
}
.hidden-file-input {
	display: none;
}
</style>
