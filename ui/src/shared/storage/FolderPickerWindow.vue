<!-- src/shared/storage/FolderPickerWindow.vue -->
<!-- Folder chooser inside one location (spec §12.4), browsing with
     GET /v1/backup/locations/browse, so it works the same for drives, USB,
     network shares and cloud accounts and never climbs above the
     location's root. A real desktop window, opened with
     backupWindow(t, 'folderPicker', { endpoint, startPath, allowCreate, onSelect }).
     onSelect(endpoint) gets the endpoint with the chosen sub_path. A new
     folder is only a name here: the backup creates it on its first run. -->
<template>
	<div class="folder-picker" @keydown="onWindowKeydown">
		<nav class="fp-crumbs" :aria-label="$t('backup.folder.path_label')">
			<ol>
				<li>
					<button type="button" class="fp-crumb" :aria-current="!path ? 'location' : null" @click="navigate('')">
						<b-icon :icon="rootIcon" custom-size="mdi-16px" aria-hidden="true"></b-icon>
						<span>{{ rootLabel }}</span>
					</button>
				</li>
				<li v-for="c in crumbs" :key="c.path">
					<span class="fp-sep" aria-hidden="true">›</span>
					<button type="button" class="fp-crumb" :aria-current="c.path === path ? 'location' : null" @click="navigate(c.path)">{{ c.name }}</button>
				</li>
			</ol>
		</nav>

		<div class="fp-list" :aria-busy="loading ? 'true' : 'false'">
			<p v-if="loading" class="fp-status" role="status">{{ $t('backup.loc.loading') }}</p>
			<div v-else-if="error" class="fp-status" role="alert">
				<p class="fp-error-title">{{ error.title }}</p>
				<p>{{ error.cause }}</p>
				<button type="button" class="fp-secondary" @click="load">{{ $t('backup.action.try_again') }}</button>
			</div>
			<p v-else-if="!folders.length" class="fp-status" role="status">{{ $t('backup.folder.empty') }}</p>
			<ul v-else ref="listbox" class="fp-rows" role="listbox" :aria-label="$t('backup.folder.list_label')" :aria-activedescendant="activeId"
				tabindex="0" @keydown="onListKeydown" @focus="ensureActive">
				<li v-for="(f, i) in folders" :id="rowId(i)" :key="f.name" role="option" class="fp-row"
					:aria-selected="i === selected ? 'true' : 'false'" :class="{ 'is-active': i === active, 'is-selected': i === selected }"
					@click="select(i)" @dblclick="open(f)">
					<b-icon :icon="i === selected ? 'check-circle' : 'folder'" custom-size="mdi-18px" class="fp-glyph" aria-hidden="true"></b-icon>
					<span class="fp-name one-line">{{ f.name }}</span>
					<!-- Mouse shortcut only: Enter / Right arrow open the folder
					     from the keyboard, so this is no (nested) control. -->
					<span class="fp-enter" aria-hidden="true" :title="$t('backup.folder.open', { name: f.name })" @click.stop="open(f)">
						<b-icon icon="chevron-right" custom-size="mdi-18px" aria-hidden="true"></b-icon>
					</span>
				</li>
			</ul>
			<p v-if="truncated" class="fp-note" role="note">{{ $t('backup.folder.truncated') }}</p>
		</div>

		<form v-if="creating" class="fp-new" @submit.prevent="addNewFolder">
			<label :for="newId" class="fp-new-label">{{ $t('backup.folder.new_name') }}</label>
			<input :id="newId" ref="newName" v-model="newName" class="fp-input" spellcheck="false" maxlength="200" autocomplete="off"
				:aria-invalid="newNameError ? 'true' : 'false'" :aria-describedby="newId + '-hint'" @keydown.esc.stop.prevent="creating = false" />
			<button type="submit" class="fp-primary" :disabled="!newName.trim()">{{ $t('backup.folder.add') }}</button>
			<button type="button" class="fp-secondary" @click="creating = false">{{ $t('Cancel') }}</button>
			<p :id="newId + '-hint'" class="fp-hint" :class="{ 'is-error': newNameError }">
				{{ newNameError ? $t('backup.folder.bad_name') : $t('backup.folder.created_on_run') }}
			</p>
		</form>

		<footer class="fp-foot">
			<button v-if="allowCreate && !creating" type="button" class="fp-secondary" @click="startCreate">
				<b-icon icon="folder-plus-outline" custom-size="mdi-18px" aria-hidden="true"></b-icon><span>{{ $t('backup.folder.new') }}</span>
			</button>
			<p class="fp-target one-line" :title="targetText" aria-live="polite">
				<span class="sr-only">{{ $t('backup.folder.will_use') }}</span>{{ targetText }}
			</p>
			<button type="button" class="fp-secondary" @click="closeWindow">{{ $t('Cancel') }}</button>
			<button ref="choose" type="button" class="fp-primary one-line" :disabled="loading && !pendingNew" @click="choose">{{ useLabel }}</button>
		</footer>
	</div>
