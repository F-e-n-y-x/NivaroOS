<!-- src/apps/download-station/torrent/DsTorrentSettings.vue -->
<!-- Torrent settings (Settings > Torrents), modelled on qBittorrent's.
     Every change is saved at once as a partial {torrent: {...}} patch; the
     sidecar validates it and root-checks every folder. What the running
     engine can't do is greyed out (info.unsupported). -->
<template>
	<div v-if="t">
		<h3 class="setting-card-title">{{ $t('Torrent engine') }}</h3>
		<div class="setting-card">
			<div class="setting-row engine-row">
				<b-icon class="row-icon" icon="engine-outline" custom-size="mdi-20px"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Engine') }}</div>
					<div class="setting-desc">{{ engineDesc }}</div>
					<div class="segmented-control engine-pick" role="radiogroup" :aria-label="$t('Engine')">
						<button v-for="e in engines" :key="e.id" class="segmented-option" role="radio" :aria-checked="engine === e.id ? 'true' : 'false'"
							:class="{ active: engine === e.id }" :disabled="e.disabled" :title="e.disabled ? $t('qbittorrent-nox is not installed') : ''" @click="setEngine(e.id)">
							{{ $t(e.label) }}
						</button>
					</div>
					<p class="ds-hint">{{ $t('Torrents stay with the engine they were added to.') }}</p>
				</div>
			</div>
			<div v-if="engine === 'external'" class="setting-row">
				<b-icon class="row-icon" icon="server-network" custom-size="mdi-20px"></b-icon>
				<div class="row-label ext-form">
					<label class="ds-field-label" for="ts-ext-url">{{ $t('Address of your qBittorrent Web UI') }}</label>
					<input id="ts-ext-url" v-model="ext.url" class="ds-input" spellcheck="false" placeholder="http://192.168.1.10:8080" />
					<div class="ext-creds">
						<input v-model="ext.user" class="ds-input" spellcheck="false" autocomplete="off" :placeholder="$t('Username')" :aria-label="$t('Username')" />
						<input v-model="ext.pass" class="ds-input" type="password" autocomplete="new-password" :placeholder="t.external_password_set ? $t('Password (saved)') : $t('Password')" :aria-label="$t('Password')" />
						<button class="ds-primary-btn" :disabled="!ext.url" @click="saveExternal">{{ $t('Connect') }}</button>
					</div>
				</div>
			</div>
		</div>

		<template v-if="managed">
			<h3 class="setting-card-title">{{ $t('Torrent folders') }}</h3>
			<div class="setting-card">
				<div class="setting-row">
					<b-icon class="row-icon" icon="folder-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Save folder') }}</div>
						<div class="setting-desc mono">{{ t.save_dir }}</div>
					</div>
					<div class="row-control"><button class="ds-secondary-btn" @click="pick('save_dir', $t('Save folder'))">{{ $t('Change') }}</button></div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="folder-clock-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Keep incomplete torrents in') }}</div>
						<div class="setting-desc mono">{{ t.incomplete_dir || $t('Off - they download straight into the save folder') }}</div>
					</div>
					<div class="row-control">
						<button v-if="t.incomplete_dir" class="ds-secondary-btn" @click="save({ incomplete_dir: '' })">{{ $t('Off') }}</button>
						<button class="ds-secondary-btn" @click="pick('incomplete_dir', $t('Incomplete torrents folder'))">{{ $t('Choose') }}</button>
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="folder-eye-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Watch folder') }}</div>
						<div class="setting-desc mono">{{ t.watch_dir || $t('Off - .torrent files saved here are added automatically') }}</div>
					</div>
					<div class="row-control">
						<button v-if="t.watch_dir" class="ds-secondary-btn" @click="save({ watch_dir: '' })">{{ $t('Off') }}</button>
						<button class="ds-secondary-btn" @click="pick('watch_dir', $t('Watch folder'))">{{ $t('Choose') }}</button>
					</div>
				</div>
			</div>
		</template>

		<h3 class="setting-card-title">{{ $t('Categories') }}</h3>
		<div class="setting-card">
			<div v-for="(c, i) in t.categories" :key="i" class="setting-row">
				<b-icon class="row-icon" icon="tag-outline" custom-size="mdi-20px"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ c.name }}</div>
					<div v-if="managed" class="setting-desc mono">{{ c.dir || `${t.save_dir}/${c.name}` }}</div>
				</div>
				<div class="row-control">
					<button v-if="managed" class="ds-secondary-btn" @click="pickCategory(i)">{{ $t('Folder') }}</button>
					<button class="ds-icon-btn is-danger" :aria-label="$t('Remove {name}', { name: c.name })" @click="removeCategory(i)"><b-icon icon="close" custom-size="mdi-18px"></b-icon></button>
				</div>
			</div>
			<div class="setting-row">
				<b-icon class="row-icon" icon="tag-plus-outline" custom-size="mdi-20px"></b-icon>
				<div class="row-label">
					<input v-model="newCategory" class="ds-input cat-input" maxlength="64" :placeholder="$t('New category, e.g. Movies')" :aria-label="$t('New category')" @keyup.enter="addCategory" />
				</div>
				<div class="row-control"><button class="ds-secondary-btn" :disabled="!newCategory.trim()" @click="addCategory">{{ $t('Add') }}</button></div>
			</div>
		</div>

		<template v-if="managed">
			<h3 class="setting-card-title">{{ $t('Torrent speed') }}</h3>
			<div class="setting-card">
				<div class="setting-row">
					<b-icon class="row-icon" icon="speedometer" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Speed limits') }}</div>
						<div class="setting-desc">{{ $t('KiB/s, 0 = unlimited.') }}</div>
					</div>
					<div class="row-control limits">
						<label>↓ <input v-model="kib.dl_limit" class="ds-input num" type="number" min="0" :aria-label="$t('Download limit')" @change="saveKiB('dl_limit')" /></label>
						<label>↑ <input v-model="kib.up_limit" class="ds-input num" type="number" min="0" :aria-label="$t('Upload limit')" @change="saveKiB('up_limit')" /></label>
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="speedometer-slow" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Alternative speed limits') }}</div>
						<div class="setting-desc">{{ $t('Used while switched on, or during the schedule.') }}</div>
					</div>
					<div class="row-control limits">
						<label>↓ <input v-model="kib.alt_dl_limit" class="ds-input num" type="number" min="0" :aria-label="$t('Alternative download limit')" @change="saveKiB('alt_dl_limit')" /></label>
						<label>↑ <input v-model="kib.alt_up_limit" class="ds-input num" type="number" min="0" :aria-label="$t('Alternative upload limit')" @change="saveKiB('alt_up_limit')" /></label>
						<b-switch :value="t.alt_speed" :aria-label="$t('Use alternative speed limits now')" @input="v => save({ alt_speed: v })"></b-switch>
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="calendar-clock" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Schedule the alternative limits') }}</div>
						<div v-if="t.schedule" class="sched">
							<input v-model="t.schedule_from" class="ds-input" type="time" :aria-label="$t('From')" @change="save({ schedule_from: t.schedule_from })" />
							<span>–</span>
							<input v-model="t.schedule_to" class="ds-input" type="time" :aria-label="$t('To')" @change="save({ schedule_to: t.schedule_to })" />
							<div class="select is-small">
								<select v-model="t.schedule_days" :aria-label="$t('Days')" @change="save({ schedule_days: t.schedule_days })">
									<option value="every">{{ $t('Every day') }}</option>
									<option value="weekdays">{{ $t('Weekdays') }}</option>
									<option value="weekends">{{ $t('Weekends') }}</option>
								</select>
							</div>
						</div>
					</div>
					<div class="row-control"><b-switch :value="t.schedule" :aria-label="$t('Schedule the alternative limits')" @input="v => save({ schedule: v })"></b-switch></div>
				</div>
			</div>

			<h3 class="setting-card-title">{{ $t('Queue and seeding') }}</h3>
			<div class="setting-card">
				<div class="setting-row">
					<b-icon class="row-icon" icon="tray-full" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Torrent queueing') }}</div>
						<div class="setting-desc">{{ $t('At most this many active downloads, uploads and torrents (0 = no limit); the rest wait.') }}</div>
						<div v-if="t.queueing" class="limits queue">
							<label>{{ $t('Downloads') }} <input v-model.number="t.max_active_downloads" class="ds-input num" type="number" min="0" @change="save({ max_active_downloads: t.max_active_downloads })" /></label>
							<label>{{ $t('Uploads') }} <input v-model.number="t.max_active_uploads" class="ds-input num" type="number" min="0" @change="save({ max_active_uploads: t.max_active_uploads })" /></label>
							<label>{{ $t('Torrents') }} <input v-model.number="t.max_active_torrents" class="ds-input num" type="number" min="0" @change="save({ max_active_torrents: t.max_active_torrents })" /></label>
						</div>
					</div>
					<div class="row-control"><b-switch :value="t.queueing" :aria-label="$t('Torrent queueing')" @input="v => save({ queueing: v })"></b-switch></div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="sprout-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Stop seeding at') }}</div>
						<div class="setting-desc">{{ $t('A ratio and/or a seeding time in minutes (0 = off).') }}</div>
						<div class="limits queue">
							<label>{{ $t('Ratio') }} <input v-model.number="t.ratio_limit" class="ds-input num" type="number" min="0" step="0.1" @change="save({ ratio_limit: t.ratio_limit })" /></label>
							<label>{{ $t('Minutes') }} <input v-model.number="t.seed_time_limit" class="ds-input num" type="number" min="0" @change="save({ seed_time_limit: t.seed_time_limit })" /></label>
							<div class="select is-small">
								<select v-model="t.seed_limit_action" :aria-label="$t('Then')" @change="save({ seed_limit_action: t.seed_limit_action })">
									<option value="pause">{{ $t('then pause the torrent') }}</option>
									<option value="remove">{{ $t('then remove it (files kept)') }}</option>
								</select>
							</div>
						</div>
					</div>
				</div>
			</div>

			<h3 class="setting-card-title">{{ $t('Connection') }}</h3>
			<div class="setting-card">
				<div class="setting-row">
					<b-icon class="row-icon" icon="ethernet" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Listening port') }}</div>
						<div class="setting-desc">{{ $t('The only port torrents open to the internet. UPnP / NAT-PMP forwards it on your router.') }}</div>
					</div>
					<div class="row-control limits">
						<input v-model.number="t.listen_port" class="ds-input num" type="number" min="1024" max="65535" :aria-label="$t('Listening port')" @change="save({ listen_port: t.listen_port })" />
						<b-switch :value="t.upnp" @input="v => save({ upnp: v })">UPnP</b-switch>
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="account-network-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Finding peers') }}</div>
						<div class="toggles">
							<b-switch :value="t.dht" @input="v => save({ dht: v })">DHT</b-switch>
							<b-switch :value="t.pex" @input="v => save({ pex: v })">PeX</b-switch>
							<b-switch :value="t.lsd" :disabled="!can('lsd')" :title="can('lsd') ? '' : $t('Not available with this engine')" @input="v => save({ lsd: v })">{{ $t('Local peers (LSD)') }}</b-switch>
						</div>
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="lock-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Encryption') }}</div>
					</div>
					<div class="row-control">
						<div class="select is-small">
							<select v-model="t.encryption" :aria-label="$t('Encryption')" @change="save({ encryption: t.encryption })">
								<option value="allow">{{ $t('Allow') }}</option>
								<option value="prefer">{{ $t('Prefer') }}</option>
								<option value="require">{{ $t('Require') }}</option>
							</select>
						</div>
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="lan-connect" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Maximum connections') }}</div>
						<div class="setting-desc">{{ $t('In total and per torrent (0 = no limit).') }}</div>
					</div>
					<div class="row-control limits">
						<input v-model.number="t.max_connections" class="ds-input num" type="number" min="0" :disabled="!can('max_connections')" :aria-label="$t('Maximum connections')" @change="save({ max_connections: t.max_connections })" />
						<input v-model.number="t.max_connections_per_torrent" class="ds-input num" type="number" min="0" :aria-label="$t('Maximum connections per torrent')" @change="save({ max_connections_per_torrent: t.max_connections_per_torrent })" />
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="harddisk" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Pre-allocate disk space') }}</div>
						<div class="setting-desc">{{ $t('Reserve the whole size up front (less fragmentation, slower start).') }}</div>
					</div>
					<div class="row-control"><b-switch :value="t.preallocate" :disabled="!can('preallocate')" :aria-label="$t('Pre-allocate disk space')" @input="v => save({ preallocate: v })"></b-switch></div>
				</div>
			</div>
		</template>

		<h3 class="setting-card-title">{{ $t('Public trackers') }}</h3>
		<div class="setting-card">
			<div class="setting-row">
				<b-icon class="row-icon" icon="radar" custom-size="mdi-20px"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Add the best public trackers automatically') }}</div>
					<div class="setting-desc">{{ $t('Fetched from a list and added to public torrents only - never to private ones.') }}</div>
				</div>
				<div class="row-control"><b-switch :value="t.trackers_auto" :aria-label="$t('Add the best public trackers automatically')" @input="v => save({ trackers_auto: v })"></b-switch></div>
			</div>
			<div v-if="t.trackers_auto" class="setting-row">
				<b-icon class="row-icon" icon="link-variant" custom-size="mdi-20px"></b-icon>
				<div class="row-label">
					<label class="ds-field-label" for="ts-trackers-url">{{ $t('Trackers list URL') }}</label>
					<input id="ts-trackers-url" v-model="t.trackers_url" class="ds-input mono" spellcheck="false" @change="save({ trackers_url: t.trackers_url })" />
					<div class="tracker-status">
						<div class="select is-small">
							<select v-model.number="t.trackers_interval_hours" :aria-label="$t('Refresh')" @change="save({ trackers_interval_hours: t.trackers_interval_hours })">
								<option :value="6">{{ $t('Refresh every 6 hours') }}</option>
								<option :value="12">{{ $t('Refresh every 12 hours') }}</option>
								<option :value="24">{{ $t('Refresh daily') }}</option>
								<option :value="168">{{ $t('Refresh weekly') }}</option>
							</select>
						</div>
						<button class="ds-secondary-btn" :disabled="refreshing" @click="refreshTrackers">
							<b-icon :icon="refreshing ? 'loading' : 'refresh'" :custom-class="refreshing ? 'mdi-spin' : ''" custom-size="mdi-16px"></b-icon><span>{{ $t('Refresh now') }}</span>
						</button>
						<span class="ds-hint">{{ trackerSummary }}</span>
					</div>
					<p v-if="trackers && trackers.error" class="ds-error-text">{{ trackers.error }}</p>
				</div>
			</div>
		</div>
	</div>
