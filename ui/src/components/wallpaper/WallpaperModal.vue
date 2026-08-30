<template>
	<div class="modal-card wallpaper-picker" :class="{ 'is-embedded': embedded }">
		<header v-if="!embedded" class="modal-card-head">
			<div class="is-flex-grow-1">
				<h3 class="title is-header">{{ $t('Change wallpaper') }}</h3>
			</div>
		</header>

		<section class="modal-card-body">
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
		return {
			isLoading: false,
			isUpLoading: false,
			uploader: null,
			attributes: {
				accept: 'image/png, image/jpeg, image/svg+xml, image/bmp, image/png, image/gif'
			},
			wallpaperItems: [
				{
					name: "Built-in wallpaper 1",
					path: require('@/assets/background/wallpaper01.jpg')
				},
				{
					name: "Built-in wallpaper 2",
					path: require('@/assets/background/wallpaper02.jpg')
				}
			],
			backgroundStyleObj: {
				backgroundImage: `url(${this.parseUrl(this.$store.state.wallpaperObject.path)})`
			},
			path: this.$store.state.wallpaperObject.path,
			from: this.$store.state.wallpaperObject.from,
			galleryItems: []
		}
	},
	components: {},
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
			this.$api.sys.getVersion().then(res => {
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
		// Uploaded straight into the gallery folder (not the old single-slot
		// avatar-style endpoint) so it shows up as a real, re-selectable
		// tile - not just a hidden "currently applied" file with no history.
		this.uploader.on('fileSuccess', (rootFile) => {
			this.isUpLoading = false
			const uploadPath = this.getFileUrl({ path: `${galleryPath}/${rootFile.name}`, is_dir: false })
			this.loadGallery()
			this.backgroundStyleObj.backgroundImage = `url(${uploadPath})`
			this.path = uploadPath
			this.from = "Upload"
		})

	},
	computed: {
		isDirty() {
			return this.path !== this.$store.state.wallpaperObject.path
		}
	},
	methods: {
		cancel() {
			this.path = this.$store.state.wallpaperObject.path
			this.from = this.$store.state.wallpaperObject.from
			this.backgroundStyleObj.backgroundImage = `url(${this.parseUrl(this.path)})`
			this.$emit('close')
		},
		saveChange() {
			let data = {
				path: this.path,
				from: this.from
			}
			this.isLoading = true
			this.$api.users.setCustomStorage(wallpaperConfig, data).then(res => {
				this.isLoading = false
				if (res.data.success === 200) {
					this.$messageBus('dashboardsetting_wallpaper', res.data.data.path.toString())
					this.$emit("close")
					setTimeout(() => {
						this.$store.commit('SET_WALLPAPER', {
							path: res.data.data.path,
							from: res.data.data.from
						})
					}, 300)

				} else {
					this.$buefy.toast.open({
						message: this.$t('Save failed, please try again!'),
						type: 'is-danger'
					})
				}

			})
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
			this.backgroundStyleObj.backgroundImage = `url(${this.parseUrl(path)})`
			this.path = path
			this.from = "Built-in"
		},

		checkActive(path) {
			return this.path == path
		},
		checkActiveFrom(from) {
			return this.from == from
		},
		getTargetUrl() {
			const accessToken = localStorage.getItem("access_token")
			return `${this.$protocol}//${this.$baseURL}/v1/users/current/image/${wallpaperConfig}?token=${accessToken}&type=wallpaper`
		},
		parseUrl(serverUrl) {
			const newUrl = serverUrl.replace('SERVER_URL', `${this.$protocol}//${this.$baseURL}`)
			return newUrl;
		},
	}
}
</script>

<style lang="scss" scoped>
// Buefy's base .modal-card rule (meant for a floating, centered dialog)
// pins width to 640px with auto margins above 769px - embedded here inside
// a Settings card, it needs to fill whatever width the card actually has
// so the wallpaper grid can adapt its column count to the window size.
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
	padding: 1.25rem;
}

.wallpaper-apply-bar {
	padding: 0 1.25rem 1.25rem;

	> div:last-child {
		display: flex;
		gap: 0.5rem;
	}
}

.wallpaper-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(7rem, 1fr));
	grid-auto-rows: 6.5rem;
	gap: 0.9rem;
	max-height: calc(6.5rem * 2 + 0.9rem);
	overflow-y: auto;
	padding-right: 0.3rem;
}

.wallpaper-tile {
	position: relative;
	border-radius: 10px;
	border: 2px solid transparent;
	overflow: hidden;
	cursor: pointer;
	padding: 0;
	background: rgba(0, 0, 0, 0.03);
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
		border-color: hsla(208, 100%, 50%, 1);
	}
}

.tile-check {
	position: absolute;
	top: 0.4rem;
	right: 0.4rem;
	width: 1.3rem;
	height: 1.3rem;
	border-radius: 50%;
	background: hsla(208, 100%, 50%, 1);
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
	gap: 0.3rem;
	color: rgba(44, 62, 80, 0.6);
	font-size: 0.75rem;
}
</style>
