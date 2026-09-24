<template>
	<div class="modal-card">
		<!-- Modal-Card Body Start -->
		<section class="modal-card-body">
			<div class="node-card">
				<b-field :label="$t('Folder')">
					<b-autocomplete ref="input" v-model="name" :data="filteredFolders" :open-on-focus="true"
						:placeholder="$t('Existing or new folder name')" append-to-body expanded field="name"
						maxlength="30" @select="option => (name = option ? option.name : name)"
						@keyup.native.enter="confirm">
						<template #empty>{{ $t('No matching folders - this will create a new one') }}</template>
					</b-autocomplete>
				</b-field>
			</div>
		</section>
		<!-- Modal-Card Body End -->
		<!-- Modal-Card Footer Start-->
		<footer class="modal-card-foot is-flex is-align-items-center">
			<div class="is-flex-grow-1"></div>
			<div>
				<b-button :disabled="!trimmedName || isSaving" :loading="isSaving" :label="$t('Add')" expanded rounded type="is-primary" @click="confirm" />
			</div>
		</footer>
		<!-- Modal-Card Footer End -->
	</div>
</template>

<style lang="scss" scoped>
.modal-card {
	height: 100%;
	display: flex;
	flex-direction: column;
}

.modal-card-body {
	flex: 1 1 auto;
}
</style>

<script>
import events from '@/events/events'
import business_Folders from '@/mixins/app/Business_Folders'
import { apiErrorHtml } from '@/mixins/app/apiError'

export default {
	mixins: [business_Folders],
	props: {
		folders: {
			type: Array,
			default: () => []
		},
		// When set, confirm() adds this app to the chosen/new folder itself
		// instead of relying on a parent-listened 'confirm' event - needed
		// once this component is opened as a standalone desktop window
		// rather than embedded inline, since the shared window chrome only
		// forwards close/minimize/drag-start/status-change, not custom
		// business events.
		itemName: {
			type: String,
			default: null
		}
	},
	data() {
		return {
			name: '',
			isSaving: false
		}
	},
	computed: {
		trimmedName() {
			return String(this.name || '').trim()
		},
		filteredFolders() {
			if (!this.trimmedName) return this.folders
			const lower = this.trimmedName.toLowerCase()
			return this.folders.filter(f => f.name.toLowerCase().includes(lower))
		}
	},
	mounted() {
		this.$nextTick(() => {
			this.$refs.input.focus()
		})
	},
	methods: {
		async confirm() {
			const name = this.trimmedName
			if (!name || this.isSaving) return
			this.isSaving = true
			this.$emit('confirm', name)

			try {
				if (this.itemName) {
					let folder = this.folders.find(f => f.name.trim() === name)
					if (!folder) {
						folder = await this.createFolder(name)
					}
					await this.addAppToFolder(this.itemName, folder.id)
					this.$EventBus.$emit(events.GET_APP_LIST)
				}
				this.$emit('close')
			} catch (e) {
				console.error('add to folder', e)
				this.$buefy.toast.open({
					message: this.$t('Folders could not be updated: {reason}', { reason: apiErrorHtml(e, this.$t('Something went wrong')) }),
					type: 'is-danger',
					position: 'is-top',
					duration: 5000
				})
			} finally {
				this.isSaving = false
			}
		}
	}
}
</script>
