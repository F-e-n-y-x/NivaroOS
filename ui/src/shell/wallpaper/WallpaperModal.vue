<template>
	<div class="modal-card wallpaper-picker" :class="{ 'is-embedded': embedded }">
		<header v-if="!embedded" class="modal-card-head">
			<div class="is-flex-grow-1">
				<h3 class="title is-header">{{ $t('Change wallpaper') }}</h3>
			</div>
		</header>

		<section class="modal-card-body">
			<!-- Dual Mode Toggle Bar -->
			<div class="dual-mode-bar is-flex is-align-items-center is-justify-content-between mb-3">
				<div class="dual-mode-info is-flex is-align-items-center">
					<b-icon icon="theme-light-dark" pack="mdi" size="is-18" class="mr-2 has-text-primary" />
					<div>
						<div class="dual-mode-title">{{ $t('Dual Mode Wallpaper') }}</div>
						<div class="dual-mode-sub">{{ $t('Automatically switch wallpaper between Light and Dark mode') }}</div>
					</div>
				</div>
				<b-switch v-model="dualMode" size="is-small" type="is-primary" @input="onDualModeToggled"></b-switch>
			</div>

			<!-- Mode Tabs (when dual mode is active) -->
			<div v-if="dualMode" class="wallpaper-mode-tabs is-flex mb-3">
				<button type="button" class="mode-tab-btn is-flex is-align-items-center"
					:class="{ active: activeModeTab === 'light' }" @click="activeModeTab = 'light'">
					<b-icon icon="white-balance-sunny" pack="mdi" size="is-16" class="mr-1" />
					<span>{{ $t('Light Mode Wallpaper') }}</span>
				</button>
				<button type="button" class="mode-tab-btn is-flex is-align-items-center ml-2"
					:class="{ active: activeModeTab === 'dark' }" @click="activeModeTab = 'dark'">
					<b-icon icon="weather-night" pack="mdi" size="is-16" class="mr-1" />
					<span>{{ $t('Dark Mode Wallpaper') }}</span>
				</button>
			</div>

			<div class="wallpaper-grid">
				<button v-for="(item, index) in wallpaperItems" :key="'wallpaper' + index" class="wallpaper-tile"
					:class="{ active: checkActive(item.path) }" @click="changeWallpaper(item.path)">
					<img :src="item.path" :alt="item.name" />
					<span v-if="checkActive(item.path)" class="tile-check">
						<b-icon icon="check-outline" pack="casa" size="is-16"></b-icon>
					</span>
				</button>

				<button v-for="item in galleryItems" :key="item.path" class="wallpaper-tile"
					:class="{ active: checkActive(item.path) }" @click="changeWallpaper(item.path)">
					<img :src="item.path" :alt="item.name" />
					<span v-if="checkActive(item.path)" class="tile-check">
						<b-icon icon="check-outline" pack="casa" size="is-16"></b-icon>
					</span>
				</button>

				<button class="wallpaper-tile upload-tile" :class="{ active: checkActiveFrom('Upload') }">
					<div id="upload-wallpaper" class="upload-tile-inner">
						<b-icon icon="picture-upload-outline" pack="casa" size="is-large"></b-icon>
						<span>{{ $t('Upload') }}</span>
					</div>
					<b-loading v-model="isUpLoading" :can-cancel="false" :is-full-page="false"></b-loading>
				</button>
			</div>
		</section>

		<footer v-if="isDirty" class="wallpaper-apply-bar is-flex is-align-items-center">
			<div class="is-flex-grow-1"></div>
			<div>
				<b-button :label="$t('Cancel')" rounded size="is-small" @click="cancel" />
				<b-button :label="$t('Apply')" :loading="isLoading" expaned rounded size="is-small" type="is-primary" @click="saveChange" />
			</div>
		</footer>
	</div>
</template>

