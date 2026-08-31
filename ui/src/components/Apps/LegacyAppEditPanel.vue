<template>
	<div class="app-studio-window">
		<div class="app-studio-body">
			<!-- Left Column: Live Icon Editor & Preview Studio -->
			<div class="studio-sidebar">
				<div class="studio-sidebar-header">
					<span class="studio-section-title">{{ $t('Icon & Preview') }}</span>
					<button class="btn-text-sm" type="button" @click="resetTransforms">
						<i class="mdi mdi-restore mr-1"></i>{{ $t('Reset') }}
					</button>
				</div>

				<!-- Interactive Pan & Zoom Viewport -->
				<div
					ref="viewport"
					class="studio-viewport"
					:class="{ 'is-draggable': zoom > 1, 'is-dragging': dragging }"
					@mousedown="startDrag"
					@touchstart="startDrag"
				>
					<div class="studio-crop-box" :style="{ borderRadius: iconRadius + '%' }">
						<img
							ref="previewImg"
							:src="iconRaw || icon || fallbackIcon"
							:style="imgTransformStyle"
							draggable="false"
							class="studio-icon-img"
							@load="onImageLoaded"
						/>
					</div>
					<div v-if="zoom > 1" class="drag-hint">
						<i class="mdi mdi-cursor-move mr-1"></i>{{ $t('Drag to reposition') }}
					</div>
				</div>

				<!-- Studio Controls: Zoom & Roundness -->
				<div class="studio-controls-card">
					<!-- Zoom Slider -->
					<div class="control-row">
						<div class="control-label">
							<i class="mdi mdi-magnify-plus-outline"></i>
							<span>{{ $t('Zoom') }}</span>
						</div>
						<input
							v-model.number="iconZoom"
							max="3"
							min="1"
							step="0.02"
							type="range"
							class="studio-slider"
						/>
						<span class="control-badge">{{ Math.round(iconZoom * 100) }}%</span>
					</div>

					<!-- Roundness Slider -->
					<div class="control-row">
						<div class="control-label">
							<i class="mdi mdi-rounded-corner"></i>
							<span>{{ $t('Roundness') }}</span>
						</div>
						<input
							v-model.number="iconRadius"
							max="50"
							min="0"
							step="1"
							type="range"
							class="studio-slider"
						/>
						<span class="control-badge">{{ iconRadius }}%</span>
					</div>

					<!-- Quick Roundness Presets -->
					<div class="preset-pills">
						<button
							v-for="p in roundnessPresets"
							:key="p.value"
							type="button"
							class="preset-pill"
							:class="{ active: iconRadius === p.value }"
							@click="iconRadius = p.value"
						>
							{{ p.label }}
						</button>
					</div>
				</div>

				<!-- Desktop Miniature Simulation -->
				<div class="desktop-mini-preview">
					<div class="mini-card-icon" :style="{ borderRadius: iconRadius + '%' }">
						<img :src="iconRaw || icon || fallbackIcon" :style="imgTransformStyle" draggable="false" />
					</div>
					<div class="mini-card-title">{{ displayName }}</div>
				</div>
			</div>

			<!-- Right Column: App Information & Icon Sources -->
			<div class="studio-main">
				<!-- 1. App General Info Card -->
				<div class="studio-card mb-4">
					<div class="card-title-row">
						<span class="card-heading">{{ $t('General Details') }}</span>
					</div>

					<b-field :label="$t('Display Name')" class="mb-3">
						<b-input
							v-model="name"
							:placeholder="originalName"
							icon="pencil-outline"
							expanded
						></b-input>
					</b-field>

					<b-field v-if="showUrlField" :label="$t('Web UI URL')" class="mb-0">
						<b-input
							v-model="url"
							:placeholder="$t('e.g. http://192.168.1.50:8080 or /my-app')"
							icon="link-variant"
							expanded
						></b-input>
					</b-field>
				</div>

				<!-- 2. Icon Selection Card -->
				<div class="studio-card">
					<div class="card-title-row">
						<span class="card-heading">{{ $t('Select Icon') }}</span>
						<div class="source-nav-tabs">
							<button
								type="button"
								class="nav-tab"
								:class="{ active: iconTab === 'store' }"
								@click="iconTab = 'store'"
							>
								<i class="mdi mdi-shopping-outline mr-1"></i>{{ $t('App Store') }}
							</button>
							<button
								type="button"
								class="nav-tab"
								:class="{ active: iconTab === 'url' }"
								@click="iconTab = 'url'"
							>
								<i class="mdi mdi-link-variant mr-1"></i>{{ $t('URL') }}
							</button>
							<button
								type="button"
								class="nav-tab"
								:class="{ active: iconTab === 'upload' }"
								@click="iconTab = 'upload'"
							>
								<i class="mdi mdi-upload-outline mr-1"></i>{{ $t('Upload') }}
							</button>
							<button
								type="button"
								class="nav-tab"
								:class="{ active: iconTab === 'palette' }"
								@click="iconTab = 'palette'"
							>
								<i class="mdi mdi-palette-outline mr-1"></i>{{ $t('Badges') }}
							</button>
						</div>
					</div>

					<!-- Tab A: App Store Suggestions Grid -->
					<div v-if="iconTab === 'store'" class="tab-pane">
						<b-input
							v-model="storeSearch"
							:placeholder="$t('Search App Store icons...')"
							icon="magnify"
							size="is-small"
							class="mb-3"
							clearable
						></b-input>

						<div class="store-grid-wrapper" :class="{ 'is-loading': loadingStoreIcons }">
							<div
								v-for="app in filteredStoreApps"
								:key="app.id || app.title"
								class="store-icon-tile"
								:class="{ 'is-active': isCurrentIcon(app.icon) }"
								:title="app.title"
								@click="selectStoreIcon(app)"
							>
								<img :src="app.icon" class="tile-icon" :alt="app.title" loading="lazy" />
								<span class="tile-label">{{ app.title }}</span>
							</div>
							<div v-if="!filteredStoreApps.length" class="empty-state">
								{{ $t('No matching App Store icons found') }}
							</div>
						</div>
					</div>

					<!-- Tab B: Direct Image URL -->
					<div v-else-if="iconTab === 'url'" class="tab-pane">
						<b-field :label="$t('Image Address (PNG, SVG, JPG, WebP)')">
							<b-input
								v-model="inputUrl"
								:placeholder="$t('Paste an image URL here...')"
								icon="link"
								expanded
								@blur="applyCustomUrl"
								@keyup.enter.native="applyCustomUrl"
							></b-input>
						</b-field>
						<b-button
							type="is-primary"
							outlined
							size="is-small"
							:loading="isCompressing"
							class="mt-2"
							@click="applyCustomUrl"
						>
							<i class="mdi mdi-check mr-1"></i>{{ $t('Load Image') }}
						</b-button>
					</div>

					<!-- Tab C: File Upload -->
					<div v-else-if="iconTab === 'upload'" class="tab-pane">
						<div class="upload-dropzone" @click="$refs.iconFile.click()">
							<i class="mdi mdi-cloud-upload-outline dropzone-icon"></i>
							<div class="dropzone-text">{{ $t('Click to browse or drop an image') }}</div>
							<div class="dropzone-sub">PNG, SVG, JPG, WebP (auto-optimized)</div>
						</div>
						<input
							ref="iconFile"
							accept="image/*"
							style="display: none"
							type="file"
							@change="handleIconFile"
						/>
					</div>

					<!-- Tab D: Monogram Badge Generator -->
					<div v-else-if="iconTab === 'palette'" class="tab-pane">
						<div class="badge-grid">
							<button
								v-for="(bg, idx) in badgeGradients"
								:key="idx"
								type="button"
								class="badge-tile"
								:style="{ background: bg }"
								@click="generateBadgeIcon(bg)"
							>
								<span>{{ badgeLetter }}</span>
							</button>
						</div>
						<div class="badge-hint">{{ $t('Generates a clean minimalist letter monogram icon') }}</div>
					</div>
				</div>
			</div>
		</div>

		<!-- Window Footer -->
		<footer class="app-studio-footer">
			<button
				v-if="override"
				type="button"
				class="btn-reset-overrides"
				@click="resetAllOverrides"
			>
				<i class="mdi mdi-restore mr-1"></i>{{ $t('Reset to Defaults') }}
			</button>
			<div class="is-flex-grow-1"></div>
			<div class="is-flex" style="gap: 0.75rem;">
				<b-button rounded @click="$emit('close')">{{ $t('Cancel') }}</b-button>
				<b-button type="is-primary" rounded :loading="isSaving" @click="save">
					{{ $t('Save Changes') }}
				</b-button>
			</div>
		</footer>
	</div>
