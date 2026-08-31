<template>
	<div class="app-studio-root">
		<div class="studio-content-layout">
			<!-- LEFT COLUMN: Live Preview & Canvas Studio -->
			<div class="studio-left-pane">
				<div class="pane-section-header">
					<span class="pane-title">{{ $t('Icon Preview') }}</span>
					<button class="btn-link" type="button" @click="resetTransforms">
						<i class="mdi mdi-restore mr-1"></i>{{ $t('Reset') }}
					</button>
				</div>

				<!-- Interactive Viewport -->
				<div
					ref="viewport"
					class="studio-canvas"
					:class="{ 'is-draggable': iconZoom > 1, 'is-dragging': dragging }"
					@mousedown="startDrag"
					@touchstart="startDrag"
				>
					<div class="studio-crop-layer" :style="{ borderRadius: iconRadius + '%' }">
						<img
							ref="previewImg"
							:src="iconRaw || icon || fallbackIcon"
							:style="imgTransformStyle"
							draggable="false"
							class="studio-source-img"
							@load="onImageLoaded"
						/>
					</div>
					<div v-if="iconZoom > 1" class="canvas-drag-badge">
						<i class="mdi mdi-cursor-move mr-1"></i>{{ $t('Drag to reposition') }}
					</div>
				</div>

				<!-- Zoom & Roundness Controls -->
				<div class="studio-sliders-box">
					<!-- Zoom Slider -->
					<div class="slider-row">
						<div class="slider-label">
							<i class="mdi mdi-magnify-plus-outline"></i>
							<span>{{ $t('Zoom') }}</span>
						</div>
						<input
							v-model.number="iconZoom"
							max="3"
							min="1"
							step="0.02"
							type="range"
							class="range-slider"
						/>
						<span class="slider-val">{{ Math.round(iconZoom * 100) }}%</span>
					</div>

					<!-- Roundness Slider -->
					<div class="slider-row">
						<div class="slider-label">
							<i class="mdi mdi-rounded-corner"></i>
							<span>{{ $t('Corners') }}</span>
						</div>
						<input
							v-model.number="iconRadius"
							max="50"
							min="0"
							step="1"
							type="range"
							class="range-slider"
						/>
						<span class="slider-val">{{ iconRadius }}%</span>
					</div>

					<!-- Quick Roundness Presets -->
					<div class="preset-buttons-row">
						<button
							v-for="p in roundnessPresets"
							:key="p.value"
							type="button"
							class="btn-preset"
							:class="{ active: iconRadius === p.value }"
							@click="iconRadius = p.value"
						>
							{{ p.label }}
						</button>
					</div>
				</div>

				<!-- Miniature Desktop Representation -->
				<div class="mini-desktop-sample">
					<div class="mini-app-tile" :style="{ borderRadius: iconRadius + '%' }">
						<img :src="iconRaw || icon || fallbackIcon" :style="imgTransformStyle" draggable="false" />
					</div>
					<div class="mini-app-name">{{ displayName }}</div>
				</div>
			</div>

			<!-- RIGHT COLUMN: Metadata & Source Selection -->
			<div class="studio-right-pane">
				<!-- App Info Details -->
				<div class="pane-card mb-3">
					<div class="card-heading-row">
						<span class="card-title">{{ $t('Application Info') }}</span>
					</div>

					<div class="columns is-mobile is-variable is-2 mb-0">
						<div class="column" :class="{ 'is-6': showUrlField, 'is-12': !showUrlField }">
							<b-field :label="$t('Display Title')">
								<b-input
									v-model="name"
									:placeholder="originalName"
									size="is-small"
									icon="pencil-outline"
									expanded
								></b-input>
							</b-field>
						</div>
						<div v-if="showUrlField" class="column is-6">
							<b-field :label="$t('Web UI URL')">
								<b-input
									v-model="url"
									:placeholder="$t('http://... or /path')"
									size="is-small"
									icon="link-variant"
									expanded
								></b-input>
							</b-field>
						</div>
					</div>
				</div>

				<!-- Icon Source Picker -->
				<div class="pane-card is-flex-grow-1">
					<div class="card-heading-row">
						<span class="card-title">{{ $t('Choose Icon Source') }}</span>
						<div class="source-segmented-control">
							<button
								type="button"
								class="seg-btn"
								:class="{ active: iconTab === 'store' }"
								@click="iconTab = 'store'"
							>
								<i class="mdi mdi-shopping-outline mr-1"></i>{{ $t('App Store') }}
							</button>
							<button
								type="button"
								class="seg-btn"
								:class="{ active: iconTab === 'url' }"
								@click="iconTab = 'url'"
							>
								<i class="mdi mdi-link-variant mr-1"></i>{{ $t('URL') }}
							</button>
							<button
								type="button"
								class="seg-btn"
								:class="{ active: iconTab === 'upload' }"
								@click="iconTab = 'upload'"
							>
								<i class="mdi mdi-upload-outline mr-1"></i>{{ $t('Upload') }}
							</button>
							<button
								type="button"
								class="seg-btn"
								:class="{ active: iconTab === 'badges' }"
								@click="iconTab = 'badges'"
							>
								<i class="mdi mdi-palette-outline mr-1"></i>{{ $t('Monogram') }}
							</button>
						</div>
					</div>

					<!-- Tab 1: App Store Catalog -->
					<div v-if="iconTab === 'store'" class="tab-content-box">
						<b-input
							v-model="storeSearch"
							:placeholder="$t('Search App Store icons...')"
							icon="magnify"
							size="is-small"
							class="mb-2"
							clearable
						></b-input>

						<div class="store-icons-scroll-grid" :class="{ 'is-loading': loadingStoreIcons }">
							<div
								v-for="app in filteredStoreApps"
								:key="app.id || app.title"
								class="store-app-tile"
								:class="{ 'is-selected': isCurrentIcon(app.icon) }"
								:title="app.title"
								@click="selectStoreIcon(app)"
							>
								<img :src="app.icon" class="store-app-icon" :alt="app.title" loading="lazy" />
								<span class="store-app-title">{{ app.title }}</span>
							</div>
							<div v-if="!filteredStoreApps.length" class="empty-hint">
								{{ $t('No matching icons found') }}
							</div>
						</div>
					</div>

					<!-- Tab 2: URL Input -->
					<div v-else-if="iconTab === 'url'" class="tab-content-box p-3">
						<b-field :label="$t('Image URL (PNG, SVG, JPG, WebP)')">
							<b-input
								v-model="inputUrl"
								:placeholder="$t('https://example.com/logo.png')"
								size="is-small"
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
							class="mt-2"
							:loading="isCompressing"
							@click="applyCustomUrl"
						>
							<i class="mdi mdi-check mr-1"></i>{{ $t('Load Image') }}
						</b-button>
					</div>

					<!-- Tab 3: Upload Image File -->
					<div v-else-if="iconTab === 'upload'" class="tab-content-box p-3">
						<div class="upload-box" @click="$refs.iconFile.click()">
							<i class="mdi mdi-cloud-upload-outline upload-box-icon"></i>
							<div class="upload-box-text">{{ $t('Click to browse image file') }}</div>
							<div class="upload-box-sub">PNG, SVG, JPG, WebP</div>
						</div>
						<input
							ref="iconFile"
							accept="image/*"
							style="display: none"
							type="file"
							@change="handleIconFile"
						/>
					</div>

					<!-- Tab 4: Monogram Badge Generator -->
					<div v-else-if="iconTab === 'badges'" class="tab-content-box p-3">
						<div class="monogram-grid">
							<button
								v-for="(bg, idx) in badgeGradients"
								:key="idx"
								type="button"
								class="monogram-tile"
								:style="{ background: bg }"
								@click="generateBadgeIcon(bg)"
							>
								<span>{{ badgeLetter }}</span>
							</button>
						</div>
						<div class="monogram-hint">
							{{ $t('Generates a clean gradient monogram icon with the initial letter') }}
						</div>
					</div>
				</div>
			</div>
		</div>

		<!-- Window Footer -->
		<footer class="app-studio-footer-bar">
			<button
				v-if="override"
				type="button"
				class="btn-reset-danger"
				@click="resetAllOverrides"
			>
				<i class="mdi mdi-restore mr-1"></i>{{ $t('Reset to Defaults') }}
			</button>
			<div class="is-flex-grow-1"></div>
			<div class="footer-actions">
				<b-button size="is-small" rounded @click="$emit('close')">{{ $t('Cancel') }}</b-button>
				<b-button type="is-primary" size="is-small" rounded :loading="isSaving" @click="save">
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

