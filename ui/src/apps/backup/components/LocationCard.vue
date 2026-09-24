<!-- The chosen destination on the Where step (spec §12.4): the location with
     its status (icon + text), free space and a Change button, plus the
     folder inside it, proposed as "NivaroOS Backups/<job name>" and
     created by the first run. -->
<template>
	<div class="location-card">
		<button v-if="!dest" :id="destFieldId" type="button" class="wz-primary lc-choose" :aria-describedby="destDescribedBy" @click="$emit('choose')">
			<b-icon icon="folder-search-outline" custom-size="mdi-18px" aria-hidden="true"></b-icon>
			<span>{{ $t('backup.wizard.where.choose') }}</span>
		</button>
		<template v-else>
			<div class="lc-card">
				<b-icon :icon="icon" custom-size="mdi-24px" class="lc-icon" aria-hidden="true"></b-icon>
				<span class="lc-main">
					<span class="lc-name one-line">{{ dest.label || dest.ref_id }}</span>
					<span class="lc-meta">
						<span>{{ kindText }}</span>
						<span v-if="spaceText">{{ spaceText }}</span>
					</span>
				</span>
				<span v-if="status" class="wz-pill" :class="'tone-' + status.tone">
					<b-icon :icon="status.icon" custom-size="mdi-14px" aria-hidden="true"></b-icon>
					<span>{{ status.text }}</span>
				</span>
				<button :id="destFieldId" type="button" class="wz-secondary" :aria-describedby="destDescribedBy"
					:aria-label="$t('backup.wizard.where.change_aria', { name: dest.label || dest.ref_id })" @click="$emit('choose')">
					{{ $t('backup.wizard.change') }}
				</button>
			</div>

			<div class="lc-folder">
				<label :for="pathFieldId" class="wz-label">{{ $t('backup.wizard.where.folder') }}</label>
				<div class="wz-row">
					<input :id="pathFieldId" class="wz-input lc-path" type="text" spellcheck="false" autocomplete="off" :value="dest.sub_path"
						:aria-invalid="errors['dest.sub_path'] ? 'true' : 'false'" :aria-describedby="pathDescribedBy"
						:placeholder="$t('backup.wizard.where.folder_root')" @input="$emit('sub-path', $event.target.value)" />
					<button type="button" class="wz-secondary" :disabled="browseDisabled" :title="browseDisabled ? $t('backup.wizard.where.browse_offline') : ''"
						@click="$emit('browse')">
						<b-icon icon="folder-open-outline" custom-size="mdi-16px" aria-hidden="true"></b-icon>
						<span>{{ $t('backup.wizard.where.browse') }}</span>
					</button>
				</div>
				<p :id="pathFieldId + '-hint'" class="wz-hint">{{ $t('backup.wizard.where.folder_hint') }}</p>
				<field-error :idp="idp" field="dest.sub_path" :errors="errors"></field-error>
			</div>
		</template>
		<field-error :idp="idp" field="dest" :errors="errors"></field-error>
	</div>
</template>

<script>
import FieldError from './FieldError.vue'
import { fieldId, describedBy } from '../wizard/fields'
import { locationIcon, locationStatus, spaceInfo } from '@/shared/storage/locations'

export default {
	name: 'LocationCard',
	components: { FieldError },
	props: {
		dest: { type: Object, default: null },
		// The GET /locations entry of dest, when known
		location: { type: Object, default: null },
		idp: { type: String, required: true },
		errors: { type: Object, default: () => ({}) },
		fmt: { type: Object, required: true }
	},
	computed: {
		destFieldId() {
			return fieldId(this.idp, 'dest')
		},
		pathFieldId() {
			return fieldId(this.idp, 'dest.sub_path')
		},
		destDescribedBy() {
			return describedBy(this.idp, 'dest', this.errors)
		},
		pathDescribedBy() {
			return describedBy(this.idp, 'dest.sub_path', this.errors, this.pathFieldId + '-hint')
		},
		icon() {
			return this.location ? locationIcon(this.location) : 'folder-outline'
		},
		kindText() {
			const l = this.location
			if (l && l.system_disk) return this.$t('backup.loc.system_disk')
			if (l && l.kind === 'cloud' && l.provider) return this.$t('backup.loc.cloud_provider', { provider: l.provider })
			if (l && l.fstype && l.kind !== 'cloud' && l.kind !== 'smb') return String(l.fstype).replace(/^fuse\./, '').toUpperCase()
			return this.$t('backup.ep.' + this.dest.kind)
		},
		spaceText() {
			const s = spaceInfo(this.location)
			if (!s) return ''
			return s.total ? this.$t('backup.loc.free_of', { free: this.fmt.bytes(s.free), total: this.fmt.bytes(s.total) }) : this.$t('backup.loc.free', { free: this.fmt.bytes(s.free) })
		},
		status() {
			if (!this.location) return null
			const st = locationStatus(this.location, 'dest')
			const args = { ...st.args }
			if (args.at) args.at = this.fmt.relative(args.at)
			return { tone: st.tone, icon: st.icon, text: this.$t(st.key, args) }
		},
		browseDisabled() {
			return !!(this.location && !this.location.online)
		}
	}
}
</script>

<style lang="scss" scoped>
@import './wizard-form.scss';

.location-card {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	min-width: 0;
}

.lc-choose {
	align-self: flex-start;
}

.lc-card {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--space-2) var(--space-3);
	padding: var(--space-3);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-card);
	background: var(--theme-card-bg);
}

.lc-icon {
	flex-shrink: 0;
	color: var(--color-primary-fg);
}

.lc-main {
	flex: 1 1 10rem;
	min-width: 0;
	display: flex;
	flex-direction: column;
}

.lc-name {
	font-size: var(--font-sm);
	font-weight: 600;
	color: var(--theme-text-primary);
}

.lc-meta {
	display: flex;
	flex-wrap: wrap;
	gap: 0 var(--space-2);
	font-size: var(--font-xs);
	color: var(--theme-text-muted);

	> span + span::before {
		content: '·';
		margin-right: var(--space-2);
	}
}

.lc-folder {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.lc-path {
	flex: 1 1 14rem;
}
</style>
