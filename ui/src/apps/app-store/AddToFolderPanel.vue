<template>
	<div class="modal-card">
		<!-- Modal-Card Body Start -->
		<section class="modal-card-body">
			<div class="node-card">
				<b-field :label="$t('Folder')">
					<b-autocomplete ref="input" v-model="name" :data="filteredFolders" :open-on-focus="true"
						:placeholder="$t('Existing or new folder name')" append-to-body expanded field="name"
						@select="option => (name = option.name)">
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
				<b-button :disabled="!name" :label="$t('Add')" expaned rounded type="is-primary" @click="confirm" />
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
			name: ''
		}
	},
	computed: {
		filteredFolders() {
			if (!this.name) return this.folders
			const lower = this.name.toLowerCase()
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
			if (!this.name) return
			this.$emit('confirm', this.name)

			if (this.itemName) {
				let folder = this.folders.find(f => f.name === this.name)
				if (!folder) {
					folder = await this.createFolder(this.name)
				}
				await this.addAppToFolder(this.itemName, folder.id)
				this.$EventBus.$emit(events.GET_APP_LIST)
			}
			this.$emit('close')
		}
	}
}
</script>