const VIEWPORT_SIZE = 160
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
				{ label: '0%', value: 0 },
				{ label: '15%', value: 15 },
				{ label: '25%', value: 25 },
				{ label: '50%', value: 50 }
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
				// Keep fallback
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
.app-studio-root {
	height: 100%;
	display: flex;
	flex-direction: column;
	background: #f8fafc;
	color: #0f172a;
	user-select: none;
	overflow: hidden;
}

.studio-content-layout {
	flex: 1 1 auto;
	display: flex;
	overflow: hidden;
}

/* Left Pane: Preview Studio */
.studio-left-pane {
	width: 255px;
	flex-shrink: 0;
	padding: 1rem;
	background: #ffffff;
	border-right: 1px solid #e2e8f0;
	display: flex;
	flex-direction: column;
	overflow-y: auto;
}

.pane-section-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 0.65rem;
}

.pane-title {
	font-size: 0.8125rem;
	font-weight: 700;
	color: #0f172a;
	text-transform: uppercase;
	letter-spacing: 0.04em;
}

.btn-link {
	border: none;
	background: transparent;
	font-size: 0.72rem;
	font-weight: 600;
	color: #2563eb;
	cursor: pointer;
	padding: 0;

	&:hover {
		text-decoration: underline;
	}
}

/* Canvas Viewport */
.studio-canvas {
	position: relative;
	width: 160px;
	height: 160px;
	margin: 0 auto 0.75rem;
	border-radius: 14px;
	overflow: hidden;
	background-color: #0f172a;
	background-image: repeating-conic-gradient(rgba(255, 255, 255, 0.08) 0% 25%, transparent 0% 50%);
	background-size: 14px 14px;
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

.studio-crop-layer {
	position: absolute;
	inset: 0;
	overflow: hidden;
	display: flex;
	align-items: center;
	justify-content: center;
	transition: border-radius 0.12s ease;
	box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.2), 0 4px 16px rgba(0, 0, 0, 0.35);
}

