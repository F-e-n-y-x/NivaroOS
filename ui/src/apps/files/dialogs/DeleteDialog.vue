<!-- src/apps/files/dialogs/DeleteDialog.vue -->
<!--
	Delete confirmation, saying exactly what happens where: into the Trash
	(restorable for 30 days - local disks, network shares, cloud drives
	that move files on the server, phones), into a cloud provider's own
	trash (Google Drive, OneDrive, TeraBox), or permanently, with why
	(no Trash there, read-only share, or "delete permanently instead" /
	Shift+Delete).
-->
<template>
	<files-dialog-overlay :title="title" @close="$emit('cancel')">
		<div class="delete-dialog">
			<p class="dd-lead">{{ lead }}</p>
			<p v-if="outcome.mode === 'provider'" class="dd-note">
				{{ $t("Restore from {provider}'s trash (its website or app) - it isn't listed in Trash here.", { provider: outcome.provider }) }}
			</p>
			<p v-else-if="outcome.mode === 'trash'" class="dd-note">{{ trashNote }}</p>
			<p v-else class="dd-note warn">
				<b-icon icon="alert-outline" custom-size="mdi-16px"></b-icon>
				{{ warning }}
			</p>
			<b-checkbox v-if="supported" v-model="skipTrash" size="is-small" class="dd-check">{{ $t('Delete permanently instead') }}</b-checkbox>
			<div class="buttons is-justify-content-flex-end mt-4">
				<b-button @click="$emit('cancel')">{{ $t('Cancel') }}</b-button>
				<b-button :type="outcome.mode === 'permanent' ? 'is-danger' : 'is-primary'" :loading="checking" @click="$emit('confirm', { permanent })">
					{{ button }}
				</b-button>
			</div>
		</div>
	</files-dialog-overlay>
</template>

<script>
import DialogOverlay from '../DialogOverlay.vue'
import { deleteOutcome } from '@/utils/files/trashWhere'

export default {
	emits: ['cancel', 'confirm'],
	name: 'delete-dialog',
	components: { FilesDialogOverlay: DialogOverlay },
	props: {
		items: { type: Array, required: true },
		forcePermanent: { type: Boolean, default: false },
	},
	data() {
		return { support: { supported: true, kind: 'disk' }, checking: true, skipTrash: this.forcePermanent }
	},
	computed: {
		count() {
			return this.items.length
		},
		name() {
			return this.items[0] ? this.items[0].name || this.items[0].path.split('/').pop() : ''
		},
		supported() {
			return !!this.support.supported
		},
		outcome() {
			return deleteOutcome(this.support, this.skipTrash)
		},
		permanent() {
			return this.outcome.mode === 'permanent'
		},
		title() {
			if (this.outcome.mode === 'provider') return this.$t("Move to {provider}'s trash?", { provider: this.outcome.provider })
			return this.permanent ? this.$t('Delete permanently?') : this.$t('Move to Trash?')
		},
		lead() {
			const one = this.count === 1
			switch (this.outcome.mode) {
				case 'provider':
					return one ? this.$t("“{name}” will be moved to {provider}'s trash.", { name: this.name, provider: this.outcome.provider }) : this.$t("{n} items will be moved to {provider}'s trash.", { n: this.count, provider: this.outcome.provider })
				case 'trash':
					return one ? this.$t('“{name}” will be moved to the Trash.', { name: this.name }) : this.$t('{n} items will be moved to the Trash.', { n: this.count })
				default:
					return one ? this.$t('“{name}” will be deleted permanently.', { name: this.name }) : this.$t('{n} items will be deleted permanently.', { n: this.count })
			}
		},
		trashNote() {
			if (this.outcome.kind === 'phone') return this.$t('They stay on the phone, hidden, and can be restored from Trash in the sidebar for 30 days.')
			return this.$t('You can restore them from Trash in the sidebar for 30 days.')
		},
		warning() {
			switch (this.outcome.reason) {
				case 'chosen':
					return this.$t("This can't be undone.")
				case 'readonly':
					return this.$t("This share is read-only for NivaroOS, so it can't keep a Trash - deleted items can't be recovered.")
				case 'no_server_move':
					return this.$t("This cloud drive has no Trash (it can't move files on its servers) - deleted items can't be recovered.")
				default:
					return this.$t("This location has no Trash - deleted items can't be recovered.")
			}
		},
		button() {
			if (this.outcome.mode === 'provider') return this.$t('Delete')
			return this.permanent ? this.$t('Delete permanently') : this.$t('Move to Trash')
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
			this.support = (res.data && res.data.data) || {}
		} catch (e) {
			// The server decides; it falls back to permanent where it must.
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