</template>

<script>
import { ice_i18n } from '@/mixins/base/common-i18n'
import business_LegacyAppOverrides from '@/mixins/app/Business_LegacyAppOverrides'
import events from '@/events/events'

const VIEWPORT_SIZE = 190
const OUTPUT_SIZE = 256

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

const BADGE_GRADIENTS = [
	'linear-gradient(135deg, #2563eb, #1d4ed8)',
	'linear-gradient(135deg, #7c3aed, #6d28d9)',
	'linear-gradient(135deg, #db2777, #be185d)',
	'linear-gradient(135deg, #ea580c, #c2410c)',
	'linear-gradient(135deg, #059669, #047857)',
	'linear-gradient(135deg, #0891b2, #0e7490)',
	'linear-gradient(135deg, #475569, #334155)',
	'linear-gradient(135deg, #1e293b, #0f172a)'
]

export default {
	name: 'LegacyAppEditPanel',
	mixins: [business_LegacyAppOverrides],
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
			iconTab: 'store',
			inputUrl: '',
			storeSearch: '',
			storeApps: [...POPULAR_STORE_ICONS],
			loadingStoreIcons: false,
			isCompressing: false,
			isSaving: false,
			dragging: false,
			dragStart: null,
			badgeGradients: BADGE_GRADIENTS,
			roundnessPresets: [
				{ label: 'Square (0%)', value: 0 },
				{ label: 'Squircle (18%)', value: 18 },
				{ label: 'Smooth (30%)', value: 30 },
				{ label: 'Circle (50%)', value: 50 }
			],
			fallbackIcon: require('@/assets/img/app/default.svg')
		}
	},
	computed: {
		showUrlField() {
			return this.item.app_type === 'container' || this.item.app_type === 'LinkApp'
		},
		originalName() {
			return (this.item.title && ice_i18n({ ...this.item.title, custom: undefined })) || this.item.name
		},
		displayName() {
			return this.name.trim() || this.originalName
		},
		badgeLetter() {
			const name = this.displayName.trim()
			return name ? name.charAt(0).toUpperCase() : 'A'
		},
		filteredStoreApps() {
			if (!this.storeSearch.trim()) return this.storeApps
			const q = this.storeSearch.trim().toLowerCase()
			return this.storeApps.filter(app => app.title.toLowerCase().includes(q))
		},
		imgTransformStyle() {
			return {
				transform: `translate3d(${this.iconOffsetX}px, ${this.iconOffsetY}px, 0) scale(${this.iconZoom})`,
				transformOrigin: 'center center'
			}
		}
	},
	watch: {
		iconZoom() {
			this.clampOffsets()
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
		if (this.iconRaw && !this.iconRaw.startsWith('data:')) {
			this.inputUrl = this.iconRaw
		}
		this.fetchStoreIcons()
	},
	beforeDestroy() {
		this.stopDrag()
	},
	methods: {
		isCurrentIcon(src) {
			return (this.iconRaw || this.icon) === src
		},
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
					const map = new Map()
					POPULAR_STORE_ICONS.forEach(a => map.set(a.title.toLowerCase(), a))
					dynamicApps.forEach(a => map.set(a.title.toLowerCase(), a))
					this.storeApps = Array.from(map.values())
				}
			} catch (e) {
				// Fallback to popular icons
			} finally {
				this.loadingStoreIcons = false
			}
		},

		selectStoreIcon(app) {
			this.icon = app.icon
			this.iconRaw = app.icon
			this.inputUrl = app.icon
			this.iconZoom = 1
			this.iconOffsetX = 0
			this.iconOffsetY = 0
		},

		applyCustomUrl() {
			if (!this.inputUrl.trim()) return
			this.icon = this.inputUrl.trim()
			this.iconRaw = this.inputUrl.trim()
			this.iconZoom = 1
			this.iconOffsetX = 0
			this.iconOffsetY = 0
		},

		generateBadgeIcon(gradient) {
			const canvas = document.createElement('canvas')
			canvas.width = OUTPUT_SIZE
			canvas.height = OUTPUT_SIZE
			const ctx = canvas.getContext('2d')

			// Parse gradient stops or draw
			const grad = ctx.createLinearGradient(0, 0, OUTPUT_SIZE, OUTPUT_SIZE)
			if (gradient.includes('#2563eb')) {
				grad.addColorStop(0, '#2563eb')
				grad.addColorStop(1, '#1d4ed8')
			} else if (gradient.includes('#7c3aed')) {
				grad.addColorStop(0, '#7c3aed')
				grad.addColorStop(1, '#6d28d9')
			} else if (gradient.includes('#db2777')) {
				grad.addColorStop(0, '#db2777')
				grad.addColorStop(1, '#be185d')
			} else if (gradient.includes('#ea580c')) {
				grad.addColorStop(0, '#ea580c')
				grad.addColorStop(1, '#c2410c')
			} else if (gradient.includes('#059669')) {
				grad.addColorStop(0, '#059669')
				grad.addColorStop(1, '#047857')
			} else if (gradient.includes('#0891b2')) {
				grad.addColorStop(0, '#0891b2')
				grad.addColorStop(1, '#0e7490')
			} else if (gradient.includes('#475569')) {
				grad.addColorStop(0, '#475569')
				grad.addColorStop(1, '#334155')
			} else {
				grad.addColorStop(0, '#1e293b')
				grad.addColorStop(1, '#0f172a')
			}

			ctx.fillStyle = grad
			ctx.fillRect(0, 0, OUTPUT_SIZE, OUTPUT_SIZE)

			ctx.fillStyle = '#ffffff'
			ctx.font = 'bold 120px Inter, system-ui, -apple-system, sans-serif'
			ctx.textAlign = 'center'
			ctx.textBaseline = 'middle'
			ctx.fillText(this.badgeLetter, OUTPUT_SIZE / 2, OUTPUT_SIZE / 2 + 6)

			const dataUrl = canvas.toDataURL('image/png')
			this.icon = dataUrl
			this.iconRaw = dataUrl
			this.iconZoom = 1
			this.iconOffsetX = 0
			this.iconOffsetY = 0
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

		resizeImageToDataUrl(img) {
			const scale = Math.min(1, OUTPUT_SIZE / Math.max(img.width, img.height))
			const canvas = document.createElement('canvas')
			canvas.width = Math.round(img.width * scale)
			canvas.height = Math.round(img.height * scale)
			const ctx = canvas.getContext('2d')
			ctx.drawImage(img, 0, 0, canvas.width, canvas.height)
			return canvas.toDataURL('image/png')
		},

		onImageLoaded() {
			this.clampOffsets()
		},

		clampOffsets() {
			const maxOffsetX = Math.max(0, ((this.iconZoom - 1) * VIEWPORT_SIZE) / 2)
			const maxOffsetY = Math.max(0, ((this.iconZoom - 1) * VIEWPORT_SIZE) / 2)
			this.iconOffsetX = Math.min(maxOffsetX, Math.max(-maxOffsetX, this.iconOffsetX))
			this.iconOffsetY = Math.min(maxOffsetY, Math.max(-maxOffsetY, this.iconOffsetY))
		},

		resetTransforms() {
			this.iconZoom = 1
			this.iconOffsetX = 0
			this.iconOffsetY = 0
			this.iconRadius = 0
		},

		startDrag(e) {
			if (this.iconZoom <= 1) return
			const point = e.touches ? e.touches[0] : e
			this.dragging = true
			this.dragStart = {
				x: point.clientX,
				y: point.clientY,
				offsetX: this.iconOffsetX,
				offsetY: this.iconOffsetY
			}
			window.addEventListener('mousemove', this.onDrag)
			window.addEventListener('touchmove', this.onDrag)
			window.addEventListener('mouseup', this.stopDrag)
			window.addEventListener('touchend', this.stopDrag)
		},

		onDrag(e) {
			if (!this.dragging) return
			const point = e.touches ? e.touches[0] : e
			const dx = point.clientX - this.dragStart.x
			const dy = point.clientY - this.dragStart.y
			const maxOffsetX = Math.max(0, ((this.iconZoom - 1) * VIEWPORT_SIZE) / 2)
			const maxOffsetY = Math.max(0, ((this.iconZoom - 1) * VIEWPORT_SIZE) / 2)
			this.iconOffsetX = Math.min(maxOffsetX, Math.max(-maxOffsetX, this.dragStart.offsetX + dx))
			this.iconOffsetY = Math.min(maxOffsetY, Math.max(-maxOffsetY, this.dragStart.offsetY + dy))
		},

		stopDrag() {
			this.dragging = false
			window.removeEventListener('mousemove', this.onDrag)
			window.removeEventListener('touchmove', this.onDrag)
			window.removeEventListener('mouseup', this.stopDrag)
			window.removeEventListener('touchend', this.stopDrag)
		},

		bakeRenderedIcon() {
			const canvas = document.createElement('canvas')
			canvas.width = OUTPUT_SIZE
			canvas.height = OUTPUT_SIZE
			const ctx = canvas.getContext('2d')

			try {
				const img = this.$refs.previewImg
				if (!img) return this.iconRaw || this.icon
				const ratio = OUTPUT_SIZE / VIEWPORT_SIZE

				ctx.save()
				ctx.translate(OUTPUT_SIZE / 2, OUTPUT_SIZE / 2)
				ctx.translate(this.iconOffsetX * ratio, this.iconOffsetY * ratio)
				ctx.scale(this.iconZoom, this.iconZoom)
				ctx.drawImage(img, -OUTPUT_SIZE / 2, -OUTPUT_SIZE / 2, OUTPUT_SIZE, OUTPUT_SIZE)
				ctx.restore()

				return canvas.toDataURL('image/png')
			} catch (e) {
				return this.iconRaw || this.icon
			}
		},

		async resetAllOverrides() {
			await this.saveLegacyAppOverride(this.item.name, null)
			this.$EventBus.$emit(events.RELOAD_APP_LIST)
			this.$buefy.toast.open({
				message: `<i class="mdi mdi-restore mr-1"></i> ${this.$t('App reset to original defaults')}`,
				type: 'is-dark',
				position: 'is-top',
				duration: 2000,
				queue: false
			})
			this.$emit('close')
		},

		async save() {
			this.isSaving = true
			const bakedIcon = this.bakeRenderedIcon()
			await this.saveLegacyAppOverride(this.item.name, {
				title: this.name,
				url: this.url,
				icon: bakedIcon,
				iconRaw: this.iconRaw || this.icon,
				iconZoom: this.iconZoom,
				iconOffsetX: this.iconOffsetX,
				iconOffsetY: this.iconOffsetY,
				iconRadius: this.iconRadius
			})
			this.$EventBus.$emit(events.RELOAD_APP_LIST)
			this.isSaving = false
			this.$buefy.toast.open({
				message: `<i class="mdi mdi-check-circle-outline mr-1"></i> ${this.$t('App settings saved')}`,
				type: 'is-dark',
				position: 'is-top',
				duration: 2000,
				queue: false
			})
			this.$emit('close')
		}
	}
}
</script>

