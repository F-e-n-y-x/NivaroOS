<!-- src/components/files/dialogs/ConfirmDialog.vue -->
<template>
	<files-dialog-overlay :title="title" @close="$emit('cancel')">
		<div class="confirm-dialog">
			<p v-html="message"></p>
			<div class="buttons is-justify-content-flex-end mt-4">
				<b-button @click="$emit('cancel')">{{ $t('Cancel') }}</b-button>
				<b-button type="is-danger" @click="$emit('confirm')">{{ confirmText }}</b-button>
			</div>
		</div>
	</files-dialog-overlay>
</template>

<script>
import DialogOverlay from '../DialogOverlay.vue'

export default {
	name: 'confirm-dialog',
	components: { FilesDialogOverlay: DialogOverlay },
	props: {
		title: { type: String, required: true },
		message: { type: String, required: true },
		confirmText: { type: String, required: true },
	},
}
</script>
