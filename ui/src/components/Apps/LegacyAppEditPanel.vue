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
			iconTab: 'url',
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
	background: #f5f5f5;
	border: 1px solid #e0e0e0;
	cursor: pointer;
	display: flex;
	align-items: center;
	justify-content: center;
	transition: all 0.15s ease;

	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}

	.icon-preview-edit-hint {
		position: absolute;
		inset: 0;
		background: rgba(0, 0, 0, 0.55);
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
	border: 1px solid #e0e0e0;
	border-radius: 6px;
	overflow: hidden;

	button {
		border: none;
		background: #fafafa;
		padding: 0.35rem 0.85rem;
		font-size: 0.8rem;
		cursor: pointer;
		color: #666;
		transition: all 0.15s ease;

		&.active {
			background: #2563eb;
			color: #fff;
			font-weight: 600;
		}
	}
}
</style>
