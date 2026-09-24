<!-- src/shared/storage/StoragePickerWindow.vue -->
<!-- "Choose where to save" / "Choose what to back up" (spec §12.4): every
     NivaroOS storage type GET /v1/backup/locations knows - internal drives
     and merge pools, USB drives (also remembered ones that are unplugged),
     network shares and cloud accounts - as one keyboard-first radio list
     grouped by type. A real desktop window, opened with
     backupWindow(t, 'storagePicker', { role, selected, sourceEndpoint, onSelect }).
     onSelect(location) gets the chosen GET /locations entry. -->
<template>
	<div class="storage-picker" @keydown="onWindowKeydown">
		<div class="sp-toolbar">
			<label class="sr-only" :for="searchId">{{ $t('backup.loc.search') }}</label>
			<input :id="searchId" ref="search" v-model="query" class="sp-search" type="search" autocomplete="off" spellcheck="false"
				:placeholder="$t('backup.loc.search')" @keydown.down.prevent="focusRow(0)" @keydown.esc="clearSearch" />
			<button type="button" class="sp-secondary" :disabled="loading" @click="load(true)">
				<b-icon icon="refresh" custom-size="mdi-18px" aria-hidden="true"></b-icon>
				<span>{{ $t('backup.loc.refresh') }}</span>
			</button>
		</div>

		<div class="sp-list" :aria-busy="loading ? 'true' : 'false'">
			<p v-if="loading && !locations.length" class="sp-status" role="status">{{ $t('backup.loc.loading') }}</p>
			<div v-else-if="error" class="sp-status" role="alert">
				<p class="sp-error-title">{{ error.title }}</p>
				<p>{{ error.cause }}</p>
				<button type="button" class="sp-secondary" @click="load(true)">{{ $t('backup.action.try_again') }}</button>
			</div>
			<p v-else-if="!groups.length" class="sp-status" role="status">
				{{ query ? $t('backup.loc.no_match', { query }) : $t('backup.loc.none') }}
			</p>
			<div v-else ref="radiogroup" role="radiogroup" :aria-label="title" :aria-describedby="noteId" @keydown="onListKeydown">
				<div v-for="g in groups" :key="g.id" role="group" :aria-labelledby="searchId + '-' + g.id" class="sp-group">
					<h3 :id="searchId + '-' + g.id" class="sp-group-title">{{ $t(g.titleKey) }}</h3>
					<location-row v-for="loc in g.items" :key="keyOf(loc)" :ref="'row-' + keyOf(loc)" :data-key="keyOf(loc)" :location="loc" :role="role"
						:fmt="fmt" :checked="keyOf(loc) === selectedKey" :disabled="!isSelectable(loc)" :tabindex="keyOf(loc) === rovingKey ? 0 : -1"
						@click="select(loc, true)" @dblclick="select(loc) && choose()"></location-row>
				</div>
			</div>
		</div>

		<div :id="noteId" class="sp-details" aria-live="polite">
			<template v-if="current">
				<p v-if="!current.online && current.kind === 'usb'" class="sp-note tone-info">
					<b-icon icon="information-outline" custom-size="mdi-16px" aria-hidden="true"></b-icon>
					<span>{{ $t('backup.loc.offline_usb_note') }}</span>
				</p>
				<p v-for="w in currentWarnings" :key="'w-' + w" class="sp-note tone-warn">
					<b-icon icon="alert-outline" custom-size="mdi-16px" aria-hidden="true"></b-icon>
					<span>{{ $t('backup.warn.' + w) }}</span>
				</p>
				<p v-if="risk === 'same_disk'" class="sp-note tone-warn">
					<b-icon icon="alert-outline" custom-size="mdi-16px" aria-hidden="true"></b-icon>
					<span>{{ $t('backup.loc.same_disk_note') }}</span>
				</p>
				<ul v-if="currentQuirks.length" class="sp-quirks" :aria-label="$t('backup.loc.quirks_label')">
					<li v-for="q in currentQuirks" :key="'q-' + q">
						<b-icon icon="information-outline" custom-size="mdi-14px" aria-hidden="true"></b-icon>
						<span>{{ $t('backup.quirk.' + q) }}</span>
					</li>
				</ul>
				<label v-if="risk" class="sp-confirm">
					<input v-model="riskAccepted" type="checkbox" />
					<span>{{ $t('backup.loc.use_anyway') }}</span>
				</label>
			</template>
		</div>

		<footer class="sp-foot">
			<div class="sp-foot-links">
				<button type="button" class="sp-link" @click="openSettings('network')">
					<b-icon icon="plus" custom-size="mdi-16px" aria-hidden="true"></b-icon><span>{{ $t('backup.loc.connect_share') }}</span>
				</button>
				<button type="button" class="sp-link" @click="openSettings('cloud')">
					<b-icon icon="plus" custom-size="mdi-16px" aria-hidden="true"></b-icon><span>{{ $t('backup.loc.add_cloud') }}</span>
				</button>
			</div>
			<div class="sp-foot-actions">
				<button type="button" class="sp-secondary" @click="closeWindow">{{ $t('Cancel') }}</button>
				<button type="button" class="sp-primary" :disabled="!canChoose" @click="choose">
					<span>{{ chooseLabel || $t('backup.loc.choose_folder') }}</span>
					<b-icon icon="arrow-right" custom-size="mdi-16px" aria-hidden="true"></b-icon>
				</button>
			</div>
		</footer>
	</div>
