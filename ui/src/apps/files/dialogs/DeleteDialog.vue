<!-- src/apps/files/dialogs/DeleteDialog.vue -->
<!--
	Delete confirmation. By default items go to the Trash (restorable for
	30 days); the dialog says so, offers "delete permanently instead", and
	switches to a clear permanent-delete warning for locations that can't
	keep a Trash (cloud drives, phones, network shares) or when opened
	with Shift+Delete.
-->
<template>
	<files-dialog-overlay :title="permanent ? $t('Delete permanently?') : $t('Move to Trash?')" @close="$emit('cancel')">
		<div class="delete-dialog">
			<p class="dd-lead">
				<template v-if="permanent">{{ count === 1 ? $t('“{name}” will be deleted permanently.', { name }) : $t('{n} items will be deleted permanently.', { n: count }) }}</template>
				<template v-else>{{ count === 1 ? $t('“{name}” will be moved to the Trash.', { name }) : $t('{n} items will be moved to the Trash.', { n: count }) }}</template>
			</p>
			<p v-if="!supported" class="dd-note warn">
				<b-icon icon="alert-outline" custom-size="mdi-16px"></b-icon>
				{{ $t("This location has no Trash - deleted items can't be recovered.") }}
			</p>
			<p v-else-if="!permanent" class="dd-note">{{ $t('You can restore them from Trash in the sidebar for 30 days.') }}</p>
			<p v-else class="dd-note warn">
				<b-icon icon="alert-outline" custom-size="mdi-16px"></b-icon>
				{{ $t("This can't be undone.") }}
			</p>
			<b-checkbox v-if="supported" v-model="skipTrash" size="is-small" class="dd-check">{{ $t('Delete permanently instead') }}</b-checkbox>
			<div class="buttons is-justify-content-flex-end mt-4">
				<b-button @click="$emit('cancel')">{{ $t('Cancel') }}</b-button>
				<b-button :type="permanent ? 'is-danger' : 'is-primary'" :loading="checking" @click="$emit('confirm', { permanent })">
					{{ permanent ? $t('Delete permanently') : $t('Move to Trash') }}
				</b-button>
			</div>
		</div>
	</files-dialog-overlay>
</template>

<script>
import DialogOverlay from '../DialogOverlay.vue'

export default {
	name: 'delete-dialog',
	components: { FilesDialogOverlay: DialogOverlay },
	props: {
		items: { type: Array, required: true },
		forcePermanent: { type: Boolean, default: false },
	},
	data() {
		return { supported: true, checking: true, skipTrash: this.forcePermanent }
	},
	computed: {
		count() {
			return this.items.length
		},
		name() {
			return this.items[0] ? this.items[0].name || this.items[0].path.split('/').pop() : ''
		},
		permanent() {
			return !this.supported || this.skipTrash
		},
	},
	async created() {
		// Every selected item comes from the same folder view, so one check
		// covers them all.
		const first = this.items[0]
		if (!first) return
		try {
			const parent = first.path.replace(/\/+$/, '').split('/').slice(0, -1).join('/') || '/'
			const res = await this.$api.trash.support(parent)
			this.supported = !!(res.data && res.data.data && res.data.data.supported)
		} catch (e) {
			this.supported = true // server decides; it falls back to permanent where it must
		} finally {
			this.checking = false
		}
	},
}
</script>

<style lang="scss" scoped>
.delete-dialog {
	font-size: var(--font-sm);
}
.dd-lead {
	font-weight: 600;
	word-break: break-word;
}
.dd-note {
	margin-top: var(--space-2);
	color: var(--theme-text-muted, #64748b);
	font-size: var(--font-xs);
	display: flex;
	align-items: center;
	gap: 0.3rem;

	&.warn {
		color: var(--color-danger, #dc2626);
	}
}
.dd-check {
	margin-top: var(--space-3);
	font-size: var(--font-xs);
}
</style>