<script>
const wallpaperConfig = "wallpaper"
const galleryPath = "/DATA/Gallery/Wallpaper"
const imageExtensions = ['.jpg', '.jpeg', '.png', '.webp', '.bmp', '.gif']
import Uploader from 'simple-uploader.js'
import { mixin } from '@/mixins/mixin'
import { getEffectiveTheme, getStoredThemeMode } from '@/utils/theme'

const DEFAULT_LIGHT = require('@/assets/background/wallpaper01.jpg')
const DEFAULT_DARK = require('@/assets/background/wallpaper02.jpg')

export default {
	mixins: [mixin],
	props: {
		// Embedded inside Settings - no floating overlay to close, so
		// Cancel just reverts the live preview back to what's applied.
		embedded: {
			type: Boolean,
			default: false
		}
	},
	data() {
		const storedDual = localStorage.getItem('wallpaper_dual_mode')
		const currentObj = this.$store.state.wallpaperObject || {}
		return {
			isLoading: false,
			isUpLoading: false,
			uploader: null,
			attributes: {
				accept: 'image/png, image/jpeg, image/svg+xml, image/bmp, image/png, image/gif'
			},
			dualMode: currentObj.dualMode !== undefined ? Boolean(currentObj.dualMode) : (storedDual !== 'false'),
			activeModeTab: getEffectiveTheme(getStoredThemeMode()),
			lightWallpaper: (currentObj.light && currentObj.light.path) || localStorage.getItem('wallpaper_light') || DEFAULT_LIGHT,
			darkWallpaper: (currentObj.dark && currentObj.dark.path) || localStorage.getItem('wallpaper_dark') || DEFAULT_DARK,
			wallpaperItems: [
				{
					name: "Daylight Peak (Light)",
					path: require('@/assets/background/wallpaper01.jpg')
				},
				{
					name: "Starry Night (Dark)",
					path: require('@/assets/background/wallpaper02.jpg')
				},
				{
					name: "Nivaro Landscape",
					path: require('@/assets/background/default_wallpaper.jpg')
				}
			],
			path: currentObj.path || localStorage.getItem('wallpaper') || DEFAULT_DARK,
			from: currentObj.from || "Built-in",
			galleryItems: []
		}
	},
	created() {
		this.loadGallery()
		this.uploader = new Uploader({
			target: `${this.$protocol}//${this.$baseURL}/v2/casaos/file/upload`,
			singleFile: true,
			testChunks: false,
			uploadMethod: "POST",
			allowDuplicateUploads: true,
			chunkSize: 1024 * 1024 * 1024 * 1024,
			query: (file) => ({ path: galleryPath, name: file.name })
		});
	},
	mounted() {
		this.uploader.assignBrowse(document.getElementById('upload-wallpaper'), false, true, this.attributes)
		this.uploader.on('filesSubmitted', () => {
			this.isUpLoading = true
			this.$api.sys.getVersion().then(() => {
				this.uploader.opts.headers.Authorization = this.$store.state.access_token || localStorage.getItem("access_token")
				this.uploader.upload()
			})
		})
		this.uploader.on('fileError', () => {
			this.isUpLoading = false
			this.$buefy.toast.open({
				message: this.$t('Upload failed, please try again!'),
				type: 'is-danger'
			})
		})
		this.uploader.on('fileSuccess', (rootFile) => {
			this.isUpLoading = false
			const uploadPath = this.getFileUrl({ path: `${galleryPath}/${rootFile.name}`, is_dir: false })
			this.loadGallery()
			this.applyWallpaper(uploadPath, "Upload")
		})
	},
	computed: {
		isDirty() {
			return false
		}
	},
	methods: {
		applyWallpaper(path, from) {
			let cleanPath = path
			if (cleanPath && cleanPath.includes('path=')) {
				try {
					const urlObj = new URL(cleanPath, 'http://localhost')
					const extracted = urlObj.searchParams.get('path')
					if (extracted) {
						cleanPath = extracted
					}
				} catch (e) {}
			}

			if (this.dualMode) {
				if (this.activeModeTab === 'light') {
					this.lightWallpaper = cleanPath
					localStorage.setItem('wallpaper_light', cleanPath)
				} else {
					this.darkWallpaper = cleanPath
					localStorage.setItem('wallpaper_dark', cleanPath)
				}
			} else {
				this.lightWallpaper = cleanPath
				this.darkWallpaper = cleanPath
				localStorage.setItem('wallpaper_light', cleanPath)
				localStorage.setItem('wallpaper_dark', cleanPath)
			}

			const effectiveTheme = getEffectiveTheme(getStoredThemeMode())
			const activePath = this.dualMode
				? (effectiveTheme === 'dark' ? this.darkWallpaper : this.lightWallpaper)
				: cleanPath

			const data = {
				path: activePath,
				from: from || this.from || "Built-in",
				dualMode: this.dualMode,
				light: {
					path: this.lightWallpaper,
					from: (this.activeModeTab === 'light' ? from : 'Built-in') || 'Built-in'
				},
				dark: {
					path: this.darkWallpaper,
					from: (this.activeModeTab === 'dark' ? from : 'Built-in') || 'Built-in'
				}
			}

			this.path = activePath
			this.from = data.from
			localStorage.setItem('wallpaper', activePath)
			localStorage.setItem('wallpaper_dual_mode', String(this.dualMode))
			this.$store.commit('SET_WALLPAPER', data)
			this.$messageBus('dashboardsetting_wallpaper', activePath.toString())
			this.$EventBus.$emit('desktop:wallpaper-change')

			this.$api.users.setCustomStorage(wallpaperConfig, data).catch(err => {
				console.error('Failed to save wallpaper setting', err)
			})
		},
		onDualModeToggled(val) {
			this.dualMode = val
			localStorage.setItem('wallpaper_dual_mode', String(val))
			const effectiveTheme = getEffectiveTheme(getStoredThemeMode())
			const activePath = val
				? (effectiveTheme === 'dark' ? this.darkWallpaper : this.lightWallpaper)
				: (this.path || this.darkWallpaper)

			const data = {
				path: activePath,
				from: this.from || "Built-in",
				dualMode: val,
				light: { path: this.lightWallpaper, from: 'Built-in' },
				dark: { path: this.darkWallpaper, from: 'Built-in' }
			}
			this.$store.commit('SET_WALLPAPER', data)
			this.$EventBus.$emit('desktop:wallpaper-change')
			this.$api.users.setCustomStorage(wallpaperConfig, data).catch(() => {})
		},
		cancel() {
			this.path = this.$store.state.wallpaperObject.path
			this.from = this.$store.state.wallpaperObject.from
			this.$emit('close')
		},
		saveChange() {
			this.applyWallpaper(this.path, this.from)
			this.$emit('close')
		},
		loadGallery() {
			this.$api.folder.getList(galleryPath).then(res => {
				if (res.data.success !== 200) return
				const content = (res.data.data && res.data.data.content) || []
				this.galleryItems = content
					.filter(f => !f.is_dir && imageExtensions.some(ext => f.name.toLowerCase().endsWith(ext)))
					.map(f => ({ name: f.name, path: this.getFileUrl({ path: f.path, is_dir: false }) }))
			}).catch(() => {
				this.galleryItems = []
			})
		},
		changeWallpaper(path) {
			const from = path.includes('/DATA/') ? 'Gallery' : 'Built-in'
			this.applyWallpaper(path, from)
		},
		cleanUrlPath(p) {
			if (!p) return ''
			if (p.includes('path=')) {
				try {
					const u = new URL(p, 'http://localhost')
					const extracted = u.searchParams.get('path')
					if (extracted) return extracted
				} catch (e) {}
			}
			return p
		},
		checkActive(path) {
			const clean = this.cleanUrlPath(path)
			if (this.dualMode) {
				const activeTarget = this.activeModeTab === 'light' ? this.lightWallpaper : this.darkWallpaper
				return this.cleanUrlPath(activeTarget) === clean
			}
			const current = (this.$store.state.wallpaperObject && this.$store.state.wallpaperObject.path) || this.path
			return this.cleanUrlPath(current) === clean || this.cleanUrlPath(this.path) === clean
		},
		checkActiveFrom(from) {
			return this.from == from
		},
		getTargetUrl() {
			const accessToken = localStorage.getItem("access_token")
			return `${this.$protocol}//${this.$baseURL}/v1/users/current/image/${wallpaperConfig}?token=${accessToken}&type=wallpaper`
		},
		parseUrl(serverUrl) {
			if (!serverUrl) return ''
			const newUrl = serverUrl.replace('SERVER_URL', `${this.$protocol}//${this.$baseURL}`)
			return newUrl;
		},
	}
}
</script>