<style lang="scss" scoped>
.app-studio-window {
	height: 100%;
	display: flex;
	flex-direction: column;
	background: #f8fafc;
	color: #0f172a;
	user-select: none;
}

.app-studio-body {
	flex: 1 1 auto;
	display: flex;
	overflow: hidden;
}

/* Left Column: Live Studio */
.studio-sidebar {
	width: 275px;
	flex-shrink: 0;
	padding: 1.25rem;
	background: #ffffff;
	border-right: 1px solid #e2e8f0;
	display: flex;
	flex-direction: column;
	overflow-y: auto;
}

.studio-sidebar-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 0.85rem;
}

.studio-section-title {
	font-size: 0.875rem;
	font-weight: 700;
	color: #0f172a;
}

.btn-text-sm {
	border: none;
	background: transparent;
	font-size: 0.75rem;
	font-weight: 600;
	color: #2563eb;
	cursor: pointer;
	padding: 0;

	&:hover {
		text-decoration: underline;
	}
}

/* Viewport */
.studio-viewport {
	position: relative;
	width: 190px;
	height: 190px;
	margin: 0 auto 1rem;
	border-radius: 14px;
	overflow: hidden;
	background-color: #0f172a;
	background-image: repeating-conic-gradient(rgba(255, 255, 255, 0.08) 0% 25%, transparent 0% 50%);
	background-size: 16px 16px;
	background-position: 50% 50%;
	border: 1px solid rgba(15, 23, 42, 0.3);
	box-shadow: inset 0 3px 12px rgba(0, 0, 0, 0.45);
	user-select: none;

	&.is-draggable {
		cursor: grab;
	}

	&.is-dragging {
		cursor: grabbing;
	}
}