</template>

<script>
import { downloadSidecar } from '@/api/downloadSidecar'
import { escapeHtml } from '@/utils/escapeHtml'
import { toKiB, fromKiB, supported } from './torrentUtil'

const KIB_FIELDS = ['dl_limit', 'up_limit', 'alt_dl_limit', 'alt_up_limit']

export default {
	name: 'ds-torrent-settings',
	data() {
		return { t: null, info: null, kib: {}, ext: { url: '', user: '', pass: '' }, newCategory: '', trackers: null, refreshing: false }
	},
	computed: {
		engine() {
			return (this.info && this.info.engine) || this.t.engine
		},
		managed() {
			return this.engine !== 'external'
		},
		engines() {
			const installed = !this.info || this.info.qbittorrent
			return [
				{ id: 'qbittorrent', label: 'qBittorrent (recommended)', disabled: !installed },
				{ id: 'external', label: 'My qBittorrent' },
				{ id: 'builtin', label: 'Built-in' }
			]
		},
		engineDesc() {
			switch (this.engine) {
				case 'qbittorrent':
					return this.$t('libtorrent, the fastest engine. Runs only while a torrent is active.')
				case 'external':
					return this.$t('Your own qBittorrent. Its speed, queue and connection settings stay in its own Web UI.')
			}
			return this.$t('A lighter engine inside Download Station, for small boxes.')
		},
		trackerSummary() {
			const tl = this.trackers
			if (!tl || !tl.updated_at || tl.updated_at.startsWith('0001')) return this.$t('Not fetched yet')
			return this.$t('{count} trackers · updated {when}', { count: (tl.trackers || []).length, when: new Date(tl.updated_at).toLocaleString() })
		}
	},
	async created() {
		try {
			const [s, info] = await Promise.all([downloadSidecar.getSettings(), downloadSidecar.listTorrents().catch(() => null)])
			this.info = info
			this.apply(s.torrent)
			this.trackers = await downloadSidecar.trackerList()
		} catch (e) {
			this.toastError(this.$t('Could not load the settings: {error}', { error: e.message }))
		}
	},
	methods: {
		can(key) {
			return supported(this.info && this.info.unsupported, key)
		},
		apply(t) {
			this.t = t
			this.kib = Object.fromEntries(KIB_FIELDS.map(k => [k, toKiB(t[k])]))
			this.ext = { url: t.external_url, user: t.external_user, pass: '' }
		},
		toastError(text) {
			this.$buefy.toast.open({ message: escapeHtml(text), type: 'is-danger' })
		},
		async save(patch) {
			try {
				const s = await downloadSidecar.updateSettings({ torrent: patch })
				this.apply(s.torrent)
				this.$emit('changed')
				if ('engine' in patch || 'external_url' in patch) this.info = await downloadSidecar.listTorrents().catch(() => this.info)
			} catch (e) {
				this.toastError(this.$t('Could not save: {error}', { error: e.message }))
				this.apply({ ...this.t }) // put the controls back
			}
		},
		saveKiB(k) {
			this.save({ [k]: fromKiB(this.kib[k]) })
		},
		setEngine(id) {
			if (id === 'external' && !this.t.external_url) {
				// Needs an address first: just show the form.
				this.info = { ...(this.info || {}), engine: 'external', unsupported: ['all'] }
				return
			}
			this.save({ engine: id })
		},
		saveExternal() {
			const patch = { engine: 'external', external_url: this.ext.url.trim(), external_user: this.ext.user.trim() }
			if (this.ext.pass) patch.external_password = this.ext.pass
			this.save(patch)
		},
		pick(field, title) {
			const id = 'ds-folder-' + Date.now()
			this.$store.commit('OPEN_WINDOW', {
				id,
				title,
				component: 'DsFolderPickerWindow',
				props: { winId: id, startPath: this.t[field] || this.t.save_dir, onSelect: path => this.save({ [field]: path }) },
				width: 460,
				height: 440
			})
		},
		pickCategory(i) {
			const id = 'ds-folder-' + Date.now()
			const cats = this.t.categories.map(c => ({ ...c }))
			this.$store.commit('OPEN_WINDOW', {
				id,
				title: this.$t('Folder for {name}', { name: cats[i].name }),
				component: 'DsFolderPickerWindow',
				props: {
					winId: id,
					startPath: cats[i].dir || this.t.save_dir,
					onSelect: path => {
						cats[i].dir = path
						this.save({ categories: cats })
					}
				},
				width: 460,
				height: 440
			})
		},
		addCategory() {
			const name = this.newCategory.trim()
			if (!name) return
			this.save({ categories: [...this.t.categories, { name, dir: '' }] })
			this.newCategory = ''
		},
		removeCategory(i) {
			this.save({ categories: this.t.categories.filter((_, j) => j !== i) })
		},
		async refreshTrackers() {
			this.refreshing = true
			try {
				this.trackers = await downloadSidecar.refreshTrackerList()
			} catch (e) {
				this.toastError(e.message)
			} finally {
				this.refreshing = false
			}
		}
	}
}
</script>

<style lang="scss" scoped>
@import '../ds-common.scss';

.mono {
	font-family: $family-monospace;
	word-break: break-all;
}

.engine-pick {
	margin: var(--space-2) 0 0;
	flex-wrap: wrap;
}

.ext-form {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.ext-creds,
.sched,
.tracker-status,
.toggles {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--space-2);
	margin-top: var(--space-2);
}

.ext-creds .ds-input {
	flex: 1 1 8rem;
}

.sched .ds-input {
	width: 7.5rem;
}

.limits {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--space-2);

	label {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-xs);
		color: var(--theme-text-secondary, #475569);
	}

	&.queue {
		margin-top: var(--space-2);
	}
}

.num {
	width: 5.5rem;
	text-align: right;
}

.cat-input {
	max-width: 18rem;
}
</style>
