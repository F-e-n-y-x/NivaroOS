<!-- src/apps/files/dialogs/ConfirmDialog.vue -->
<template>
	<files-dialog-overlay :title="title" @close="$emit('cancel')">
		<div class="confirm-dialog">
			<!-- Was v-html - an XSS sink for any future call that interpolates
			     a filename/device/container name into `message`. FilesApp.vue's
			     delete confirmation genuinely wants a literal <b>delete</b> for
			     emphasis though, so messageParts below parses just that one tag
			     out and renders it as a real <b> element - every text segment
			     (including inside the bold one) still only ever goes through
			     {{ }} interpolation, which Vue auto-escapes, so this stays safe
			     even if a later caller puts a dynamic value inside the <b>. -->
			<p>
				<component :is="part.bold ? 'b' : 'span'" v-for="(part, i) in messageParts" :key="i">{{ part.text }}</component>
			</p>
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
	computed: {
		// Splits `message` on <b>...</b> only - every other character (tags
		// or not) is treated as literal text. Deliberately not a general
		// HTML parser: the only markup any caller actually needs is bold
		// emphasis, and keeping this to one hardcoded tag is what makes it
		// safe to render without v-html in the first place.
		messageParts() {
			const parts = []
			const re = /<b>(.*?)<\/b>/gi
			let lastIndex = 0
			let match
			while ((match = re.exec(this.message)) !== null) {
				if (match.index > lastIndex) {
					parts.push({ text: this.message.slice(lastIndex, match.index), bold: false })
				}
				parts.push({ text: match[1], bold: true })
				lastIndex = re.lastIndex
			}
			if (lastIndex < this.message.length) {
				parts.push({ text: this.message.slice(lastIndex), bold: false })
			}
			return parts
		},
	},
}
</script>