.studio-crop-box {
	position: absolute;
	inset: 0;
	overflow: hidden;
	display: flex;
	align-items: center;
	justify-content: center;
	transition: border-radius 0.12s ease;
	box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.2), 0 6px 20px rgba(0, 0, 0, 0.4);
}

.studio-icon-img {
	width: 100% !important;
	height: 100% !important;
	max-width: none !important;
	max-height: none !important;
	object-fit: cover;
	pointer-events: none;
	display: block;
}

.drag-hint {
	position: absolute;
	bottom: 6px;
	left: 50%;
	transform: translateX(-50%);
	background: rgba(15, 23, 42, 0.85);
	color: #ffffff;
	font-size: 0.65rem;
	font-weight: 500;
	padding: 0.15rem 0.45rem;
	border-radius: 9999px;
	pointer-events: none;
	white-space: nowrap;
	backdrop-filter: blur(8px);
}

/* Controls */
.studio-controls-card {
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	border-radius: 10px;
	padding: 0.75rem;
	margin-bottom: 1rem;
}

.control-row {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	margin-bottom: 0.6rem;

	&:last-child {
		margin-bottom: 0;
	}
}

.control-label {
	width: 5.5rem;
	font-size: 0.75rem;
	font-weight: 600;
	color: #475569;
	display: flex;
	align-items: center;
	gap: 0.25rem;

	i {
		font-size: 0.95rem;
		color: #64748b;
	}
}