</template>

<script>
import backup from '@/service/backup'
import { createFormatter } from '@/apps/backup/format'
import { explainError } from '@/apps/backup/messages'
import LocationRow from './LocationRow.vue'
import { windowFocusMixin } from './windowFocus'
import { groupLocations, locationKey, findLocation, selectable, riskyDestination } from './locations'

let uid = 0

export default {
	name: 'StoragePickerWindow',
	components: { LocationRow },
	mixins: [windowFocusMixin],
	props: {
		winId: { type: String, default: '' },
		// 'source' (what to back up) or 'dest' (where to save)
		role: { type: String, default: 'dest' },
		// The endpoint currently chosen, preselected
		selected: { type: Object, default: null },
		// The job's source, to warn about backing up onto the same disk
		sourceEndpoint: { type: Object, default: null },
		// Primary button text (default "Choose folder →")
		chooseLabel: { type: String, default: '' },
		onSelect: { type: Function, default: null },
		// () => element to focus on close (see windowFocus.js)
		returnFocus: { type: Function, default: null }
	},
	data() {
		const id = `sp-${++uid}`
		return {
			searchId: `${id}-search`,
			noteId: `${id}-details`,
			locations: [],
			loading: false,
			error: null,
			query: '',
			selectedKey: '',
			rovingKey: '',
			riskAccepted: false
		}
	},
	computed: {
		title() {
			return this.role === 'source' ? this.$t('backup.loc.title_source') : this.$t('backup.loc.title_dest')
		},
		fmt() {
			return createFormatter({ locale: this.$i18n && this.$i18n.locale, t: this.$t.bind(this) })
		},
		groups() {
			return groupLocations(this.locations, this.query)
		},
		visible() {
			return this.groups.flatMap(g => g.items)
		},
		current() {
			return this.locations.find(l => this.keyOf(l) === this.selectedKey) || null
		},
		sourceLocation() {
			return this.sourceEndpoint ? findLocation(this.locations, this.sourceEndpoint) : null
		},
		risk() {
			return this.role === 'dest' ? riskyDestination(this.current, this.sourceLocation) : null
		},
		currentWarnings() {
			const w = (this.current && this.current.warnings) || []
			// The system disk warning is the risk note + confirmation below.
			return this.role === 'dest' ? w : w.filter(x => x !== 'system_disk')
		},
		currentQuirks() {
			return this.role === 'dest' ? (this.current && this.current.quirks) || [] : []
		},
		canChoose() {
			return !!this.current && this.isSelectable(this.current) && (!this.risk || this.riskAccepted)
		}
	},
	watch: {
		selectedKey() {
			this.riskAccepted = false
		},
		// Keep a visible row focusable when the search hides the current one.
		visible(list) {
			if (!list.some(l => this.keyOf(l) === this.rovingKey)) this.rovingKey = list.length ? this.keyOf(list[0]) : ''
		}
	},
	created() {
		this.load(false)
	},
	mounted() {
		this.focusFirst('search')
	},
	methods: {
		keyOf(loc) {
			return locationKey(loc)
		},
		// Esc in a non-empty search clears it; an empty one closes as usual.
		clearSearch(e) {
			if (!this.query) return
			e.preventDefault()
			e.stopPropagation()
			this.query = ''
		},
		isSelectable(loc) {
			return selectable(loc, this.role)
		},
		async load(refresh) {
			this.loading = true
			this.error = null
			try {
				const list = await backup.locations(this.role)
				this.locations = Array.isArray(list) ? list : []
				const pre = this.selected ? findLocation(this.locations, this.selected) : null
				if (pre && !this.selectedKey) this.selectedKey = this.keyOf(pre)
				if (!this.rovingKey || !this.locations.some(l => this.keyOf(l) === this.rovingKey)) {
					const first = pre || this.visible[0]
					this.rovingKey = first ? this.keyOf(first) : ''
				}
				if (refresh) this.$nextTick(() => this.focusRow(this.visible.findIndex(l => this.keyOf(l) === this.rovingKey)))
			} catch (e) {
				this.error = explainError(this.$t.bind(this), e.code)
			} finally {
				this.loading = false
			}
		},
		select(loc, fromPointer = false) {
			if (!this.isSelectable(loc)) return false
			this.selectedKey = this.keyOf(loc)
			this.rovingKey = this.selectedKey
			if (fromPointer) this.focusRow(this.visible.indexOf(loc))
			return true
		},
		focusRow(i) {
			const loc = this.visible[i]
			if (!loc) return
			this.rovingKey = this.keyOf(loc)
			this.$nextTick(() => {
				const r = this.$refs['row-' + this.rovingKey]
				const el = Array.isArray(r) ? r[0] && r[0].$el : r && r.$el
				if (el) el.focus()
			})
		},
		// Radio-group keys: arrows move and select (skipping locations that
		// can't be chosen), Home/End jump, Space selects, Enter chooses.
		onListKeydown(e) {
			const list = this.visible
			if (!list.length) return
			const at = Math.max(0, list.findIndex(l => this.keyOf(l) === this.rovingKey))
			const step = dir => {
				for (let i = at + dir; i >= 0 && i < list.length; i += dir) {
					if (this.isSelectable(list[i])) return i
				}
				return at
			}
			let next = null
			switch (e.key) {
				case 'ArrowDown':
				case 'ArrowRight':
					next = step(1)
					break
				case 'ArrowUp':
				case 'ArrowLeft':
					if (at === 0) {
						e.preventDefault()
						this.focusFirst('search')
						return
					}
					next = step(-1)
					break
				case 'Home':
					next = list.findIndex(l => this.isSelectable(l))
					break
				case 'End':
					for (let i = list.length - 1; i >= 0; i--) {
						if (this.isSelectable(list[i])) {
							next = i
							break
						}
					}
					break
				case ' ':
					e.preventDefault()
					this.select(list[at])
					return
				case 'Enter':
					e.preventDefault()
					if (this.select(list[at])) this.choose()
					return
				default:
					return
			}
			e.preventDefault()
			if (next !== null && next >= 0) {
				this.select(list[next])
				this.focusRow(next)
			}
		},
		choose() {
			if (!this.canChoose) return
			const loc = this.current
			// Close first so focus returns to the opener before the opener
			// opens the next window (the folder picker).
			this.closeWindow()
			if (typeof this.onSelect === 'function') this.onSelect(JSON.parse(JSON.stringify(loc)))
		},
		openSettings(section) {
			this.$store.commit('OPEN_WINDOW', {
				id: 'settings', title: this.$t('Settings'), component: 'SettingsApp', width: 760, height: 540, props: { section }
			})
		}
	}
}
</script>