</template>

<script>
import backup from '@/service/backup'
import { explainError } from '@/apps/backup/messages'
import { windowFocusMixin } from './windowFocus'
import { cleanSubPath, joinSubPath, parentSubPath, validFolderName, locationIcon, pickerTarget } from './locations'

let uid = 0

export default {
	name: 'FolderPickerWindow',
	mixins: [windowFocusMixin],
	props: {
		winId: { type: String, default: '' },
		// { kind, ref_id, match?, label, sub_path? } - browsed from its root
		endpoint: { type: Object, required: true },
		// Folder to open first, relative to the location root
		startPath: { type: String, default: '' },
		allowCreate: { type: Boolean, default: false },
		onSelect: { type: Function, default: null },
		returnFocus: { type: Function, default: null }
	},
	data() {
		const id = `fp-${++uid}`
		return {
			idBase: id,
			newId: `${id}-new`,
			path: '',
			entries: [],
			truncated: false,
			loading: false,
			error: null,
			// Keyboard cursor in the list (aria-activedescendant)
			active: -1,
			// The folder clicked (or picked with Space): "Use" takes it
			// instead of the folder being browsed. Selection doesn't follow
			// the cursor, so arrowing through the list never changes what
			// "Use" does.
			selected: -1,
			creating: false,
			newName: '',
			newNameError: false,
			// A folder name added with "New folder" (not created yet)
			pendingNew: ''
		}
	},
	computed: {
		rootLabel() {
			return this.endpoint.label || this.endpoint.ref_id
		},
		// The endpoint's kind (cloud, USB, share...) - it has no system_disk
		// flag, so the system disk shows as a plain drive here.
		rootIcon() {
			return locationIcon({ kind: this.endpoint.kind })
		},
		crumbs() {
			const out = []
			let acc = ''
			for (const seg of this.path.split('/').filter(Boolean)) {
				acc = acc ? `${acc}/${seg}` : seg
				out.push({ name: seg, path: acc })
			}
			return out
		},
		folders() {
			return this.entries.filter(e => e.dir && !e.name.startsWith('.')).sort((a, b) => a.name.localeCompare(b.name))
		},
		activeId() {
			return this.active >= 0 && this.active < this.folders.length ? this.rowId(this.active) : null
		},
		selectedFolder() {
			return this.selected >= 0 && !this.pendingNew ? this.folders[this.selected] || null : null
		},
		target() {
			return pickerTarget(this.path, this.selectedFolder ? this.selectedFolder.name : '', this.pendingNew)
		},
		useLabel() {
			return this.selectedFolder ? this.$t('backup.folder.use_name', { name: this.selectedFolder.name }) : this.$t('backup.folder.use')
		},
		targetText() {
			const where = this.target ? `${this.rootLabel} › ${this.target}` : this.rootLabel
			return this.pendingNew ? this.$t('backup.folder.new_target', { path: where }) : where
		}
	},
	async created() {
		// Open the start folder, or the closest parent of it that exists
		// (a proposed job folder usually doesn't exist yet).
		let p = cleanSubPath(this.startPath).path
		for (;;) {
			this.path = p
			const ok = await this.load()
			if (ok || !p || !this.error || this.error.code !== 'not_found') break
			p = parentSubPath(p)
		}
		this.$nextTick(() => this.focusInitial())
	},
	mounted() {
		// "Use this folder" is disabled while the first listing loads:
		// hold focus on the location crumb until focusInitial() runs.
		this.focusFirst(this.$el.querySelector('.fp-crumb'))
	},
	methods: {
		// Once the first listing is in: the folder list (arrow keys), else
		// "Use this folder". Only while focus is still in this window or
		// nowhere (the picker that opened this one may have handed focus
		// back to the wizard as it closed).
		focusInitial() {
			if (this._isDestroyed || !this.$el) return
			const a = document.activeElement
			const inOtherWindow = a && a !== document.body && !this.$el.contains(a) && a.closest && a.closest('.folder-picker, .storage-picker')
			if (inOtherWindow) return
			this.focusFirst(this.$refs.listbox || this.$refs.choose)
		},
		rowId(i) {
			return `${this.idBase}-row-${i}`
		},
		async load() {
			this.loading = true
			this.error = null
			this.pendingNew = ''
			try {
				const res = await backup.browseLocation({ kind: this.endpoint.kind, ref_id: this.endpoint.ref_id, sub_path: '', path: this.path, dirsOnly: true })
				this.entries = (res && res.entries) || []
				this.truncated = !!(res && res.truncated)
				this.active = this.folders.length ? 0 : -1
				this.selected = -1
				return true
			} catch (e) {
				this.entries = []
				this.truncated = false
				this.selected = -1
				this.error = { ...explainError(this.$t.bind(this), e.code), code: e.code }
				return false
			} finally {
				this.loading = false
			}
		},
		async navigate(p) {
			this.path = cleanSubPath(p).path
			this.creating = false
			await this.load()
			this.$nextTick(() => this.$refs.listbox && this.$refs.listbox.focus())
		},
		open(f) {
			this.navigate(joinSubPath(this.path, f.name))
		},
		// select toggles a row as the folder "Use" takes.
		select(i) {
			this.active = i
			this.selected = this.selected === i ? -1 : i
			this.pendingNew = ''
		},
		ensureActive() {
			if (this.active < 0 && this.folders.length) this.active = 0
		},
		scrollActive() {
			this.$nextTick(() => {
				const el = this.activeId && document.getElementById(this.activeId)
				if (el && el.scrollIntoView) el.scrollIntoView({ block: 'nearest' })
			})
		},
		// Listbox keys: arrows move, Space selects, Enter/Right opens,
		// Backspace/Left goes up, Home/End jump.
		onListKeydown(e) {
			const n = this.folders.length
			switch (e.key) {
				case 'ArrowDown':
					this.active = Math.min(n - 1, this.active + 1)
					break
				case 'ArrowUp':
					this.active = Math.max(0, this.active - 1)
					break
				case 'Home':
					this.active = n ? 0 : -1
					break
				case 'End':
					this.active = n - 1
					break
				case ' ':
					if (this.active >= 0) this.select(this.active)
					break
				case 'Enter':
				case 'ArrowRight':
					if (this.folders[this.active]) this.open(this.folders[this.active])
					break
				case 'Backspace':
				case 'ArrowLeft':
					if (!this.path) return
					this.navigate(parentSubPath(this.path))
					break
				default:
					return
			}
			e.preventDefault()
			this.scrollActive()
		},
		startCreate() {
			this.creating = true
			this.newName = ''
			this.newNameError = false
			this.focusFirst('newName')
		},
		addNewFolder() {
			const name = this.newName.trim()
			if (!validFolderName(name)) {
				this.newNameError = true
				return
			}
			const existing = this.folders.find(f => f.name === name)
			this.creating = false
			if (existing) {
				this.navigate(joinSubPath(this.path, name))
				return
			}
			this.pendingNew = name
			this.selected = -1
			this.focusFirst('choose')
		},
		choose() {
			const ep = { ...this.endpoint, sub_path: this.target }
			if (ep.match) ep.match = { ...ep.match }
			this.closeWindow()
			if (typeof this.onSelect === 'function') this.onSelect(ep)
		}
	}
}
</script>

