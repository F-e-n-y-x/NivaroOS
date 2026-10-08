<!-- src/apps/download-station/torrent/DsAddTorrentWindow.vue -->
<!-- "Add torrent": magnet links and links to .torrent files (one per line)
     and/or .torrent files, with a save folder, category and the start
     options. The sidecar checks the folder against the storage roots. -->
<template>
	<div class="ds-add-window">
		<div class="add-body scrollbars-light">
			<label class="ds-field-label" for="ds-torrent-sources">{{ $t('Magnet links or links to .torrent files (one per line)') }}</label>
			<textarea id="ds-torrent-sources" ref="input" v-model="text" class="ds-input url-area" rows="4" spellcheck="false" placeholder="magnet:?xt=urn:btih:…"></textarea>

			<div class="file-row">
				<button class="ds-secondary-btn" @click="$refs.file.click()">
					<b-icon icon="file-upload-outline" custom-size="mdi-18px"></b-icon><span>{{ $t('Choose .torrent files') }}</span>
				</button>
				<input ref="file" type="file" class="ds-visually-hidden" accept=".torrent,application/x-bittorrent" multiple @change="pickFiles" />
				<span v-if="!files.length" class="ds-hint">{{ $t('or drop them on the Torrents list') }}</span>
			</div>
			<ul v-if="files.length" class="file-list">
				<li v-for="(f, i) in files" :key="f.name + i">
					<b-icon icon="file-outline" custom-size="mdi-16px"></b-icon><span class="one-line">{{ f.name }}</span>
					<button class="ds-icon-btn" :aria-label="$t('Remove {name}', { name: f.name })" @click="files.splice(i, 1)"><b-icon icon="close" custom-size="mdi-16px"></b-icon></button>
				</li>
			</ul>

			<div v-if="categories.length" class="field">
				<label class="ds-field-label" for="ds-torrent-cat">{{ $t('Category') }}</label>
				<div class="select is-fullwidth">
					<select id="ds-torrent-cat" v-model="category">
						<option value="">{{ $t('None') }}</option>
						<option v-for="c in categories" :key="c.name" :value="c.name">{{ c.name }}</option>
					</select>
				</div>
			</div>

			<div v-if="!external" class="field">
				<label class="ds-field-label" for="ds-torrent-dir">{{ $t('Save to') }}</label>
				<div class="folder-row">
					<input id="ds-torrent-dir" v-model="dir" class="ds-input" spellcheck="false" :placeholder="defaultDir" />
					<button class="ds-secondary-btn" @click="browseFolder">
						<b-icon icon="folder-outline" custom-size="mdi-18px"></b-icon><span>{{ $t('Browse') }}</span>
					</button>
				</div>
			</div>
			<p v-else class="ds-hint">{{ $t('Your qBittorrent saves these in its own folders.') }}</p>

			<div class="checks">
				<b-checkbox v-model="paused">{{ $t('Start paused') }}</b-checkbox>
				<b-checkbox v-model="sequential">{{ $t('Download in sequential order') }}</b-checkbox>
				<b-checkbox v-model="firstLast">{{ $t('Download first and last pieces first') }}</b-checkbox>
			</div>

			<ul v-if="failures.length" class="batch-errors" role="alert">
				<li v-for="(f, i) in failures" :key="i" class="ds-error-text"><span class="one-line mono" :title="f.what">{{ f.what }}</span> {{ f.error }}</li>
			</ul>
		</div>

		<div class="add-foot">
			<button class="ds-secondary-btn" @click="close">{{ $t('Cancel') }}</button>
			<button class="ds-primary-btn" :disabled="!count || submitting" @click="submit">
				<b-icon :icon="submitting ? 'loading' : 'magnet'" :custom-class="submitting ? 'mdi-spin' : ''" custom-size="mdi-18px"></b-icon>
				<span>{{ count > 1 ? $t('Add {count} torrents', { count }) : $t('Add torrent') }}</span>
			</button>
		</div>
	</div>
</template>

<script>
import { downloadSidecar } from '@/api/downloadSidecar'
import { parseSources, isTorrentFile, fileToBase64, magnetName } from './torrentUtil'