<style lang="scss" scoped>
.modal-card.is-embedded {
	box-shadow: none;
	background: transparent;
	width: 100%;
	max-width: none;
	max-height: none;
	margin: 0;
	overflow: visible;
}

.wallpaper-picker .modal-card-body {
	padding: var(--space-5);
}

.dual-mode-bar {
	padding: var(--space-2) var(--space-3);
	background: var(--theme-card-subtle, rgba(0, 0, 0, 0.02));
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	border-radius: var(--radius-card, 10px);

	.dual-mode-title {
		font-size: var(--font-sm, 0.85rem);
		font-weight: 600;
		color: var(--theme-text-primary, #0f172a);
		line-height: 1.2;
	}

	.dual-mode-sub {
		font-size: var(--font-xs, 0.75rem);
		color: var(--theme-text-secondary, #64748b);
		line-height: 1.2;
		margin-top: 2px;
	}
}

.wallpaper-mode-tabs {
	display: flex;
	gap: var(--space-2);

	.mode-tab-btn {
		flex: 1;
		padding: var(--space-2) var(--space-3);
		border-radius: var(--radius-sm, 6px);
		border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
		background: var(--theme-card-subtle, rgba(0, 0, 0, 0.02));
		color: var(--theme-text-secondary, #64748b);
		font-size: var(--font-xs, 0.75rem);
		font-weight: 600;
		cursor: pointer;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		transition: all 0.15s ease;

		&:hover {
			background: var(--theme-card-hover, rgba(0, 0, 0, 0.04));
			color: var(--theme-text-primary, #0f172a);
		}

		&.active {
			background: var(--theme-card-bg, #ffffff);
			color: var(--color-primary, #2563eb);
			border-color: var(--color-primary, #2563eb);
			box-shadow: 0 2px 6px rgba(37, 99, 235, 0.15);
		}
	}
}

.wallpaper-apply-bar {
	padding: 0 var(--space-5) var(--space-5);

	> div:last-child {
		display: flex;
		gap: var(--space-2);
	}
}

.wallpaper-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(7rem, 1fr));
	grid-auto-rows: 6.5rem;
	gap: 0.9rem;
	max-height: calc(6.5rem * 2 + 0.9rem);
	overflow-y: auto;
	padding-right: var(--space-1);
}

.wallpaper-tile {
	position: relative;
	border-radius: var(--radius-card);
	border: 2px solid transparent;
	overflow: hidden;
	cursor: pointer;
	padding: 0;
	background: var(--theme-card-hover, rgba(0, 0, 0, 0.03));
	transition: border-color 0.15s ease, transform 0.15s ease;

	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}

	&:hover {
		transform: translateY(-1px);
	}

	&.active {
		border-color: var(--color-primary, #2563eb);
	}
}

.tile-check {
	position: absolute;
	top: 0.4rem;
	right: 0.4rem;
	width: 1.3rem;
	height: 1.3rem;
	border-radius: 50%;
	background: var(--color-primary, #2563eb);
	color: #fff;
	display: flex;
	align-items: center;
	justify-content: center;
}

.upload-tile {
	background: rgba(0, 0, 0, 0.02);
	border-color: rgba(0, 0, 0, 0.1);
	border-style: dashed;
}

.upload-tile-inner {
	width: 100%;
	height: 100%;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: var(--space-1);
	color: rgba(44, 62, 80, 0.6);
	font-size: var(--font-xs);
}
</style>