<style lang="scss" scoped>
@import './picker-common.scss';

.storage-picker {
	display: flex;
	flex-direction: column;
	height: 100%;
	min-width: 0;
	background: var(--theme-bg-window);
	color: var(--theme-text-primary);
}

.sr-only {
	position: absolute;
	width: 1px;
	height: 1px;
	overflow: hidden;
	clip: rect(0 0 0 0);
	white-space: nowrap;
}

.sp-toolbar {
	flex-shrink: 0;
	display: flex;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-4);
	border-bottom: 1px solid var(--theme-card-border);
}

.sp-search {
	@include picker-input;
	flex: 1 1 auto;
	min-width: 0;
}

.sp-list {
	position: relative;
	flex: 1 1 auto;
	min-height: 0;
	overflow-y: auto;
	padding: var(--space-2) var(--space-3);
}

.sp-group + .sp-group {
	margin-top: var(--space-3);
}

.sp-group-title {
	margin: var(--space-2) var(--space-2) var(--space-1);
	font-size: var(--font-2xs);
	font-weight: 700;
	letter-spacing: 0.06em;
	text-transform: uppercase;
	color: var(--theme-text-muted);
}

.sp-status {
	padding: var(--space-8) var(--space-4);
	text-align: center;
	font-size: var(--font-sm);
	color: var(--theme-text-secondary);

	p + p,
	p + button {
		margin-top: var(--space-2);
	}
}

.sp-error-title {
	font-weight: 600;
	color: var(--color-danger-fg);
}

.sp-details {
	flex-shrink: 0;
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	max-height: 40%;
	overflow-y: auto;
	padding: 0 var(--space-4);

	&:not(:empty) {
		padding-top: var(--space-3);
		padding-bottom: var(--space-1);
		border-top: 1px solid var(--theme-card-border);
	}
}

.sp-note {
	margin: 0;
	&.tone-warn {
		@include picker-note('warn');
	}
	&.tone-info {
		@include picker-note('info');
	}
}

.sp-quirks {
	margin: 0;
	padding: 0;
	list-style: none;
	font-size: var(--font-xs);
	color: var(--theme-text-secondary);

	li {
		display: flex;
		align-items: center;
		gap: var(--space-1);
	}
}

.sp-confirm {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	min-height: 2rem;
	font-size: var(--font-sm);
	font-weight: 600;
	cursor: pointer;

	input {
		width: 18px;
		height: 18px;
		accent-color: var(--color-primary);
	}
	input:focus-visible {
		@include picker-focus-ring;
	}
}

.sp-foot {
	flex-shrink: 0;
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-4);
	padding-bottom: calc(var(--space-3) + env(safe-area-inset-bottom, 0px));
	border-top: 1px solid var(--theme-card-border);
}

.sp-foot-links,
.sp-foot-actions {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
}

.sp-foot-actions {
	margin-left: auto;
}

.sp-primary {
	@include picker-primary-button;
}

.sp-secondary {
	@include picker-secondary-button;
}

.sp-link {
	@include picker-link-button;
	font-size: var(--font-xs);
}
</style>
