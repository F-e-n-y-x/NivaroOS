<template>
	<div class="legacy-app-edit-window">
		<!-- No header here - this opens as a real desktop window, whose
		titlebar (title + close/minimize) is provided by DesktopWindow. -->
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
						<div :style="{ borderRadius: iconRadius + '%' }" class="icon-preview" :title="$t('Click to edit')"
							@click="showIconEditor = true">
							<img :src="icon || fallbackIcon">
							<span class="icon-preview-edit-hint">{{ $t('Edit') }}</span>
						</div>

						<div class="icon-field-controls">
							<div class="source-toggle">
								<button :class="{ active: iconTab === 'url' }" type="button" @click="iconTab = 'url'">
									{{ $t('URL') }}
								</button>
								<button :class="{ active: iconTab === 'upload' }" type="button" @click="iconTab = 'upload'">
									{{ $t('Upload') }}
								</button>
							</div>

							<b-input v-if="iconTab === 'url'" v-model="icon" :loading="isCompressing"
								:placeholder="$t('Icon URL')" class="mt-2" expanded @blur="compressIconUrl"></b-input>
							<b-button v-else :loading="isCompressing" class="mt-2" expanded @click="$refs.iconFile.click()">
								{{ $t('Choose file') }}
							</b-button>
							<input ref="iconFile" accept="image/*" style="display: none" type="file"
								@change="handleIconFile">
						</div>
					</div>
				</b-field>
			</div>
		</section>
		<footer class="edit-footer is-flex is-align-items-center">
			<div class="is-flex-grow-1"></div>
			<div>
				<b-button :label="$t('Cancel')" rounded @click="$emit('close')" />
				<b-button :label="$t('Save')" expaned rounded type="is-primary" @click="save" />
			</div>
		</footer>

		<b-modal v-model="showIconEditor" :can-cancel="['escape', 'outside']" animation="zoom-in" aria-modal
				 has-modal-card>
			<template #default>
				<icon-editor-modal v-if="icon" :initial-radius="iconRadius" :src="icon" @apply="handleIconEdited"
					@close="showIconEditor = false"></icon-editor-modal>
			</template>
		</b-modal>
	</div>
</template>

<script>
import IconEditorModal from './IconEditorModal.vue'
import { ice_i18n } from '@/mixins/base/common-i18n'
import business_LegacyAppOverrides from '@/mixins/app/Business_LegacyAppOverrides'
import events from '@/events/events'

// Icons render at 64px CSS (32px in this panel's own preview); 192px
// covers up to 3x-DPI displays sharply while staying tiny on disk -
// far smaller than most source images (especially pasted URLs, which
// are often full-size logos), which is what actually matters for
// dashboard load time.
const ICON_MAX_DIM = 192

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
			iconRadius: 0,
			iconTab: 'url',
			isCompressing: false,
			showIconEditor: false,
			fallbackIcon: require('@/assets/img/app/default.svg')
		}
	},
	computed: {
		// Only container/legacy apps have no real launch behavior of
		// their own to open a click-through URL for - v1/v2 apps already
		// launch normally, so editing them is icon-only.
		showUrlField() {
			return this.item.app_type === 'container'
		},
		// The app's real name, unaffected by any rename override - shown
		// as the input's placeholder so an empty field clearly means
		// "use the default name" rather than looking blank/broken.
		originalName() {
			return (this.item.title && ice_i18n({ ...this.item.title, custom: undefined })) || this.item.name
		}
	},
	created() {
		if (this.override) {
			this.name = this.override.title || ''
			this.url = this.override.url || ''
			this.icon = this.override.icon || ''
			this.iconRadius = this.override.iconRadius || 0
		} else {
			this.icon = this.item.icon || ''
		}
	},
	methods: {
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
					this.icon = this.resizeImageToDataUrl(img)
					this.isCompressing = false
				}
				img.src = reader.result
			}
			reader.readAsDataURL(file)
		},

		// Best-effort: pasted icon URLs are often full-size logos hosted
		// elsewhere. Try to fetch+resize+cache them locally the same way
		// as an upload; most icon hosts don't allow cross-origin canvas
		// reads, in which case this silently keeps the plain URL instead
		// (still works, just isn't resized/cached).
		compressIconUrl() {
			if (!this.icon || this.icon.startsWith('data:')) return
			const url = this.icon
			this.isCompressing = true
			const img = new Image()
			img.crossOrigin = 'anonymous'
			img.onload = () => {
				try {
					this.icon = this.resizeImageToDataUrl(img)
				} catch (e) {
					// Tainted canvas (no CORS) - keep the original URL.
				}
				this.isCompressing = false
			}
			img.onerror = () => {
				this.isCompressing = false
			}
			img.src = url
		},

		// Only the crop gets baked into the icon itself - roundness is
		// kept as separate metadata and applied live via CSS wherever
		// the icon renders (see AppCard.vue), so it stays adjustable
		// every time instead of compounding on re-edit.
		handleIconEdited({ dataUrl, radius }) {
			if (dataUrl) this.icon = dataUrl
			this.iconRadius = radius
		},

		save() {
			this.saveLegacyAppOverride(this.item.name, {
				title: this.name,
				url: this.url,
				icon: this.icon,
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
	border-top: 1px solid rgb(228 233 237);

	.button + .button {
		margin-left: 0.5rem;
	}
}

.icon-field {
	display: flex;
	align-items: flex-start;
	gap: 0.9rem;
	width: 100%;
}

.icon-preview {
	position: relative;
	flex-shrink: 0;
	width: 56px;
	height: 56px;
	overflow: hidden;
	background: repeating-conic-gradient(#00000010 0% 25%, transparent 0% 50%) 50% / 12px 12px;
	border: 1px solid rgba(0, 0, 0, 0.12);
	cursor: pointer;

	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}

	.icon-preview-edit-hint {
		position: absolute;
		inset: 0;
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: 0.65rem;
		color: #fff;
		background: rgba(0, 0, 0, 0.45);
		opacity: 0;
		transition: opacity 0.15s ease;
	}

	&:hover .icon-preview-edit-hint {
		opacity: 1;
	}
}

.icon-field-controls {
	flex: 1;
	min-width: 0;
}

.source-toggle {
	display: inline-flex;
	padding: 2px;
	background: rgba(0, 0, 0, 0.06);
	border-radius: 8px;

	button {
		border: none;
		background: transparent;
		padding: 0.3rem 0.85rem;
		border-radius: 6px;
		font-size: 0.75rem;
		cursor: pointer;
		color: inherit;

		&.active {
			background: #fff;
			box-shadow: 0 1px 2px rgba(0, 0, 0, 0.15);
			font-weight: 600;
		}
	}
}
</style>