<style lang="scss" scoped>
@import './picker-common.scss';

.folder-picker {
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

.fp-crumbs {
	flex-shrink: 0;
	padding: var(--space-2) var(--space-3);
	border-bottom: 1px solid var(--theme-card-border);

	ol {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 0.15rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	li {
		display: inline-flex;
		align-items: center;
		min-width: 0;
	}
}

.fp-crumb {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	min-height: 2rem;
	max-width: 14rem;
	padding: 0 var(--space-2);
	border: none;
	border-radius: var(--radius-xs);
	background: transparent;
	color: var(--theme-text-secondary);
	font-family: inherit;
	font-size: var(--font-sm);
	cursor: pointer;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;

	&:hover {
		background: var(--theme-card-hover);
		color: var(--theme-text-primary);
	}
	&[aria-current='location'] {
		font-weight: 600;
		color: var(--theme-text-primary);
	}
	&:focus-visible {
		@include picker-focus-ring;
		outline-offset: 0;
	}
	@media (pointer: coarse) {
		min-height: 44px;
	}
}

.fp-sep {
	color: var(--theme-text-muted);
	padding: 0 0.1rem;
}

.fp-list {
	position: relative;
	flex: 1 1 auto;
	min-height: 0;
	overflow-y: auto;
	padding: var(--space-2);
}

.fp-rows {
	margin: 0;
	padding: 0;
	list-style: none;
	border-radius: var(--radius-sm);

	&:focus-visible {
		@include picker-focus-ring;
		outline-offset: 0;
	}
}

.fp-row {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	min-height: 2.25rem;
	padding: 0 var(--space-2);
	border-radius: var(--radius-sm);
	font-size: var(--font-sm);
	cursor: pointer;
	user-select: none;

	.fp-name {
		flex: 1 1 auto;
		min-width: 0;
	}
	&:hover {
		background: var(--theme-card-hover);
	}
	// The keyboard cursor: an outline while the list has focus.
	&.is-active {
		box-shadow: inset 0 0 0 1px var(--theme-card-border);
	}
	.fp-rows:focus-visible &.is-active {
		box-shadow: inset 0 0 0 2px var(--color-primary-fg);
	}
	// The chosen folder.
	&.is-selected {
		background: var(--theme-card-selected);
		box-shadow: inset 0 0 0 1px var(--color-primary-fg);

		.fp-glyph {
			color: var(--color-primary-fg);
		}
	}
	@media (pointer: coarse) {
		min-height: 44px;
	}
}

.fp-glyph {
	color: var(--color-primary-fg);
}

.fp-enter {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 2rem;
	height: 2rem;
	border: none;
	border-radius: var(--radius-xs);
	background: transparent;
	color: var(--theme-text-muted);
	cursor: pointer;

	&:hover {
		color: var(--theme-text-primary);
		background: var(--theme-card-hover);
	}
}

.fp-status {
	padding: var(--space-8) var(--space-4);
	text-align: center;
	font-size: var(--font-sm);
	color: var(--theme-text-secondary);

	p + p,
	p + button {
		margin-top: var(--space-2);
	}
}

.fp-error-title {
	font-weight: 600;
	color: var(--color-danger-fg);
}

.fp-note {
	margin: var(--space-2);
	font-size: var(--font-xs);
	color: var(--theme-text-muted);
}

.fp-new {
	flex-shrink: 0;
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-4);
	border-top: 1px solid var(--theme-card-border);
}

.fp-new-label {
	width: 100%;
	font-size: var(--font-xs);
	font-weight: 600;
	color: var(--theme-text-secondary);
}

.fp-input {
	@include picker-input;
	flex: 1 1 10rem;
	min-width: 0;
}

.fp-hint {
	width: 100%;
	margin: 0;
	font-size: var(--font-xs);
	color: var(--theme-text-muted);

	&.is-error {
		color: var(--color-danger-fg);
	}
}

.fp-foot {
	flex-shrink: 0;
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-4);
	padding-bottom: calc(var(--space-3) + env(safe-area-inset-bottom, 0px));
	border-top: 1px solid var(--theme-card-border);
}

.fp-target {
	flex: 1 1 8rem;
	min-width: 0;
	margin: 0;
	font-size: var(--font-xs);
	color: var(--theme-text-secondary);
}

.fp-primary {
	@include picker-primary-button;
}

.fp-secondary {
	@include picker-secondary-button;
}
</style>