export default {
	emits: ['close'],
	name: 'DsAddTorrentWindow',
	props: {
		winId: { type: String, default: '' },
		sources: { type: String, default: '' },
		// [{ name, data (base64) }] - .torrent files dropped on the list.
		torrents: { type: Array, default: () => [] },
		onAdded: { type: Function, default: null }
	},
	data() {
		return {
			text: this.sources,
			files: this.torrents.slice(),
			dir: '',
			saveDir: '',
			category: '',
			categories: [],
			external: false,
			paused: false,
			sequential: false,
			firstLast: false,
			submitting: false,
			failures: []
		}
	},
	computed: {
		links() {
			return parseSources(this.text)
		},
		count() {
			return this.links.length + this.files.length
		},
		// Where it goes when no folder is typed (the sidecar decides the same way).
		defaultDir() {
			const cat = this.categories.find(x => x.name === this.category)
			if (cat) return cat.dir || `${this.saveDir}/${cat.name}`
			return this.saveDir
		}
	},
	async created() {
		try {
			const s = (await downloadSidecar.getSettings()).torrent || {}
			this.categories = s.categories || []
			this.saveDir = s.save_dir || ''
			const info = await downloadSidecar.listTorrents()
			this.external = info && info.engine === 'external'
		} catch (e) {}
	},
	mounted() {
		this.$nextTick(() => this.$refs.input && this.$refs.input.focus())
	},
	methods: {
		async pickFiles(e) {
			for (const f of Array.from(e.target.files || []).filter(isTorrentFile)) {
				this.files.push({ name: f.name, data: await fileToBase64(f) })
			}
			e.target.value = ''
		},
		browseFolder() {
			const id = 'ds-folder-' + Date.now()
			this.$store.commit('OPEN_WINDOW', {
				id,
				title: this.$t('Choose Folder'),
				component: 'DsFolderPickerWindow',
				props: { winId: id, startPath: this.dir || this.defaultDir || '/DATA', onSelect: path => (this.dir = path) },
				width: 460,
				height: 440
			})
		},
		async submit() {
			this.submitting = true
			this.failures = []
			const common = { dir: this.dir.trim(), category: this.category, paused: this.paused, sequential: this.sequential, first_last: this.firstLast }
			const items = [...this.links.map(s => ({ what: magnetName(s), body: { source: s } })), ...this.files.map(f => ({ what: f.name, body: { torrent: f.data }, file: f }))]
			const leftLinks = []
			const leftFiles = []
			// One at a time: a bad one doesn't stop the rest, and only the
			// failed ones stay behind for a retry.
			for (const it of items) {
				try {
					await downloadSidecar.addTorrent({ ...common, ...it.body })
				} catch (e) {
					this.failures.push({ what: it.what, error: e.message })
					if (it.file) leftFiles.push(it.file)
					else leftLinks.push(it.body.source)
				}
			}
			this.submitting = false
			if (this.onAdded) this.onAdded()
			if (!this.failures.length) return this.close()
			this.text = leftLinks.join('\n')
			this.files = leftFiles
		},
		close() {
			const id = this.winId || (this.$parent && this.$parent.win && this.$parent.win.id)
			if (id) this.$store.commit('CLOSE_WINDOW', id)
			this.$emit('close')
		}
	}
}
</script>

<style lang="scss" scoped>
@import '../ds-common.scss';

.ds-add-window {
	display: flex;
	flex-direction: column;
	height: 100%;
	background: var(--theme-bg-window, #f8fafc);
	color: var(--theme-text-primary, #1e293b);
}

.add-body {
	flex: 1 1 auto;
	overflow-y: auto;
	padding: var(--space-5);
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
}

.url-area {
	font-family: $family-monospace;
	font-size: var(--font-xs);
}

.file-row,
.folder-row {
	display: flex;
	align-items: center;
	gap: var(--space-2);
}

.folder-row .ds-secondary-btn {
	height: 2.1rem;
}

.file-list,
.batch-errors {
	margin: 0;
	padding: 0;
	list-style: none;
	display: flex;
	flex-direction: column;
	gap: var(--space-1);

	li {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-width: 0;
		font-size: var(--font-sm);
	}
}

.checks {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.mono {
	font-family: $family-monospace;
}

.add-foot {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-5);
	border-top: 1px solid var(--theme-card-border, rgb(228 233 237));
	background: var(--theme-bg-window, #fff);
}
</style>
