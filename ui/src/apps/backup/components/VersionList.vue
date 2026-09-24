<!-- A job's versions (spec §12.7): the current copy, then the recycle
     folders (mirror) or the archives, newest first. As a radio group
     (Restore step 2: roving tabindex, arrow keys, Space) or as a plain
     list with a Browse button per version (job details). -->
<template>
	<div class="bk-versions">
		<p v-if="!versions.length" class="bk-secondary">{{ $t('backup.restore.no_versions') }}</p>
		<p v-else-if="jobType === 'copy'" class="bk-secondary bk-versions-note">{{ $t('backup.restore.copy_note') }}</p>
		<div
			v-if="selectable && versions.length"
			ref="group"
			class="bk-versions-list"
			role="radiogroup"
			:aria-labelledby="labelledby || null"
			@keydown="onKeydown"
		>
			<div
				v-for="v in versions"
				:key="v.id"
				class="bk-version is-radio"
				role="radio"
				:aria-checked="v.id === value ? 'true' : 'false'"
				:tabindex="v.id === focusId ? 0 : -1"
				:data-id="v.id"
				@click="$emit('input', v.id)"
			>
				<span class="bk-radio-dot" aria-hidden="true"></span>
				<span class="bk-version-label">{{ label(v) }}</span>
				<span class="bk-version-meta">{{ meta(v) }}</span>
			</div>
		</div>
		<ul v-else-if="versions.length" class="bk-versions-list">
			<li v-for="v in versions" :key="v.id" class="bk-version">
				<span class="bk-version-label">{{ label(v) }}</span>
				<span class="bk-version-meta">{{ meta(v) }}</span>
				<button type="button" class="bk-btn is-small" :aria-label="$t('backup.restore.browse_named', { name: label(v) })" @click="$emit('browse', v)">{{ $t('backup.restore.browse') }}</button>
			</li>
		</ul>
	</div>
</template>

<script>
export default {
	name: 'VersionList',
	props: {
		versions: { type: Array, required: true },
		fmt: { type: Object, required: true },
		jobType: { type: String, default: '' },
		// When the current copy was last updated (job stats.last_success).
		currentAt: { type: String, default: '' },
		selectable: { type: Boolean, default: false },
		value: { type: String, default: '' },
		labelledby: { type: String, default: '' }
	},
	computed: {
		focusId() {
			return this.versions.some(v => v.id === this.value) ? this.value : this.versions.length ? this.versions[0].id : ''
		}
	},
	methods: {
		label(v) {
			if (v.kind === 'current') return this.currentAt ? this.$t('backup.ver.current_as_of', { at: this.fmt.when(this.currentAt) }) : this.$t(v.label_key || 'backup.ver.current')
			return this.$t(v.label_key || `backup.ver.${v.kind}`, { at: this.fmt.dateTime(v.time) })
		},
		meta(v) {
			const parts = []
			if (v.files) parts.push(this.$tc('backup.ver.files', v.files, { count: this.fmt.number(v.files) }))
			if (v.bytes) parts.push(this.fmt.bytes(v.bytes))
			return parts.join(' · ')
		},
		onKeydown(e) {
			const ids = this.versions.map(v => v.id)
			const cur = Math.max(0, ids.indexOf(this.value || this.focusId))
			let next = -1
			if (e.key === 'ArrowDown' || e.key === 'ArrowRight') next = (cur + 1) % ids.length
			else if (e.key === 'ArrowUp' || e.key === 'ArrowLeft') next = (cur - 1 + ids.length) % ids.length
			else if (e.key === 'Home') next = 0
			else if (e.key === 'End') next = ids.length - 1
			else if (e.key === ' ' || e.key === 'Enter') {
				const id = e.target && e.target.dataset && e.target.dataset.id
				if (id) {
					e.preventDefault()
					this.$emit('input', id)
				}
				return
			}
			if (next < 0) return
			e.preventDefault()
			this.$emit('input', ids[next])
			this.$nextTick(() => {
				const el = this.$refs.group && this.$refs.group.querySelector(`[data-id="${CSS.escape(ids[next])}"]`)
				if (el) el.focus()
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-versions {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	p {
		margin: 0;
	}
}
.bk-versions-list {
	list-style: none;
	margin: 0;
	padding: 0;
	display: flex;
	flex-direction: column;
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-card);
	background: var(--theme-card-bg);
	overflow: hidden;
}
.bk-version {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: var(--space-2) var(--space-3);
	min-height: 44px;
	padding: var(--space-2) var(--space-3);
	font-size: var(--font-sm);
	color: var(--theme-text-primary);
	& + & {
		border-top: 1px solid var(--theme-table-divider);
	}
	&.is-radio {
		cursor: pointer;
		&:hover {
			background: var(--theme-card-hover);
		}
		&[aria-checked='true'] {
			background: var(--theme-card-selected);
		}
		&:focus {
			outline: none;
		}
		&:focus-visible {
			outline: 2px solid var(--color-primary-fg);
			outline-offset: -2px;
		}
	}
}
.bk-version-label {
	flex: 1 1 14rem;
	min-width: 0;
	overflow-wrap: anywhere;
}
.bk-version-meta {
	color: var(--theme-text-secondary);
	font-variant-numeric: tabular-nums;
}
.bk-radio-dot {
	flex-shrink: 0;
	width: 1rem;
	height: 1rem;
	border-radius: 50%;
	border: 2px solid var(--theme-input-border);
	background: var(--theme-card-bg);
}
[aria-checked='true'] > .bk-radio-dot {
	border-color: var(--color-primary);
	box-shadow: inset 0 0 0 3px var(--theme-card-bg);
	background: var(--color-primary);
}
</style>