.studio-source-img {
	width: 100% !important;
	height: 100% !important;
	max-width: none !important;
	max-height: none !important;
	object-fit: cover;
	pointer-events: none;
	display: block;
}

.canvas-drag-badge {
	position: absolute;
	bottom: 5px;
	left: 50%;
	transform: translateX(-50%);
	background: rgba(15, 23, 42, 0.88);
	color: #ffffff;
	font-size: 0.625rem;
	font-weight: 500;
	padding: 0.15rem 0.4rem;
	border-radius: 9999px;
	pointer-events: none;
	white-space: nowrap;
	backdrop-filter: blur(6px);
}

/* Sliders Box */
.studio-sliders-box {
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	border-radius: 8px;
	padding: 0.6rem;
	margin-bottom: 0.75rem;
}

.slider-row {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	margin-bottom: 0.45rem;

	&:last-of-type {
		margin-bottom: 0;
	}
}

.slider-label {
	width: 4.8rem;
	font-size: 0.72rem;
	font-weight: 600;
	color: #475569;
	display: flex;
	align-items: center;
	gap: 0.2rem;

	i {
		font-size: 0.875rem;
		color: #64748b;
	}
}

.range-slider {
	flex: 1;
	accent-color: #2563eb;
	cursor: pointer;
	height: 4px;
}

.slider-val {
	width: 2.2rem;
	text-align: right;
	font-size: 0.68rem;
	font-weight: 700;
	color: #2563eb;
}

.preset-buttons-row {
	display: flex;
	gap: 0.25rem;
	margin-top: 0.5rem;
	padding-top: 0.5rem;
	border-top: 1px dashed #e2e8f0;
}

.btn-preset {
	flex: 1;
	border: 1px solid #cbd5e1;
	background: #ffffff;
	border-radius: 5px;
	padding: 0.15rem 0.2rem;
	font-size: 0.65rem;
	font-weight: 600;
	color: #475569;
	cursor: pointer;
	transition: all 0.12s ease;
	text-align: center;

	&:hover {
		border-color: #2563eb;
		color: #2563eb;
	}

	&.active {
		background: #2563eb;
		color: #ffffff;
		border-color: #2563eb;
	}
}

/* Miniature Desktop Sample */
.mini-desktop-sample {
	margin-top: auto;
	padding: 0.5rem;
	background: #f1f5f9;
	border-radius: 8px;
	border: 1px solid #e2e8f0;
	display: flex;
	flex-direction: column;
	align-items: center;
	text-align: center;
}

