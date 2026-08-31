<template>
	<div class="legacy-app-edit-window">
		<section class="edit-body">
			<div class="node-card">
				<b-field :label="$t('Name')">
					<b-input v-model="name" :placeholder="originalName" expanded></b-input>
				</b-field>

				<b-field v-if="showUrlField" :label="$t('URL')">
					<b-input v-model="url" :placeholder="$t('Opened when the icon is clicked (optional)')" expanded>
					</b-input>
				</b-field>

				<b-field :label="$t('Icon')">
					<div class="icon-field">
						<div
							:style="{ borderRadius: iconRadius + '%' }"
							class="icon-preview"
							:title="$t('Click to edit')"
							@click="showIconEditor = true"
						>
							<img :src="icon || fallbackIcon" :alt="name || originalName">
							<span class="icon-preview-edit-hint">{{ $t('Edit') }}</span>
						</div>

						<div class="icon-field-controls">
							<div class="source-toggle">
								<button :class="{ active: iconTab === 'suggestions' }" type="button" @click="iconTab = 'suggestions'">
									<i class="mdi mdi-shopping-outline mr-1"></i>
									{{ $t('App Store') }}
								</button>
								<button :class="{ active: iconTab === 'url' }" type="button" @click="iconTab = 'url'">
									<i class="mdi mdi-link-variant mr-1"></i>
									{{ $t('URL') }}
								</button>
								<button :class="{ active: iconTab === 'upload' }" type="button" @click="iconTab = 'upload'">
									<i class="mdi mdi-upload-outline mr-1"></i>
									{{ $t('Upload') }}
								</button>
							</div>

							<!-- 1. App Store Suggestions Tab -->
							<div v-if="iconTab === 'suggestions'" class="store-suggestions-container mt-2">
								<b-input
									v-model="storeSearch"
									:placeholder="$t('Search App Store icons...')"
									icon="magnify"
									size="is-small"
									class="mb-2"
								></b-input>

								<div class="store-icons-grid" :class="{ 'is-loading': loadingStoreIcons }">
									<div
										v-for="app in filteredStoreApps"
										:key="app.id || app.title"
										class="store-icon-card"
										:class="{ 'is-selected': (iconRaw || icon) === app.icon }"
										:title="app.title"
										@click="selectStoreIcon(app)"
									>
										<img :src="app.icon" class="store-icon-thumb" :alt="app.title" loading="lazy" />
										<span class="store-icon-name">{{ app.title }}</span>
									</div>
									<div v-if="!filteredStoreApps.length" class="has-text-grey is-size-7 is-flex-grow-1 p-3 has-text-centered">
										{{ $t('No matching icons found') }}
									</div>
								</div>
							</div>

							<!-- 2. Direct URL Input Tab -->
							<b-input
								v-else-if="iconTab === 'url'"
								v-model="icon"
								:loading="isCompressing"
								:placeholder="$t('Icon URL')"
								class="mt-2"
								expanded
								@blur="compressIconUrl"
							></b-input>

							<!-- 3. Upload File Tab -->
							<div v-else class="mt-2">
								<b-button :loading="isCompressing" expanded @click="$refs.iconFile.click()">
									<i class="mdi mdi-image-outline mr-1"></i>
									{{ $t('Choose image file') }}
								</b-button>
								<input ref="iconFile" accept="image/*" style="display: none" type="file" @change="handleIconFile">
							</div>
						</div>
					</div>
				</b-field>
			</div>
		</section>
		<footer class="edit-footer is-flex is-align-items-center">
			<div class="is-flex-grow-1"></div>
			<div>
				<b-button :label="$t('Cancel')" rounded @click="$emit('close')" />
				<b-button :label="$t('Save')" expanded rounded type="is-primary" @click="save" />
			</div>
		</footer>

		<b-modal
			v-model="showIconEditor"
			:can-cancel="['escape', 'outside']"
			animation="zoom-in"
			aria-modal
			has-modal-card
		>
			<template #default>
				<icon-editor-modal
					v-if="icon || iconRaw"
					:initial-radius="iconRadius"
					:initial-zoom="iconZoom"
					:initial-offset-x="iconOffsetX"
					:initial-offset-y="iconOffsetY"
					:src="iconRaw || icon"
					@apply="handleIconEdited"
					@close="showIconEditor = false"
				></icon-editor-modal>
			</template>
		</b-modal>
	</div>
</template>

<script>
import IconEditorModal from './IconEditorModal.vue'
import { ice_i18n } from '@/mixins/base/common-i18n'
import business_LegacyAppOverrides from '@/mixins/app/Business_LegacyAppOverrides'
import events from '@/events/events'

const ICON_MAX_DIM = 256

