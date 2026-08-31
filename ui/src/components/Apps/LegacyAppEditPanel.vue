<template>
	<div class="app-editor-container">
		<!-- Top Hero: App Identity & Live Preview -->
		<div class="editor-hero-section">
			<div
				class="hero-icon-preview dark-checkerboard"
				:style="{ borderRadius: iconRadius + '%' }"
				:title="$t('Click to fine-tune crop and zoom')"
				@click="iconTab = 'studio'"
			>
				<div class="hero-crop-box" :style="{ borderRadius: iconRadius + '%' }">
					<img
						ref="heroPreviewImg"
						:src="iconRaw || icon || fallbackIcon"
						:style="heroImgTransformStyle"
						class="hero-img"
						draggable="false"
					/>
				</div>
				<div class="hero-edit-overlay">
					<i class="mdi mdi-crop-free"></i>
					<span>{{ $t('Crop') }}</span>
				</div>
			</div>

			<div class="hero-form-fields">
				<div class="field-row">
					<label class="field-lbl">{{ $t('Application Name') }}</label>
					<div class="input-with-reset">
						<b-input
							v-model="name"
							:placeholder="originalName"
							size="is-small"
							icon="pencil-outline"
							expanded
						></b-input>
						<button
							v-if="name && name !== originalName"
							class="btn-clear-field"
							type="button"
							:title="$t('Reset name to default')"
							@click="name = ''"
						>
							<i class="mdi mdi-close-circle"></i>
						</button>
					</div>
				</div>

				<div v-if="showUrlField" class="field-row mt-2">
					<div class="is-flex is-align-items-center is-justify-content-between mb-1">
						<label class="field-lbl mb-0">{{ $t('Web UI URL') }}</label>
						<a v-if="url" :href="url" target="_blank" class="test-url-link">
							<i class="mdi mdi-open-in-new mr-1"></i>{{ $t('Test URL') }}
						</a>
					</div>
					<b-input
						v-model="url"
						:placeholder="urlPlaceholder"
						size="is-small"
						icon="link-variant"
						expanded
					></b-input>

					<!-- URL Suggestions from Docker Container / Host -->
					<div v-if="urlSuggestions.length" class="url-suggestions-wrap mt-1">
						<span class="sugg-hint">
							<i class="mdi mdi-lightbulb-on-outline mr-1"></i>{{ $t('Detected:') }}
						</span>
						<div class="sugg-pills">
							<button
								v-for="sugg in urlSuggestions"
								:key="sugg"
								type="button"
								class="sugg-pill"
								:class="{ 'is-active': url === sugg }"
								:title="$t('Click to apply URL')"
								@click="url = sugg"
							>
								{{ sugg }}
							</button>
						</div>
					</div>
				</div>
			</div>
		</div>

		<!-- Navigation Tabs -->
		<div class="editor-tabs-bar">
			<button
				type="button"
				class="tab-btn"
				:class="{ active: iconTab === 'store' }"
				@click="iconTab = 'store'"
			>
				<i class="mdi mdi-shopping-outline mr-1"></i>{{ $t('App Store Icons') }}
			</button>
			<button
				type="button"
				class="tab-btn"
				:class="{ active: iconTab === 'custom' }"
				@click="iconTab = 'custom'"
			>
				<i class="mdi mdi-image-plus-outline mr-1"></i>{{ $t('Upload / URL') }}
			</button>
			<button
				type="button"
				class="tab-btn"
				:class="{ active: iconTab === 'studio' }"
				@click="iconTab = 'studio'"
			>
				<i class="mdi mdi-crop-free mr-1"></i>{{ $t('Crop & Fine-Tune') }}
			</button>
			<button
				type="button"
				class="tab-btn"
				:class="{ active: iconTab === 'badges' }"
				@click="iconTab = 'badges'"
			>
				<i class="mdi mdi-palette-outline mr-1"></i>{{ $t('Monogram') }}
			</button>
		</div>

		<!-- Tab Workspace Bodies -->
		<div class="editor-tab-body">
			<!-- 1. App Store Icon Catalog -->
			<div v-if="iconTab === 'store'" class="tab-pane-full">
				<div class="search-bar-row">
					<b-input
						v-model="storeSearch"
						:placeholder="$t('Search icons (Nextcloud, Plex, Jellyfin, AdGuard, etc.)...')"
						icon="magnify"
						size="is-small"
						expanded
						clearable
					></b-input>
				</div>

				<div class="store-catalog-grid" :class="{ 'is-loading': loadingStoreIcons }">
					<div
						v-for="app in filteredStoreApps"
						:key="app.id || app.title"
						class="catalog-item-card"
						:class="{ 'is-active': isCurrentIcon(app.icon) }"
						:title="app.title"
						@click="selectStoreIcon(app)"
					>
						<div class="catalog-thumb-box dark-checkerboard">
							<img :src="app.icon" class="catalog-thumb" :alt="app.title" loading="lazy" />
							<div v-if="isCurrentIcon(app.icon)" class="check-badge">
								<i class="mdi mdi-check"></i>
							</div>
						</div>
						<span class="catalog-label">{{ app.title }}</span>
					</div>

					<div v-if="!filteredStoreApps.length" class="empty-results-box">
						<i class="mdi mdi-file-search-outline"></i>
						<span>{{ $t('No matching icons found for') }} "{{ storeSearch }}"</span>
					</div>
				</div>
			</div>

			<!-- 2. Combined Custom Image (Upload + URL) -->
			<div v-else-if="iconTab === 'custom'" class="tab-pane-centered">
				<div class="custom-image-card">
					<!-- Upload Dropzone Section -->
					<div class="dropzone-area" @click="$refs.iconFile.click()">
						<div class="dropzone-icon-circle">
							<i class="mdi mdi-cloud-upload-outline"></i>
						</div>
						<div class="dropzone-title">{{ $t('Upload Image File') }}</div>
						<div class="dropzone-desc">{{ $t('Drag and drop an image file here, or click to browse') }}</div>
						<div class="dropzone-badge">PNG, SVG, JPG, WebP</div>
					</div>
					<input
						ref="iconFile"
						accept="image/*"
						style="display: none"
						type="file"
						@change="handleIconFile"
					/>

					<!-- Separator -->
					<div class="custom-image-divider">
						<span>{{ $t('OR USE IMAGE URL') }}</span>
					</div>

					<!-- URL Input Section -->
					<div class="url-sub-row">
						<b-input
							v-model="inputUrl"
							:placeholder="$t('https://example.com/icon.png')"
							icon="link"
							size="is-small"
							expanded
							@keyup.enter.native="applyCustomUrl"
						></b-input>
						<b-button
							type="is-primary"
							size="is-small"
							:loading="isCompressing"
							@click="applyCustomUrl"
						>
							<i class="mdi mdi-download mr-1"></i>{{ $t('Load') }}
						</b-button>
					</div>
				</div>
			</div>

			<!-- 3. Interactive Crop, Zoom & Roundness Studio -->
			<div v-else-if="iconTab === 'studio'" class="tab-pane-studio">
				<!-- Left: Large Interactive Canvas -->
				<div class="studio-canvas-col">
					<div
						ref="viewport"
						class="interactive-viewport dark-checkerboard"
						:class="{ 'is-draggable': iconZoom > 1, 'is-dragging': dragging }"
						@mousedown="startDrag"
						@touchstart="startDrag"
					>
						<div class="canvas-crop-layer" :style="{ borderRadius: iconRadius + '%' }">
							<img
								ref="canvasImg"
								:src="iconRaw || icon || fallbackIcon"
								:style="imgTransformStyle"
								draggable="false"
								class="canvas-source-img"
								@load="onImageLoaded"
							/>
						</div>
						<div v-if="iconZoom > 1" class="canvas-pan-pill">
							<i class="mdi mdi-cursor-move mr-1"></i>{{ $t('Drag image to reposition') }}
						</div>
					</div>
					<div class="canvas-caption">{{ $t('Use sliders on the right to adjust zoom and corner curvature') }}</div>
				</div>

				<!-- Right: Controls -->
				<div class="studio-controls-col">
					<!-- Zoom Slider -->
					<div class="control-box">
						<div class="control-header">
							<div class="control-title">
								<i class="mdi mdi-magnify-plus-outline mr-1"></i>
								<span>{{ $t('Zoom Scale') }}</span>
							</div>
							<span class="control-val">{{ Math.round(iconZoom * 100) }}%</span>
						</div>
						<div class="slider-interactive-wrap">
							<button class="btn-step" type="button" :disabled="iconZoom <= 1" @click="stepZoom(-0.1)">
								<i class="mdi mdi-minus"></i>
							</button>
							<input
								v-model.number="iconZoom"
								max="3"
								min="1"
								step="0.02"
								type="range"
								class="studio-range-slider"
							/>
							<button class="btn-step" type="button" :disabled="iconZoom >= 3" @click="stepZoom(0.1)">
								<i class="mdi mdi-plus"></i>
							</button>
						</div>
					</div>

					<!-- Roundness Slider -->
					<div class="control-box mt-3">
						<div class="control-header">
							<div class="control-title">
								<i class="mdi mdi-rounded-corner mr-1"></i>
								<span>{{ $t('Corner Roundness') }}</span>
							</div>
							<span class="control-val">{{ iconRadius }}%</span>
						</div>
						<input
							v-model.number="iconRadius"
							max="50"
							min="0"
							step="1"
							type="range"
							class="studio-range-slider"
						/>

						<!-- Preset Pills -->
						<div class="corner-preset-pills mt-2">
							<button
								v-for="p in roundnessPresets"
								:key="p.value"
								type="button"
								class="pill-btn"
								:class="{ active: iconRadius === p.value }"
								@click="iconRadius = p.value"
							>
								{{ p.label }}
							</button>
						</div>
					</div>

					<div class="studio-actions-row mt-3">
						<button class="btn-reset-transforms" type="button" @click="resetTransforms">
							<i class="mdi mdi-restore mr-1"></i>{{ $t('Reset Zoom & Crop') }}
						</button>
					</div>
				</div>
			</div>

			<!-- 4. Monogram Badges -->
			<div v-else-if="iconTab === 'badges'" class="tab-pane-centered">
				<div class="monogram-picker-card">
					<div class="monogram-desc mb-3">
						{{ $t('Select a gradient color to generate a modern monogram badge for') }} <strong>{{ displayName }}</strong>
					</div>
					<div class="monogram-tiles-grid">
						<button
							v-for="(bg, idx) in badgeGradients"
							:key="idx"
							type="button"
							class="monogram-color-tile"
							:style="{ background: bg }"
							@click="generateBadgeIcon(bg)"
						>
							<span>{{ badgeLetter }}</span>
						</button>
					</div>
				</div>
			</div>
		</div>

		<!-- Footer Actions -->
		<footer class="editor-footer-bar">
			<button
				v-if="override"
				type="button"
				class="btn-danger-link"
				@click="resetAllOverrides"
			>
				<i class="mdi mdi-restore mr-1"></i>{{ $t('Reset to Defaults') }}
			</button>
			<div class="is-flex-grow-1"></div>
			<div class="footer-btn-group">
				<b-button rounded @click="$emit('close')">{{ $t('Cancel') }}</b-button>
				<b-button type="is-primary" rounded :loading="isSaving" @click="save">
					<i class="mdi mdi-check mr-1"></i>{{ $t('Save Changes') }}
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
			urlSuggestions: [],
			icon: '',
			iconRaw: '',
			iconZoom: 1,
			iconPanX: 0,
			iconPanY: 0,
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
		currentHost() {
			return window.location.hostname || 'localhost'
		},
		urlPlaceholder() {
			return `http://${this.currentHost}:8080`
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
			const maxPan = (this.iconZoom - 1) * (VIEWPORT_SIZE / 2)
			const pxX = this.iconPanX * maxPan
			const pxY = this.iconPanY * maxPan
			return {
				transform: `translate3d(${pxX}px, ${pxY}px, 0) scale(${this.iconZoom})`,
				transformOrigin: 'center center'
			}
		},
		heroImgTransformStyle() {
			const maxPan = (this.iconZoom - 1) * 30
			const pxX = this.iconPanX * maxPan
			const pxY = this.iconPanY * maxPan
			return {
				transform: `translate3d(${pxX}px, ${pxY}px, 0) scale(${this.iconZoom})`,
				transformOrigin: 'center center'
			}
		}
	},
	created() {
		if (this.override) {
			this.name = this.override.title || ''
			this.url = this.override.url || ''
			this.icon = this.override.icon || ''
			this.iconRaw = this.override.iconRaw || this.override.icon || this.item.icon || ''
			this.iconZoom = this.override.iconZoom !== undefined ? this.override.iconZoom : 1
			this.iconRadius = this.override.iconRadius || 0
			if (this.override.iconPanX !== undefined) {
				this.iconPanX = this.override.iconPanX
				this.iconPanY = this.override.iconPanY || 0
			} else if (this.override.iconOffsetX) {
				const maxPan = Math.max(1, (this.iconZoom - 1) * (VIEWPORT_SIZE / 2))
				this.iconPanX = Math.min(1, Math.max(-1, this.override.iconOffsetX / maxPan))
				this.iconPanY = Math.min(1, Math.max(-1, (this.override.iconOffsetY || 0) / maxPan))
			}
		} else {
			this.icon = this.item.icon || ''
			this.iconRaw = this.item.icon || ''
			this.iconZoom = 1
			this.iconPanX = 0
			this.iconPanY = 0
			this.iconRadius = this.item.iconRadius || 0
		}
		if (this.iconRaw && !this.iconRaw.startsWith('data:')) {
			this.inputUrl = this.iconRaw
		}
		this.fetchStoreIcons()
		this.fetchContainerSuggestions()
	},
	beforeDestroy() {
		this.stopDrag()
	},
	methods: {
		isCurrentIcon(src) {
			return (this.iconRaw || this.icon) === src
		},
		async fetchContainerSuggestions() {
			const suggestions = new Set()
			const host = window.location.hostname || 'localhost'

			// 1. Direct item properties
			if (this.item.port) {
				suggestions.add(`http://${host}:${this.item.port}`)
			}
			if (this.item.port_map) {
				suggestions.add(`http://${host}:${this.item.port_map}`)
			}

			// 2. Ports array on item
			if (Array.isArray(this.item.ports)) {
				this.item.ports.forEach(p => {
					if (typeof p === 'string') {
						const match = p.match(/^(\d+):/)
						if (match) suggestions.add(`http://${host}:${match[1]}`)
					} else if (p && p.host) {
						suggestions.add(`http://${host}:${p.host}`)
					} else if (p && p.published) {
						suggestions.add(`http://${host}:${p.published}`)
					}
				})
			}

			// 3. Query Compose App Details if available
			if (this.$openAPI?.appManagement?.compose?.myComposeApp && this.item.name) {
				try {
					const res = await this.$openAPI.appManagement.compose.myComposeApp(this.item.name)
					const data = res.data?.data
					if (data) {
						if (data.store_info?.port_map) {
							const scheme = data.store_info.scheme || 'http'
							const index = data.store_info.index || ''
							suggestions.add(`${scheme}://${host}:${data.store_info.port_map}${index}`)
						}
						const services = data.compose?.services || {}
						Object.values(services).forEach(svc => {
							if (Array.isArray(svc.ports)) {
								svc.ports.forEach(p => {
									if (typeof p === 'string') {
										const match = p.match(/^(\d+):/)
										if (match) suggestions.add(`http://${host}:${match[1]}`)
									} else if (typeof p === 'number') {
										suggestions.add(`http://${host}:${p}`)
									} else if (p && p.published) {
										suggestions.add(`http://${host}:${p.published}`)
									}
								})
							}
						})
					}
				} catch (e) {
					// Fallback
				}
			}

			// 4. Default common ports if none found for container
			if (!suggestions.size && this.showUrlField) {
				suggestions.add(`http://${host}`)
				suggestions.add(`http://${host}:8080`)
			}

			this.urlSuggestions = Array.from(suggestions).filter(Boolean)
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
			this.iconPanX = 0
			this.iconPanY = 0
		},

		applyCustomUrl() {
			if (!this.inputUrl.trim()) return
			this.icon = this.inputUrl.trim()
			this.iconRaw = this.inputUrl.trim()
			this.iconZoom = 1
			this.iconPanX = 0
			this.iconPanY = 0
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
			this.iconPanX = 0
			this.iconPanY = 0
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
					this.iconPanX = 0
					this.iconPanY = 0
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
			// Ready
		},

		stepZoom(delta) {
			const target = Math.max(1, Math.min(3, Math.round((this.iconZoom + delta) * 10) / 10))
			this.iconZoom = target
		},

		resetTransforms() {
			this.iconZoom = 1
			this.iconPanX = 0
			this.iconPanY = 0
			this.iconRadius = 0
		},

		startDrag(e) {
			if (this.iconZoom <= 1) return
			const point = e.touches ? e.touches[0] : e
			this.dragging = true
			this.dragStart = {
				x: point.clientX,
				y: point.clientY,
				panX: this.iconPanX,
				panY: this.iconPanY
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
			const maxPanPx = Math.max(1, (this.iconZoom - 1) * (VIEWPORT_SIZE / 2))
			this.iconPanX = Math.min(1, Math.max(-1, this.dragStart.panX + dx / maxPanPx))
			this.iconPanY = Math.min(1, Math.max(-1, this.dragStart.panY + dy / maxPanPx))
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
				const img = this.$refs.canvasImg || this.$refs.heroPreviewImg
				if (!img) return this.iconRaw || this.icon
				const maxPan = (this.iconZoom - 1) * (OUTPUT_SIZE / 2)
				const pxX = this.iconPanX * maxPan
				const pxY = this.iconPanY * maxPan

				ctx.save()
				ctx.translate(OUTPUT_SIZE / 2, OUTPUT_SIZE / 2)
				ctx.translate(pxX, pxY)
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
				iconPanX: this.iconPanX,
				iconPanY: this.iconPanY,
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
.app-editor-container {
	height: 100%;
	display: flex;
	flex-direction: column;
	background: #f8fafc;
	color: #0f172a;
	user-select: none;
	overflow: hidden;
}

/* Dark Authentic Transparency Checkerboard */
.dark-checkerboard {
	background-color: #1e293b;
	background-image: repeating-conic-gradient(#0f172a 0% 25%, #1e293b 0% 50%);
	background-size: 14px 14px;
	background-position: 0 0;
}

/* Hero Section */
.editor-hero-section {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: 1rem;
	padding: 0.85rem 1.25rem;
	background: #ffffff;
	border-bottom: 1px solid #e2e8f0;
}

.hero-icon-preview {
	position: relative;
	width: 60px;
	height: 60px;
	min-width: 60px;
	min-height: 60px;
	flex-shrink: 0;
	overflow: hidden;
	border: 1px solid #cbd5e1;
	box-shadow: 0 3px 10px rgba(0, 0, 0, 0.15);
	cursor: pointer;
	transition: all 0.15s ease;

	.hero-crop-box {
		position: absolute;
		inset: 0;
		overflow: hidden;
		display: flex;
		align-items: center;
		justify-content: center;
	}

	.hero-img {
		width: 100% !important;
		height: 100% !important;
		max-width: none !important;
		max-height: none !important;
		object-fit: cover;
		display: block;
		pointer-events: none;
	}

	.hero-edit-overlay {
		position: absolute;
		inset: 0;
		background: rgba(15, 23, 42, 0.75);
		color: #ffffff;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		font-size: 0.65rem;
		font-weight: 600;
		opacity: 0;
		transition: opacity 0.15s ease;

		i {
			font-size: 1.1rem;
		}
	}

	&:hover {
		border-color: #2563eb;
		transform: scale(1.04);

		.hero-edit-overlay {
			opacity: 1;
		}
	}
}

.hero-form-fields {
	flex: 1 1 auto;
	min-width: 0;
}

.field-row {
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
}

.field-lbl {
	font-size: 0.72rem;
	font-weight: 600;
	color: #475569;
}

.input-with-reset {
	position: relative;
	display: flex;
	align-items: center;

	.btn-clear-field {
		position: absolute;
		right: 8px;
		background: transparent;
		border: none;
		color: #94a3b8;
		cursor: pointer;
		font-size: 1rem;
		padding: 0;
		line-height: 1;

		&:hover {
			color: #475569;
		}
	}
}

.url-suggestions-wrap {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: 0.35rem;
}

.sugg-hint {
	font-size: 0.68rem;
	font-weight: 600;
	color: #64748b;
	display: inline-flex;
	align-items: center;

	i {
		color: #eab308;
	}
}

.sugg-pills {
	display: flex;
	flex-wrap: wrap;
	gap: 0.25rem;
}

.sugg-pill {
	border: 1px solid #cbd5e1;
	background: #f8fafc;
	border-radius: 4px;
	padding: 0.12rem 0.4rem;
	font-size: 0.65rem;
	font-family: monospace;
	font-weight: 600;
	color: #2563eb;
	cursor: pointer;
	transition: all 0.12s ease;

	&:hover {
		background: #eff6ff;
		border-color: #2563eb;
	}

	&.is-active {
		background: #2563eb;
		color: #ffffff;
		border-color: #2563eb;
	}
}

.test-url-link {
	font-size: 0.68rem;
	font-weight: 600;
	color: #2563eb;
	display: inline-flex;
	align-items: center;

	&:hover {
		text-decoration: underline;
	}
}

/* Tabs Bar */
.editor-tabs-bar {
	flex-shrink: 0;
	display: flex;
	gap: 0.35rem;
	padding: 0.45rem 1.25rem;
	background: #f1f5f9;
	border-bottom: 1px solid #e2e8f0;
	overflow-x: auto;
}

.tab-btn {
	border: 1px solid transparent;
	background: transparent;
	padding: 0.3rem 0.65rem;
	border-radius: 6px;
	font-size: 0.75rem;
	font-weight: 600;
	color: #64748b;
	cursor: pointer;
	transition: all 0.12s ease;
	display: inline-flex;
	align-items: center;
	white-space: nowrap;

	i {
		font-size: 0.9rem;
	}

	&:hover {
		color: #1e293b;
		background: rgba(255, 255, 255, 0.6);
	}

	&.active {
		background: #ffffff;
		color: #2563eb;
		border-color: #cbd5e1;
		box-shadow: 0 1px 3px rgba(0, 0, 0, 0.05);
	}
}

/* Workspace Content */
.editor-tab-body {
	flex: 1 1 auto;
	overflow-y: auto;
	display: flex;
	background: #ffffff;
}

/* Tab 1: App Store Catalog */
.tab-pane-full {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	padding: 0.75rem 1.25rem;
	overflow: hidden;
}

.search-bar-row {
	flex-shrink: 0;
	margin-bottom: 0.5rem;
}

.store-catalog-grid {
	flex: 1 1 auto;
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(72px, 1fr));
	gap: 0.5rem;
	overflow-y: auto;
	padding: 0.2rem 0.1rem;
}

.catalog-item-card {
	display: flex;
	flex-direction: column;
	align-items: center;
	padding: 0.45rem 0.3rem 0.35rem;
	border-radius: 8px;
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	cursor: pointer;
	transition: all 0.15s ease;
	text-align: center;

	.catalog-thumb-box {
		position: relative;
		width: 38px;
		height: 38px;
		border-radius: 8px;
		overflow: hidden;
		margin-bottom: 0.25rem;

		.catalog-thumb {
			width: 100%;
			height: 100%;
			object-fit: cover;
		}

		.check-badge {
			position: absolute;
			top: -2px;
			right: -2px;
			width: 15px;
			height: 15px;
			border-radius: 50%;
			background: #2563eb;
			color: #ffffff;
			display: flex;
			align-items: center;
			justify-content: center;
			font-size: 0.6rem;
			box-shadow: 0 2px 4px rgba(0, 0, 0, 0.2);
		}
	}

	.catalog-label {
		font-size: 0.65rem;
		font-weight: 500;
		color: #334155;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		width: 100%;
	}

	&:hover {
		border-color: #2563eb;
		background: #ffffff;
		transform: translateY(-2px);
		box-shadow: 0 4px 10px rgba(0, 0, 0, 0.08);
	}

	&.is-active {
		border-color: #2563eb;
		background: #eff6ff;
		box-shadow: 0 0 0 2px rgba(37, 99, 235, 0.25);
	}
}

.empty-results-box {
	grid-column: 1 / -1;
	padding: 2rem 1rem;
	text-align: center;
	color: #94a3b8;
	font-size: 0.8rem;
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: 0.3rem;

	i {
		font-size: 1.75rem;
	}
}

/* Tab 2: Combined Custom Image (Upload + URL) */
.custom-image-card {
	width: 100%;
	max-width: 440px;
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	border-radius: 12px;
	padding: 1.25rem 1.5rem;
	display: flex;
	flex-direction: column;
}

.dropzone-area {
	border: 2px dashed #cbd5e1;
	border-radius: 10px;
	padding: 1.25rem 1rem;
	text-align: center;
	cursor: pointer;
	background: #ffffff;
	transition: all 0.15s ease;

	.dropzone-icon-circle {
		width: 44px;
		height: 44px;
		border-radius: 50%;
		background: #eff6ff;
		color: #2563eb;
		font-size: 1.4rem;
		display: flex;
		align-items: center;
		justify-content: center;
		margin: 0 auto 0.4rem;
	}

	.dropzone-title {
		font-size: 0.85rem;
		font-weight: 700;
		color: #0f172a;
		margin-bottom: 0.15rem;
	}

	.dropzone-desc {
		font-size: 0.72rem;
		color: #64748b;
		margin-bottom: 0.5rem;
	}

	.dropzone-badge {
		display: inline-block;
		font-size: 0.65rem;
		font-weight: 600;
		color: #475569;
		background: #e2e8f0;
		padding: 0.1rem 0.45rem;
		border-radius: 9999px;
	}

	&:hover {
		border-color: #2563eb;
		background: #eff6ff;
	}
}

.custom-image-divider {
	display: flex;
	align-items: center;
	text-align: center;
	margin: 1rem 0;

	&::before,
	&::after {
		content: '';
		flex: 1;
		border-bottom: 1px solid #e2e8f0;
	}

	span {
		padding: 0 0.75rem;
		font-size: 0.68rem;
		font-weight: 700;
		color: #94a3b8;
		letter-spacing: 0.05em;
	}
}

.url-sub-row {
	display: flex;
	align-items: center;
	gap: 0.5rem;
}

/* Tab 3: Crop Studio */
.tab-pane-studio {
	flex: 1 1 auto;
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	justify-content: center;
	padding: 1rem 1.25rem;
	gap: 1.25rem;
	overflow-y: auto;
}

.studio-canvas-col {
	flex-shrink: 0;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
}

.interactive-viewport {
	position: relative;
	width: 160px;
	height: 160px;
	border-radius: 14px;
	overflow: hidden;
	border: 1px solid rgba(15, 23, 42, 0.4);
	box-shadow: inset 0 3px 12px rgba(0, 0, 0, 0.45);
	user-select: none;

	&.is-draggable {
		cursor: grab;
	}

	&.is-dragging {
		cursor: grabbing;
	}
}

.canvas-crop-layer {
	position: absolute;
	inset: 0;
	overflow: hidden;
	display: flex;
	align-items: center;
	justify-content: center;
	transition: border-radius 0.12s ease;
	box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.25), 0 4px 16px rgba(0, 0, 0, 0.35);
}

.canvas-source-img {
	width: 100% !important;
	height: 100% !important;
	max-width: none !important;
	max-height: none !important;
	object-fit: cover;
	pointer-events: none;
	display: block;
}

.canvas-pan-pill {
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

.canvas-caption {
	font-size: 0.68rem;
	color: #64748b;
	margin-top: 0.5rem;
	text-align: center;
	max-width: 170px;
}

.studio-controls-col {
	flex: 1 1 240px;
	max-width: 320px;
	display: flex;
	flex-direction: column;
	justify-content: center;
}

.control-box {
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	border-radius: 10px;
	padding: 0.75rem 0.85rem;
}

.control-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 0.4rem;
}

.control-title {
	font-size: 0.75rem;
	font-weight: 600;
	color: #334155;
	display: flex;
	align-items: center;
}

.control-val {
	font-size: 0.72rem;
	font-weight: 700;
	color: #2563eb;
}

.slider-interactive-wrap {
	display: flex;
	align-items: center;
	gap: 0.4rem;

	.btn-step {
		width: 22px;
		height: 22px;
		border-radius: 5px;
		border: 1px solid #cbd5e1;
		background: #ffffff;
		display: flex;
		align-items: center;
		justify-content: center;
		color: #475569;
		cursor: pointer;
		font-size: 0.75rem;
		padding: 0;

		&:hover:not(:disabled) {
			border-color: #2563eb;
			color: #2563eb;
		}

		&:disabled {
			opacity: 0.4;
			cursor: not-allowed;
		}
	}
}

.studio-range-slider {
	flex: 1;
	accent-color: #2563eb;
	cursor: pointer;
	height: 4px;
}

.corner-preset-pills {
	display: flex;
	gap: 0.3rem;
}

.pill-btn {
	flex: 1;
	border: 1px solid #cbd5e1;
	background: #ffffff;
	border-radius: 5px;
	padding: 0.2rem 0.25rem;
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

.btn-reset-transforms {
	border: none;
	background: transparent;
	font-size: 0.72rem;
	font-weight: 600;
	color: #2563eb;
	cursor: pointer;
	padding: 0;
	display: inline-flex;
	align-items: center;

	&:hover {
		text-decoration: underline;
	}
}

/* Centered Tab Panes */
.tab-pane-centered {
	flex: 1 1 auto;
	display: flex;
	align-items: center;
	justify-content: center;
	padding: 1.25rem;
}

.monogram-picker-card {
	width: 100%;
	max-width: 380px;
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	border-radius: 12px;
	padding: 1.25rem;
	text-align: center;

	.monogram-desc {
		font-size: 0.75rem;
		color: #475569;
	}
}

.monogram-tiles-grid {
	display: grid;
	grid-template-columns: repeat(4, 1fr);
	gap: 0.6rem;
	max-width: 240px;
	margin: 0 auto;
}

.monogram-color-tile {
	width: 46px;
	height: 46px;
	border-radius: 10px;
	border: none;
	cursor: pointer;
	display: flex;
	align-items: center;
	justify-content: center;
	color: #ffffff;
	font-size: 1.2rem;
	font-weight: 700;
	transition: transform 0.12s ease, box-shadow 0.12s ease;
	box-shadow: 0 2px 6px rgba(0, 0, 0, 0.12);

	&:hover {
		transform: scale(1.1);
		box-shadow: 0 6px 14px rgba(0, 0, 0, 0.22);
	}
}

/* Footer Bar */
.editor-footer-bar {
	flex-shrink: 0;
	padding: 0.65rem 1.25rem;
	background: #ffffff;
	border-top: 1px solid #e2e8f0;
	display: flex;
	align-items: center;
}

.btn-danger-link {
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

.footer-btn-group {
	display: flex;
	gap: 0.6rem;
}
</style>
