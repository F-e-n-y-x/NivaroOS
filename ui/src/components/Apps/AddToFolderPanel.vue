<template>
	<div class="modal-card">
		<!-- Modal-Card Header Start -->
		<header class="modal-card-head">
			<div class="is-flex-grow-1">
				<h3 class="title is-header">{{ $t('Add to folder') }}</h3>
			</div>
			<b-icon class="close-button" icon="close-outline" pack="casa" @click.native="$emit('close')" />
		</header>
		<!-- Modal-Card Header End -->
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

<script>
export default {
	props: {
		folders: {
			type: Array,
			default: () => []
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
		confirm() {
			if (!this.name) return
			this.$emit('confirm', this.name)
			this.$emit('close')
		}
	}
}
</script>