const POPULAR_STORE_ICONS = [
	{ title: 'Nextcloud', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Nextcloud/icon.png' },
	{ title: 'Plex', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Plex/icon.png' },
	{ title: 'Jellyfin', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Jellyfin/icon.png' },
	{ title: 'Home Assistant', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/HomeAssistant/icon.png' },
	{ title: 'AdGuard Home', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/AdGuardHome/icon.png' },
	{ title: 'Transmission', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Transmission/icon.png' },
	{ title: 'qBittorrent', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/qBittorrent/icon.png' },
	{ title: 'Nginx Proxy Manager', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/NginxProxyManager/icon.png' },
	{ title: 'Portainer', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Portainer/icon.png' },
	{ title: 'Vaultwarden', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Vaultwarden/icon.png' },
	{ title: 'Pi-hole', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Pi-hole/icon.png' },
	{ title: 'Tailscale', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Tailscale/icon.png' },
	{ title: 'Syncthing', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Syncthing/icon.png' },
	{ title: 'Photoprism', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/PhotoPrism/icon.png' },
	{ title: 'Calibre-web', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Calibre-web/icon.png' },
	{ title: 'Emby', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Emby/icon.png' },
	{ title: 'Grafana', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Grafana/icon.png' },
	{ title: 'Uptime Kuma', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/UptimeKuma/icon.png' },
	{ title: 'Duplicati', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Duplicati/icon.png' },
	{ title: 'Paperless-ngx', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Paperless-ngx/icon.png' },
	{ title: 'Node-RED', icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Node-RED/icon.png' }
]

export default {
	mixins: [business_LegacyAppOverrides],
	components: {
		IconEditorModal
	},
	props: {
		item: {
			type: Object,
			required: true
		},
		override: {
			type: Object,
			default: null
		}
	},
	data() {
		return {
			name: '',
			url: '',
			icon: '',
			iconRaw: '',
			iconZoom: 1,
			iconOffsetX: 0,
			iconOffsetY: 0,
			iconRadius: 0,
			iconTab: 'suggestions',
			storeSearch: '',
			storeApps: [...POPULAR_STORE_ICONS],
			loadingStoreIcons: false,
			isCompressing: false,
			showIconEditor: false,
			fallbackIcon: require('@/assets/img/app/default.svg')
		}
	},
	computed: {
		showUrlField() {
			return this.item.app_type === 'container'
		},
		originalName() {
			return (this.item.title && ice_i18n({ ...this.item.title, custom: undefined })) || this.item.name
		},
		filteredStoreApps() {
			if (!this.storeSearch.trim()) return this.storeApps
			const q = this.storeSearch.trim().toLowerCase()
			return this.storeApps.filter(app => app.title.toLowerCase().includes(q))
		}
	},
	created() {
		if (this.override) {
			this.name = this.override.title || ''
			this.url = this.override.url || ''
			this.icon = this.override.icon || ''
			this.iconRaw = this.override.iconRaw || this.override.icon || this.item.icon || ''
			this.iconZoom = this.override.iconZoom !== undefined ? this.override.iconZoom : 1
			this.iconOffsetX = this.override.iconOffsetX || 0
			this.iconOffsetY = this.override.iconOffsetY || 0
			this.iconRadius = this.override.iconRadius || 0
		} else {
			this.icon = this.item.icon || ''
			this.iconRaw = this.item.icon || ''
			this.iconZoom = 1
			this.iconOffsetX = 0
			this.iconOffsetY = 0
			this.iconRadius = this.item.iconRadius || 0
		}
		this.fetchStoreIcons()
	},
	methods: {
		async fetchStoreIcons() {
			this.loadingStoreIcons = true
			try {
				const res = await this.$openAPI.appManagement.appStore.composeAppStoreInfoList()
				const list = res.data?.data?.list || {}
				const dynamicApps = Object.keys(list).map(id => ({
					id,
					title: ice_i18n(list[id].title) || id,
					icon: list[id].icon
				})).filter(a => a.icon)
				if (dynamicApps.length) {
					// Merge dynamic apps with popular list, avoiding duplicates
					const map = new Map()
					POPULAR_STORE_ICONS.forEach(a => map.set(a.title.toLowerCase(), a))
					dynamicApps.forEach(a => map.set(a.title.toLowerCase(), a))
					this.storeApps = Array.from(map.values())
				}
			} catch (e) {
				// Keep popular icons fallback
			} finally {
				this.loadingStoreIcons = false
			}
		},

		selectStoreIcon(app) {
			this.icon = app.icon
			this.iconRaw = app.icon
			this.iconZoom = 1
			this.iconOffsetX = 0
			this.iconOffsetY = 0
		},

		resizeImageToDataUrl(img) {
			const scale = Math.min(1, ICON_MAX_DIM / Math.max(img.width, img.height))
			const canvas = document.createElement('canvas')
			canvas.width = Math.round(img.width * scale)
			canvas.height = Math.round(img.height * scale)
			const ctx = canvas.getContext('2d')
			ctx.drawImage(img, 0, 0, canvas.width, canvas.height)
			return canvas.toDataURL('image/png')
		},

		handleIconFile(event) {
			const file = event.target.files[0]
			if (!file) return
			this.isCompressing = true
			const reader = new FileReader()
			reader.onload = () => {
				const img = new Image()
				img.onload = () => {
					const dataUrl = this.resizeImageToDataUrl(img)
					this.icon = dataUrl
					this.iconRaw = dataUrl
					this.iconZoom = 1
					this.iconOffsetX = 0
					this.iconOffsetY = 0
					this.isCompressing = false
				}
				img.src = reader.result
			}
			reader.readAsDataURL(file)
		},

		compressIconUrl() {
			if (!this.icon || this.icon.startsWith('data:')) return
			const url = this.icon
			this.iconRaw = url
			this.iconZoom = 1
			this.iconOffsetX = 0
			this.iconOffsetY = 0
			this.isCompressing = true
			const img = new Image()
			img.crossOrigin = 'anonymous'
			img.onload = () => {
				try {
					const dataUrl = this.resizeImageToDataUrl(img)
					this.icon = dataUrl
					this.iconRaw = dataUrl
				} catch (e) {
					// Keep original URL
				}
				this.isCompressing = false
			}
			img.onerror = () => {
				this.isCompressing = false
			}
			img.src = url
		},

		handleIconEdited({ dataUrl, rawSrc, zoom, offsetX, offsetY, radius }) {
			if (dataUrl) this.icon = dataUrl
			if (rawSrc) this.iconRaw = rawSrc
			this.iconZoom = zoom !== undefined ? zoom : 1
			this.iconOffsetX = offsetX || 0
			this.iconOffsetY = offsetY || 0
			this.iconRadius = radius || 0
		},

		save() {
			this.saveLegacyAppOverride(this.item.name, {
				title: this.name,
				url: this.url,
				icon: this.icon,
				iconRaw: this.iconRaw,
				iconZoom: this.iconZoom,
				iconOffsetX: this.iconOffsetX,
				iconOffsetY: this.iconOffsetY,
				iconRadius: this.iconRadius
			}).then(() => {
				this.$EventBus.$emit(events.RELOAD_APP_LIST)
			})
			this.$emit('close')
		}
	}
}
</script>

<style lang="scss" scoped>
.legacy-app-edit-window {
	height: 100%;
	display: flex;
	flex-direction: column;
	background: #fff;
}

.edit-body {
	flex: 1 1 auto;
	overflow: auto;
	padding: 1.5rem;
}

.edit-footer {
	flex-shrink: 0;
	padding: 0.75rem 1.5rem;
	border-top: 1px solid #f0f0f0;
	background: #fafafa;
}

.node-card {
	max-width: 32rem;
	margin: 0 auto;
}

.icon-field {
	display: flex;
	gap: 1.25rem;
	align-items: flex-start;
}

.icon-preview {
	position: relative;
	width: 5rem;
	height: 5rem;
	flex-shrink: 0;
	overflow: hidden;
	background: #0f172a;
	border: 1px solid #cbd5e1;
	cursor: pointer;
	display: flex;
	align-items: center;
	justify-content: center;
	transition: all 0.15s ease;
	box-shadow: 0 2px 6px rgba(0, 0, 0, 0.1);

	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}

	.icon-preview-edit-hint {
		position: absolute;
		inset: 0;
		background: rgba(0, 0, 0, 0.6);
		color: #fff;
		font-size: 0.75rem;
		font-weight: 600;
		display: flex;
		align-items: center;
		justify-content: center;
		opacity: 0;
		transition: opacity 0.15s ease;
	}

	&:hover {
		border-color: #2563eb;
		.icon-preview-edit-hint {
			opacity: 1;
		}
	}
}

.icon-field-controls {
	flex: 1 1 auto;
	min-width: 0;
}

.source-toggle {
	display: inline-flex;
	border: 1px solid #cbd5e1;
	border-radius: 8px;
	overflow: hidden;

	button {
		border: none;
		background: #f8fafc;
		padding: 0.35rem 0.85rem;
		font-size: 0.8rem;
		cursor: pointer;
		color: #475569;
		transition: all 0.15s ease;
		display: inline-flex;
		align-items: center;

		&.active {
			background: #2563eb;
			color: #fff;
			font-weight: 600;
		}
	}
}

.store-suggestions-container {
	border: 1px solid #e2e8f0;
	border-radius: 10px;
	padding: 0.5rem;
	background: #f8fafc;
}

.store-icons-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(68px, 1fr));
	gap: 0.5rem;
	max-height: 175px;
	overflow-y: auto;
	padding: 0.25rem;
}

.store-icon-card {
	display: flex;
	flex-direction: column;
	align-items: center;
	padding: 0.4rem 0.25rem;
	border-radius: 8px;
	background: #ffffff;
	border: 1px solid #e2e8f0;
	cursor: pointer;
	transition: all 0.15s ease;
	text-align: center;

	&:hover {
		border-color: #2563eb;
		transform: translateY(-2px);
		box-shadow: 0 4px 10px rgba(0, 0, 0, 0.08);
	}

	&.is-selected {
		border-color: #2563eb;
		background: #eff6ff;
		box-shadow: 0 0 0 2px rgba(37, 99, 235, 0.2);
	}
}

.store-icon-thumb {
	width: 36px;
	height: 36px;
	border-radius: 8px;
	object-fit: cover;
	margin-bottom: 0.25rem;
}

.store-icon-name {
	font-size: 0.65rem;
	font-weight: 500;
	color: #475569;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	width: 100%;
	display: block;
}
</style>