.studio-slider {
	flex: 1;
	accent-color: #2563eb;
	cursor: pointer;
	height: 4px;
}

.control-badge {
	width: 2.5rem;
	text-align: right;
	font-size: 0.7rem;
	font-weight: 700;
	color: #2563eb;
}

.preset-pills {
	display: flex;
	flex-wrap: wrap;
	gap: 0.35rem;
	margin-top: 0.6rem;
	padding-top: 0.6rem;
	border-top: 1px dashed #e2e8f0;
}

.preset-pill {
	border: 1px solid #cbd5e1;
	background: #ffffff;
	border-radius: 6px;
	padding: 0.2rem 0.45rem;
	font-size: 0.68rem;
	font-weight: 500;
	color: #475569;
	cursor: pointer;
	transition: all 0.15s ease;

	&:hover {
		border-color: #2563eb;
		color: #2563eb;
	}

	&.active {
		background: #2563eb;
		color: #ffffff;
		border-color: #2563eb;
		font-weight: 600;
	}
}

/* Miniature Desktop Preview */
.desktop-mini-preview {
	margin-top: auto;
	padding: 0.75rem;
	background: rgba(241, 245, 249, 0.7);
	border-radius: 10px;
	border: 1px solid #e2e8f0;
	display: flex;
	flex-direction: column;
	align-items: center;
	text-align: center;
}