.mini-app-tile {
	width: 36px;
	height: 36px;
	overflow: hidden;
	box-shadow: 0 2px 6px rgba(0, 0, 0, 0.15);
	margin-bottom: 0.25rem;
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

.mini-app-name {
	font-size: 0.7rem;
	font-weight: 600;
	color: #1e293b;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	max-width: 100%;
}

/* Right Pane: Main Details */
.studio-right-pane {
	flex: 1 1 auto;
	padding: 1rem;
	overflow-y: auto;
	display: flex;
	flex-direction: column;
}

.pane-card {
	background: #ffffff;
	border: 1px solid #e2e8f0;
	border-radius: 10px;
	padding: 0.85rem;
	box-shadow: 0 1px 2px rgba(0, 0, 0, 0.03);
}

.card-heading-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 0.65rem;
}

.card-title {
	font-size: 0.8125rem;
	font-weight: 700;
	color: #0f172a;
}

.source-segmented-control {
	display: inline-flex;
	border: 1px solid #e2e8f0;
	border-radius: 6px;
	background: #f8fafc;
	padding: 2px;
}

.seg-btn {
	border: none;
	background: transparent;
	padding: 0.25rem 0.55rem;
	font-size: 0.72rem;
	font-weight: 600;
	color: #64748b;
	border-radius: 4px;
	cursor: pointer;
	transition: all 0.12s ease;
	display: inline-flex;
	align-items: center;

	i {
		font-size: 0.85rem;
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

.tab-content-box {
	display: flex;
	flex-direction: column;
	min-height: 180px;
}

/* Store Grid */
.store-icons-scroll-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(64px, 1fr));
	gap: 0.45rem;
	max-height: 190px;
	overflow-y: auto;
	padding: 0.2rem;
}

.store-app-tile {
	display: flex;
	flex-direction: column;
	align-items: center;
	padding: 0.35rem 0.2rem;
	border-radius: 6px;
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	cursor: pointer;
	transition: all 0.12s ease;
	text-align: center;

	&:hover {
		border-color: #2563eb;
		transform: translateY(-2px);
		background: #ffffff;
		box-shadow: 0 3px 8px rgba(0, 0, 0, 0.08);
	}

	&.is-selected {
		border-color: #2563eb;
		background: #eff6ff;
		box-shadow: 0 0 0 2px rgba(37, 99, 235, 0.25);
	}
}

.store-app-icon {
	width: 32px;
	height: 32px;
	border-radius: 6px;
	object-fit: cover;
	margin-bottom: 0.2rem;
}

.store-app-title {
	font-size: 0.625rem;
	font-weight: 500;
	color: #475569;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	width: 100%;
}

.empty-hint {
	grid-column: 1 / -1;
	padding: 1rem;
	text-align: center;
	color: #94a3b8;
	font-size: 0.75rem;
}

/* Upload Box */
.upload-box {
	border: 2px dashed #cbd5e1;
	border-radius: 8px;
	padding: 1.5rem 1rem;
	text-align: center;
	cursor: pointer;
	background: #f8fafc;
	transition: all 0.15s ease;

	&:hover {
		border-color: #2563eb;
		background: #eff6ff;
	}
}

.upload-box-icon {
	font-size: 2rem;
	color: #2563eb;
	margin-bottom: 0.3rem;
	display: block;
}

.upload-box-text {
	font-size: 0.8125rem;
	font-weight: 600;
	color: #1e293b;
}

.upload-box-sub {
	font-size: 0.7rem;
	color: #64748b;
}

/* Monogram Grid */
.monogram-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(44px, 1fr));
	gap: 0.5rem;
	margin-bottom: 0.6rem;
}

.monogram-tile {
	width: 44px;
	height: 44px;
	border-radius: 8px;
	border: none;
	cursor: pointer;
	display: flex;
	align-items: center;
	justify-content: center;
	color: #ffffff;
	font-size: 1.15rem;
	font-weight: 700;
	transition: transform 0.12s ease;
	box-shadow: 0 2px 5px rgba(0, 0, 0, 0.12);

	&:hover {
		transform: scale(1.08);
	}
}

.monogram-hint {
	font-size: 0.7rem;
	color: #64748b;
}

/* Footer Bar */
.app-studio-footer-bar {
	flex-shrink: 0;
	padding: 0.65rem 1rem;
	border-top: 1px solid #e2e8f0;
	background: #ffffff;
	display: flex;
	align-items: center;
}

.btn-reset-danger {
	border: none;
	background: transparent;
	font-size: 0.75rem;
	font-weight: 600;
	color: #dc2626;
	cursor: pointer;
	padding: 0.3rem 0.45rem;
	border-radius: 5px;
	display: inline-flex;
	align-items: center;
	transition: background 0.12s ease;

	&:hover {
		background: #fee2e2;
	}
}

.footer-actions {
	display: flex;
	gap: 0.5rem;
}
</style>
