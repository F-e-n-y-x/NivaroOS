<!-- "What do you want to protect?" (spec §12.4 What): the chosen folders
     with Change / Remove, and quick adds for app data, virtual machines and
     the shared /DATA folders (from the locations' folder presets). A copy
     or mirror has one source, so a quick add replaces it; an archive holds
     up to 16. -->
<template>
	<div class="source-picker">
		<ul v-if="sources.length" class="sp-sources" :aria-describedby="describedBy">
			<li v-for="(s, i) in sources" :key="i + ':' + s.kind + ':' + s.ref_id + ':' + s.sub_path" class="sp-source">
				<b-icon :icon="iconFor(s)" custom-size="mdi-20px" class="sp-source-icon" aria-hidden="true"></b-icon>
				<span class="sp-source-main">
					<span class="sp-source-name one-line">{{ shortName(s) }}</span>
					<span class="sp-source-path one-line">{{ pathText(s) }}</span>
				</span>
				<span v-if="statusOf(s)" class="wz-pill" :class="'tone-' + statusOf(s).tone">
					<b-icon :icon="statusOf(s).icon" custom-size="mdi-14px" aria-hidden="true"></b-icon>
					<span>{{ statusOf(s).text }}</span>
				</span>
				<button :id="i === 0 ? fieldDomId : null" type="button" class="wz-secondary sp-btn" :aria-label="$t('backup.wizard.what.change_source', { name: shortName(s) })" @click="$emit('pick', i)">
					{{ $t('backup.wizard.change') }}
				</button>
				<button v-if="sources.length > 1" type="button" class="wz-link sp-btn" :aria-label="$t('backup.wizard.what.remove_source', { name: shortName(s) })"
					:title="$t('backup.wizard.what.remove_source', { name: shortName(s) })" @click="$emit('remove', i)">
					<b-icon icon="close" custom-size="mdi-18px" aria-hidden="true"></b-icon>
				</button>
			</li>
		</ul>
		<button v-else :id="fieldDomId" type="button" class="wz-primary sp-choose" :aria-describedby="describedBy" @click="$emit('pick', null)">
			<b-icon icon="folder-search-outline" custom-size="mdi-18px" aria-hidden="true"></b-icon>
			<span>{{ $t('backup.wizard.what.choose_source') }}</span>
		</button>

		<div class="sp-quick">
			<button v-if="sources.length && canAddMore" type="button" class="wz-secondary" @click="$emit('pick', null)">
				<b-icon icon="plus" custom-size="mdi-16px" aria-hidden="true"></b-icon>
				<span>{{ $t('backup.wizard.what.add_folder') }}</span>
			</button>
			<template v-for="g in quickGroups">
				<span :key="g.id" class="sp-quick-field">
					<label :for="idp + '-quick-' + g.id" class="sr-only">{{ $t('backup.wizard.what.quick.' + g.id) }}</label>
					<select :id="idp + '-quick-' + g.id" class="wz-select" @change="onQuick(g, $event)">
						<option value="" disabled selected>{{ $t('backup.wizard.what.quick.' + g.id) }}</option>
						<option v-for="p in g.items" :key="p.id" :value="p.id">{{ p.label }}</option>
					</select>
				</span>
			</template>
		</div>
		<p v-if="type !== 'archive' && quickGroups.length" class="wz-hint">{{ $t('backup.wizard.what.one_source_hint') }}</p>
	</div>
</template>

<script>
import { presetsOfKind, presetEndpoint } from '../presets'
import { findLocation, locationStatus, locationIcon } from '@/shared/storage/locations'
import { fieldId, describedBy } from '../wizard/fields'
import { MAX_ARCHIVE_SOURCES } from '../wizard/draft'

export default {
	name: 'SourcePicker',
	props: {
		sources: { type: Array, required: true },
		type: { type: String, required: true },
		locations: { type: Array, default: () => [] },
		idp: { type: String, required: true },
		errors: { type: Object, default: () => ({}) },
		fmt: { type: Object, required: true }
	},
	computed: {
		fieldDomId() {
			return fieldId(this.idp, 'sources')
		},
		describedBy() {
			return describedBy(this.idp, 'sources', this.errors)
		},
		canAddMore() {
			return this.type === 'archive' && this.sources.length < MAX_ARCHIVE_SOURCES
		},
		quickGroups() {
			const groups = [
				{ id: 'apps', prefix: 'appdata:' },
				{ id: 'vms', prefix: 'vm:' },
				{ id: 'folders', prefix: 'data:' }
			]
			return groups.map(g => ({ ...g, items: presetsOfKind(this.locations, g.prefix) })).filter(g => g.items.length)
		}
	},
	methods: {
		onQuick(g, e) {
			const p = g.items.find(x => x.id === e.target.value)
			e.target.value = ''
			if (p) this.$emit('add', presetEndpoint(p))
		},
		shortName(s) {
			const tail = s.sub_path ? s.sub_path.split('/').filter(Boolean).pop() : ''
			return tail || s.label || s.ref_id
		},
		pathText(s) {
			const loc = findLocation(this.locations, s)
			const where = (loc && loc.label) || s.label || s.ref_id
			return s.sub_path ? `${where} › ${s.sub_path}` : this.$t('backup.wizard.what.whole_location', { name: where })
		},
		iconFor(s) {
			const p = s.preset || ''
			if (p.startsWith('appdata:')) return 'apps'
			if (p.startsWith('vm:')) return 'monitor'
			const loc = findLocation(this.locations, s)
			return loc ? locationIcon(loc) : 'folder-outline'
		},
		statusOf(s) {
			const loc = findLocation(this.locations, s)
			if (!loc || loc.online) return null
			const st = locationStatus(loc, 'source')
			const args = { ...st.args }
			if (args.at) args.at = this.fmt.relative(args.at)
			return { tone: st.tone, icon: st.icon, text: this.$t(st.key, args) }
		}
	}
}
</script>

<style lang="scss" scoped>
@import './wizard-form.scss';

.source-picker {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	min-width: 0;
}

.sp-sources {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	margin: 0;
	padding: 0;
	list-style: none;
}

.sp-source {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--space-2) var(--space-3);
	padding: var(--space-2) var(--space-3);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-card);
	background: var(--theme-card-bg);
}

.sp-source-icon {
	flex-shrink: 0;
	color: var(--color-primary-fg);
}

.sp-source-main {
	flex: 1 1 10rem;
	min-width: 0;
	display: flex;
	flex-direction: column;
}

.sp-source-name {
	font-size: var(--font-sm);
	font-weight: 600;
	color: var(--theme-text-primary);
}

.sp-source-path {
	font-size: var(--font-xs);
	color: var(--theme-text-muted);
}

.sp-btn {
	min-height: 2rem;
}

.sp-choose {
	align-self: flex-start;
}

.sp-quick {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
}

.sp-quick-field {
	display: inline-flex;
	min-width: 0;
	max-width: 100%;

	.wz-select {
		max-width: 14rem;
	}
}
</style>