.mini-card-icon {
	width: 44px;
	height: 44px;
	overflow: hidden;
	box-shadow: 0 4px 10px rgba(0, 0, 0, 0.15);
	margin-bottom: 0.35rem;
	display: flex;
	align-items: center;
	justify-content: center;
	background: #ffffff;

	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
}

.mini-card-title {
	font-size: 0.75rem;
	font-weight: 600;
	color: #1e293b;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	max-width: 100%;
}

/* Right Column: Main Content */
.studio-main {
	flex: 1 1 auto;
	padding: 1.25rem;
	overflow-y: auto;
}

.studio-card {
	background: #ffffff;
	border: 1px solid #e2e8f0;
	border-radius: 12px;
	padding: 1rem;
	box-shadow: 0 1px 3px rgba(0, 0, 0, 0.04);
}

.card-title-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 0.85rem;
}

.card-heading {
	font-size: 0.875rem;
	font-weight: 700;
	color: #0f172a;
}

.source-nav-tabs {
	display: inline-flex;
	border: 1px solid #e2e8f0;
	border-radius: 8px;
	background: #f8fafc;
	padding: 2px;
}

.nav-tab {
	border: none;
	background: transparent;
	padding: 0.3rem 0.7rem;
	font-size: 0.75rem;
	font-weight: 600;
	color: #64748b;
	border-radius: 6px;
	cursor: pointer;
	transition: all 0.12s ease;
	display: inline-flex;
	align-items: center;

	i {
		font-size: 0.95rem;
	}

	&:hover {
		color: #1e293b;
	}

	&.active {
		background: #ffffff;
		color: #2563eb;
		box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
	}
}

/* Store Grid */
.store-grid-wrapper {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(70px, 1fr));
	gap: 0.5rem;
	max-height: 230px;
	overflow-y: auto;
	padding: 0.25rem;
}

.store-icon-tile {
	display: flex;
	flex-direction: column;
	align-items: center;
	padding: 0.45rem 0.25rem;
	border-radius: 8px;
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	cursor: pointer;
	transition: all 0.15s ease;
	text-align: center;

	&:hover {
		border-color: #2563eb;
		transform: translateY(-2px);
		background: #ffffff;
		box-shadow: 0 4px 10px rgba(0, 0, 0, 0.08);
	}

	&.is-active {
		border-color: #2563eb;
		background: #eff6ff;
		box-shadow: 0 0 0 2px rgba(37, 99, 235, 0.25);
	}
}

.tile-icon {
	width: 38px;
	height: 38px;
	border-radius: 8px;
	object-fit: cover;
	margin-bottom: 0.25rem;
}

.tile-label {
	font-size: 0.65rem;
	font-weight: 500;
	color: #475569;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	width: 100%;
}

.empty-state {
	grid-column: 1 / -1;
	padding: 1.5rem;
	text-align: center;
	color: #94a3b8;
	font-size: 0.8125rem;
}

/* Upload Dropzone */
.upload-dropzone {
	border: 2px dashed #cbd5e1;
	border-radius: 10px;
	padding: 2rem 1.5rem;
	text-align: center;
	cursor: pointer;
	background: #f8fafc;
	transition: all 0.15s ease;

	&:hover {
		border-color: #2563eb;
		background: #eff6ff;
	}
}

.dropzone-icon {
	font-size: 2.25rem;
	color: #2563eb;
	margin-bottom: 0.4rem;
	display: block;
}

.dropzone-text {
	font-size: 0.875rem;
	font-weight: 600;
	color: #1e293b;
	margin-bottom: 0.2rem;
}

.dropzone-sub {
	font-size: 0.75rem;
	color: #64748b;
}

/* Badges Grid */
.badge-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(48px, 1fr));
	gap: 0.6rem;
	margin-bottom: 0.75rem;
}

.badge-tile {
	width: 48px;
	height: 48px;
	border-radius: 10px;
	border: none;
	cursor: pointer;
	display: flex;
	align-items: center;
	justify-content: center;
	color: #ffffff;
	font-size: 1.25rem;
	font-weight: 700;
	transition: transform 0.15s ease, box-shadow 0.15s ease;
	box-shadow: 0 2px 6px rgba(0, 0, 0, 0.15);

	&:hover {
		transform: scale(1.1);
		box-shadow: 0 6px 14px rgba(0, 0, 0, 0.25);
	}
}

.badge-hint {
	font-size: 0.75rem;
	color: #64748b;
}

/* Footer */
.app-studio-footer {
	flex-shrink: 0;
	padding: 0.75rem 1.25rem;
	border-top: 1px solid #e2e8f0;
	background: #ffffff;
	display: flex;
	align-items: center;
}

.btn-reset-overrides {
	border: none;
	background: transparent;
	font-size: 0.8125rem;
	font-weight: 600;
	color: #dc2626;
	cursor: pointer;
	padding: 0.35rem 0.5rem;
	border-radius: 6px;
	display: inline-flex;
	align-items: center;
	transition: background 0.12s ease;

	&:hover {
		background: #fee2e2;
	}
}
</style>
