<!-- Phones (mobile plan WP2-11): the phones that back up here with the
     NivaroOS app. A list, and for the selected phone: where its backups go
     (Change location... = storage picker, folder picker, then move or
     start fresh), what each category holds, the schedule the phone
     reports, how long snapshots are kept, a snapshot browser with
     downloads, and revoke / remove. Contract:
     docs/specs/2026-09-30-phone-backup-device-api.md. -->
<template>
	<div class="bk-section bk-phones" :class="{ 'is-narrow': narrow }">
		<div class="bk-section-head">
			<h2>{{ $t('backup.nav.phones') }}</h2>
		</div>

		<p v-if="listError" class="bk-inline-error" role="alert">
			{{ errText(listError) }}
			<button type="button" class="bk-btn is-small" @click="loadList">{{ $t('backup.retry') }}</button>
		</p>

		<div v-if="!listLoading && !devices.length && !listError" class="bk-empty">
			<b-icon icon="cellphone-arrow-down" pack="mdi" custom-size="mdi-48px" aria-hidden="true"></b-icon>
			<p>{{ $t('backup.phones.none') }}</p>
			<p class="bk-secondary">{{ $t('backup.phones.none_hint') }}</p>
		</div>

		<div v-else class="bk-phones-body">
			<ul class="bk-phone-list" :aria-label="$t('backup.phones.list')">
				<li v-for="d in devices" :key="d.id">
					<button
						type="button"
						class="bk-phone-item"
						:class="{ active: d.id === selectedId }"
						:aria-current="d.id === selectedId ? 'true' : null"
						@click="select(d.id)"
					>
						<b-icon :icon="d.platform === 'other' ? 'devices' : 'cellphone'" pack="mdi" custom-size="mdi-24px" aria-hidden="true"></b-icon>
						<span class="bk-phone-item-main">
							<span class="bk-phone-name">{{ d.name }}</span>
							<span class="bk-secondary bk-phone-meta">{{ listMeta(d) }}</span>
						</span>
						<span class="bk-pill" :class="'is-' + tone(d)">{{ $t(stateKey(d)) }}</span>
					</button>
				</li>
			</ul>

			<div v-if="selectedId" class="bk-phone-detail" :aria-busy="loading ? 'true' : null">
				<p v-if="error" class="bk-inline-error" role="alert">
					{{ errText(error) }}
					<button type="button" class="bk-btn is-small" @click="loadDetail">{{ $t('backup.retry') }}</button>
				</p>
				<p v-else-if="!detail" class="bk-secondary" role="status">{{ $t('backup.loading') }}</p>

				<template v-if="detail">
					<!-- Header -->
					<section class="bk-card">
						<div class="bk-phone-head">
							<div>
								<h3 class="bk-card-title">{{ detail.device.name }}</h3>
								<p class="bk-secondary bk-card-text">{{ headMeta }}</p>
							</div>
							<span class="bk-pill" :class="'is-' + tone(detail.device, detail.destination)">{{ $t(stateKey(detail.device, detail.destination)) }}</span>
						</div>
						<p v-if="detail.device.revoked_at" class="bk-inline-error">{{ $t('backup.phones.revoked_note', { when: fmt.dateTime(detail.device.revoked_at) }) }}</p>
						<p v-if="detail.open_sessions.length" class="bk-card-text">
							<b-icon icon="sync" pack="mdi" custom-size="mdi-16px" custom-class="mdi-spin" aria-hidden="true"></b-icon>
							{{ $t('backup.phones.backing_up', { count: fmt.number(detail.open_sessions[0].counts.uploaded) }) }}
						</p>
						<div class="bk-form-actions">
							<button type="button" class="bk-btn is-small" :disabled="busy" @click="verify">{{ $t('backup.phones.verify') }}</button>
							<button v-if="!detail.device.revoked_at" type="button" class="bk-btn is-small" :disabled="busy" @click="revokeAccess">{{ $t('backup.phones.revoke') }}</button>
							<button type="button" class="bk-btn is-small" :disabled="busy" @click="remove">{{ $t('backup.phones.remove') }}</button>
						</div>
						<p v-if="detail.last_verify" class="bk-secondary bk-card-text">{{ verifyText(detail.last_verify) }}</p>
					</section>

					<!-- Location -->
					<section class="bk-card" :aria-labelledby="`${uid}-loc`">
						<h3 :id="`${uid}-loc`" class="bk-card-title">{{ $t('backup.phones.location') }}</h3>
						<p class="bk-card-text">
							<span class="bk-code">{{ detail.destination.path }}</span>
						</p>
						<p class="bk-secondary bk-card-text">{{ locationLine }}</p>
						<p v-if="detail.destination.error_code" class="bk-inline-error" role="alert">
							{{ explain(detail.destination.error_code).title }} - {{ explain(detail.destination.error_code).cause }}
						</p>
						<div v-if="detail.move && detail.move.state === 'moving'" class="bk-card-text" role="status">
							<p>{{ $t('backup.phones.moving', { path: detail.move.target_path }) }}</p>
							<div class="bk-progress" :class="{ 'is-indeterminate': moveRatio === null }">
								<span :style="{ width: moveRatio === null ? null : Math.round(moveRatio * 100) + '%' }"></span>
							</div>
							<p class="bk-secondary">{{ $t('backup.phones.moving_count', { files: fmt.number(detail.move.files), total: fmt.number(detail.move.total_files), bytes: fmt.bytes(detail.move.bytes) }) }}</p>
						</div>
						<p v-else-if="detail.move && detail.move.state === 'failed'" class="bk-inline-error">{{ $t('backup.phones.move_failed', { error: detail.move.error }) }}</p>

						<div v-if="pending" class="bk-phone-mode" role="group" :aria-labelledby="`${uid}-mode`">
							<p :id="`${uid}-mode`" class="bk-card-text">
								{{ $t('backup.phones.change_to', { path: pendingText }) }}
							</p>
							<template v-if="pendingNeedsMode">
								<label class="bk-radio">
									<input v-model="pendingMode" type="radio" value="move" />
									<span>
										<strong>{{ $t('backup.phones.mode_move') }}</strong>
										<span class="bk-secondary">{{ $t('backup.phones.mode_move_hint') }}</span>
									</span>
								</label>
								<label class="bk-radio">
									<input v-model="pendingMode" type="radio" value="fresh" />
									<span>
										<strong>{{ $t('backup.phones.mode_fresh') }}</strong>
										<span class="bk-secondary">{{ $t('backup.phones.mode_fresh_hint') }}</span>
									</span>
								</label>
							</template>
							<p v-if="pendingError" class="bk-inline-error" role="alert">{{ errText(pendingError) }}</p>
							<div class="bk-form-actions">
								<button type="button" class="bk-btn is-primary" :disabled="busy || (pendingNeedsMode && !pendingMode)" @click="applyLocation">{{ $t('backup.phones.change_confirm') }}</button>
								<button type="button" class="bk-btn" :disabled="busy" @click="cancelLocation">{{ $t('backup.cancel') }}</button>
							</div>
						</div>
						<div v-else class="bk-form-actions">
							<button type="button" class="bk-btn" :disabled="busy || moving || !!detail.open_sessions.length" @click="chooseLocation">{{ $t('backup.phones.change_location') }}</button>
							<button v-if="!detail.destination.default" type="button" class="bk-btn is-link" :disabled="busy || moving" @click="useDefault">{{ $t('backup.phones.use_default') }}</button>
						</div>
					</section>

					<!-- Categories -->
					<section class="bk-card" :aria-labelledby="`${uid}-cats`">
						<h3 :id="`${uid}-cats`" class="bk-card-title">{{ $t('backup.phones.categories') }}</h3>
						<div class="bk-table-wrap">
							<table class="bk-phone-table">
								<thead>
									<tr>
										<th scope="col">{{ $t('backup.phones.col_category') }}</th>
										<th scope="col">{{ $t('backup.phones.col_last') }}</th>
										<th scope="col">{{ $t('backup.phones.col_kept') }}</th>
										<th scope="col">{{ $t('backup.phones.col_size') }}</th>
										<th scope="col">{{ $t('backup.phones.col_schedule') }}</th>
									</tr>
								</thead>
								<tbody>
									<tr v-for="c in detail.categories" :key="c.category">
										<th scope="row">
											<b-icon :icon="icons[c.category]" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
											{{ $t('backup.phones.cat.' + c.category) }}
										</th>
										<td>
											<template v-if="c.last_backup_at">
												{{ fmt.relative(c.last_backup_at) }}
												<span v-if="c.last_status && c.last_status !== 'success'" class="bk-pill" :class="'is-' + statusView(c.last_status).tone">{{ $t('backup.status.' + c.last_status) }}</span>
											</template>
											<span v-else class="bk-secondary">{{ $t('backup.phones.never') }}</span>
										</td>
										<td>{{ countText(c) }}</td>
										<td>{{ c.size_bytes ? fmt.bytes(c.size_bytes) : '' }}</td>
										<td class="bk-secondary">{{ scheduleText(c.category) }}</td>
									</tr>
								</tbody>
							</table>
						</div>
						<p v-if="!detail.phone" class="bk-secondary bk-card-text">{{ $t('backup.phones.schedule_unknown') }}</p>
						<p v-else-if="detail.phone.paused" class="bk-card-text">{{ $t('backup.phones.paused') }}</p>
					</section>

					<!-- Retention -->
					<form class="bk-card" novalidate @submit.prevent="saveSettings">
						<h3 class="bk-card-title">{{ $t('backup.phones.keep') }}</h3>
						<div class="bk-fields">
							<div v-for="(lim, key) in limits" :key="key" class="bk-field">
								<label :for="`${uid}-${key}`">{{ $t('backup.phones.settings.' + key) }}</label>
								<input
									:id="`${uid}-${key}`"
									v-model.number="form[key]"
									type="number"
									class="bk-input"
									:min="lim.min"
									:max="lim.max"
									step="1"
									inputmode="numeric"
									:aria-invalid="formErrors[key] ? 'true' : null"
									:aria-describedby="`${uid}-${key}-hint`"
								/>
								<p :id="`${uid}-${key}-hint`" class="bk-field-hint">{{ $t('backup.phones.settings.' + key + '_hint') }}</p>
								<p v-if="formErrors[key]" class="bk-field-error">{{ $t('backup.field.out_of_range', lim) }}</p>
							</div>
						</div>
						<div class="bk-form-actions">
							<button type="submit" class="bk-btn is-primary" :disabled="busy || !settingsDirty || Object.keys(formErrors).length > 0">{{ $t('backup.save') }}</button>
						</div>
					</form>

					<!-- Browse -->
					<section class="bk-card" :aria-labelledby="`${uid}-browse`">
						<h3 :id="`${uid}-browse`" class="bk-card-title">{{ $t('backup.phones.browse') }}</h3>
						<div class="bk-phone-browse-bar">
							<label class="bk-sr-only" :for="`${uid}-snap`">{{ $t('backup.phones.snapshot') }}</label>
							<select :id="`${uid}-snap`" v-model="snapshot" class="bk-select">
								<option value="latest">{{ snapLabel('latest') }}</option>
								<option v-for="s in snapshots" :key="s.id" :value="s.id">{{ snapLabel(s) }}</option>
							</select>
							<label class="bk-sr-only" :for="`${uid}-cat`">{{ $t('backup.phones.col_category') }}</label>
							<select :id="`${uid}-cat`" v-model="browseCategory" class="bk-select">
								<option v-for="c in browseCategories" :key="c" :value="c">{{ $t('backup.phones.cat.' + c) }}</option>
							</select>
							<label v-if="isFiles" class="bk-check">
								<input v-model="includeDeleted" type="checkbox" />
								<span>{{ $t('backup.phones.include_deleted') }}</span>
							</label>
						</div>

						<template v-if="isFiles">
							<nav class="bk-crumbs" :aria-label="$t('backup.phones.folder')">
								<button type="button" class="bk-btn is-link is-small" @click="openDir('')">{{ $t('backup.phones.cat.' + browseCategory) }}</button>
								<template v-for="c in pathCrumbs" :key="c.path">
									<span aria-hidden="true">›</span>
									<button type="button" class="bk-btn is-link is-small" @click="openDir(c.path)">{{ c.name }}</button>
								</template>
							</nav>
							<p v-if="browseError" class="bk-inline-error" role="alert">{{ errText(browseError) }}</p>
							<p v-else-if="browseLoading && !entries.length" class="bk-secondary" role="status">{{ $t('backup.loading') }}</p>
							<p v-else-if="!entries.length" class="bk-secondary">{{ $t('backup.browse.empty') }}</p>
							<div v-else class="bk-table-wrap">
								<table class="bk-phone-table">
									<tbody>
										<tr v-for="e in entries" :key="e.path">
											<th scope="row">
												<button v-if="e.dir" type="button" class="bk-btn is-link" @click="openDir(e.path)">
													<b-icon icon="folder-outline" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
													<span>{{ e.name }}</span>
												</button>
												<span v-else>
													<b-icon icon="file-outline" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
													{{ e.name }}
												</span>
												<span v-if="e.deleted_on_device_at" class="bk-pill is-muted">{{ $t('backup.phones.deleted_on_phone') }}</span>
												<span v-if="e.version" class="bk-pill is-info">{{ $t('backup.phones.older_version') }}</span>
											</th>
											<td>{{ e.dir ? '' : fmt.bytes(e.size) }}</td>
											<td class="bk-secondary">{{ e.dir || !e.mtime ? '' : fmt.dateTime(e.mtime) }}</td>
											<td>
												<button
													v-if="!e.dir"
													type="button"
													class="bk-icon-btn"
													:aria-label="$t('backup.phones.download_file', { name: e.name })"
													:title="$t('backup.phones.download')"
													:disabled="downloading"
													@click="downloadFile(e)"
												>
													<b-icon icon="download" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
												</button>
											</td>
										</tr>
									</tbody>
								</table>
								<p v-if="truncated" class="bk-muted">{{ $t('backup.browse.truncated') }}</p>
							</div>
						</template>

						<template v-else>
							<p v-if="browseError" class="bk-inline-error" role="alert">{{ errText(browseError) }}</p>
							<p v-else-if="!exports.length" class="bk-secondary">{{ $t('backup.phones.no_exports') }}</p>
							<div v-else class="bk-table-wrap">
								<table class="bk-phone-table">
									<tbody>
										<tr v-for="x in exports" :key="x.id">
											<th scope="row">{{ exportName(x) }}</th>
											<td class="bk-secondary">{{ fmt.dateTime(x.taken_at) }}</td>
											<td>{{ x.items ? $t('backup.phones.count_items', { count: fmt.number(x.items) }) : '' }}</td>
											<td>{{ fmt.bytes(x.size) }}</td>
											<td>
												<button type="button" class="bk-icon-btn" :aria-label="$t('backup.phones.download_file', { name: exportName(x) })" :title="$t('backup.phones.download')" :disabled="downloading" @click="downloadExport(x)">
													<b-icon icon="download" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
												</button>
											</td>
										</tr>
									</tbody>
								</table>
							</div>
							<div v-if="isIncremental" class="bk-form-actions">
								<button type="button" class="bk-btn" :disabled="downloading || !exports.length" @click="downloadFull">{{ $t('backup.phones.download_full') }}</button>
							</div>
						</template>
					</section>
				</template>
			</div>
		</div>
		<span class="bk-sr-only" aria-live="polite">{{ note }}</span>
	</div>
</template>

<script>
import { backupMixin } from '../backupMixin'
import { confirmWindowMixin } from '@/mixins/confirmWindow'
import { downloadUrl } from '@/service/backup'
import { BACKUP_EVENTS } from '../events'
import { subscribe } from '../liveRun'
import { escapeHtml } from '@/utils/escapeHtml'
import {
	PHONE_CATEGORIES,
	CATEGORY_ICONS,
	INCREMENTAL_CATEGORIES,
	SETTINGS_LIMITS,
	isFileCategory,
	deviceTone,
	deviceStateKey,
	crumbs,
	sortEntries,
	locationText,
	moveProgress,
	categoryCount,
	phoneSchedule,
	settingsErrors,
	needsMode,
	snapshotLabel
} from '../phones'

let uidSeq = 0
const RELOAD_DEBOUNCE_MS = 500
const MOVE_POLL_MS = 3000

export default {
	name: 'PhonesSection',
	mixins: [backupMixin, confirmWindowMixin],
	props: {
		initialDeviceId: { type: String, default: '' },
		narrow: { type: Boolean, default: false }
	},
	data() {
		return {
			uid: `bk-phones-${++uidSeq}`,
			devices: [],
			listLoading: false,
			listError: null,
			selectedId: '',
			detail: null,
			loading: false,
			error: null,
			busy: false,
			note: '',
			form: {},
			// Change location: the picked endpoint (null = default) awaiting
			// move/fresh.
			pending: false,
			pendingLocation: null,
			pendingMode: '',
			pendingError: null,
			snapshots: [],
			snapshot: 'latest',
			browseCategory: 'media',
			browsePath: '',
			includeDeleted: false,
			entries: [],
			truncated: false,
			exports: [],
			browseLoading: false,
			browseError: null,
			browseSeq: 0,
			downloading: false,
			icons: CATEGORY_ICONS,
			limits: SETTINGS_LIMITS,
			browseCategories: PHONE_CATEGORIES
		}
	},
	computed: {
		isFiles() {
			return isFileCategory(this.browseCategory)
		},
		isIncremental() {
			return INCREMENTAL_CATEGORIES.includes(this.browseCategory)
		},
		pathCrumbs() {
			return crumbs(this.browsePath)
		},
		moving() {
			return !!(this.detail && this.detail.move && this.detail.move.state === 'moving')
		},
		moveRatio() {
			return moveProgress(this.detail && this.detail.move)
		},
		headMeta() {
			const d = this.detail.device
			const parts = [this.$t('backup.settings.device_platform_' + (d.platform || 'android'))]
			const p = this.detail.phone
			if (p && p.model) parts.push(p.model)
			if (p && p.app_version) parts.push(this.$t('backup.phones.app_version', { version: p.app_version }))
			parts.push(d.last_seen ? this.$t('backup.settings.device_last_seen', { when: this.fmt.relative(d.last_seen) }) : this.$t('backup.settings.device_never_seen'))
			return parts.join(' · ')
		},
		locationLine() {
			const dest = this.detail.destination
			const parts = [locationText(this.$t.bind(this), dest)]
			if (!dest.online) parts.push(this.$t('backup.phones.drive_missing'))
			else if (dest.free_bytes !== null && dest.free_bytes !== undefined) parts.push(this.$t('backup.phones.free', { free: this.fmt.bytes(dest.free_bytes) }))
			if (this.detail.device.size_bytes) parts.push(this.$t('backup.phones.used', { size: this.fmt.bytes(this.detail.device.size_bytes) }))
			return parts.join(' · ')
		},
		pendingNeedsMode() {
			return needsMode(this.detail)
		},
		pendingText() {
			const l = this.pendingLocation
			if (!l) return this.$t('backup.phones.default_location')
			const where = l.label || l.ref_id
			return l.sub_path ? `${where} › ${l.sub_path}` : where
		},
		formErrors() {
			return settingsErrors(this.form)
		},
		settingsDirty() {
			if (!this.detail) return false
			const s = this.detail.settings
			return Object.keys(SETTINGS_LIMITS).some(k => this.form[k] !== s[k])
		}
	},
	watch: {
		snapshot() {
			this.browsePath = ''
			this.loadBrowse()
		},
		browseCategory() {
			this.browsePath = ''
			this.loadBrowse()
		},
		includeDeleted() {
			this.loadBrowse()
		},
		initialDeviceId(id) {
			if (id) this.select(id)
		}
	},
	created() {
		this.loadList().then(() => {
			const id = this.initialDeviceId || (this.devices[0] && this.devices[0].id)
			if (id) this.select(id)
		})
		this.unsubscribe = subscribe(this.$socket, {
			[BACKUP_EVENTS.DEVICE_CHANGED]: props => this.onChanged(props)
		})
	},
	beforeUnmount() {
		if (this.unsubscribe) this.unsubscribe()
		clearTimeout(this.reloadTimer)
		clearInterval(this.movePoll)
	},
	methods: {
		tone(d, destination) {
			return deviceTone(d, { destination, staleDays: this.detail && this.detail.device.id === d.id ? this.detail.settings.stale_days : 7 })
		},
		stateKey(d, destination) {
			return deviceStateKey(d, { destination })
		},
		listMeta(d) {
			const parts = [d.last_backup_at ? this.$t('backup.phones.last_backup', { when: this.fmt.relative(d.last_backup_at) }) : this.$t('backup.phones.never_backed_up')]
			if (d.size_bytes) parts.push(this.fmt.bytes(d.size_bytes))
			return parts.join(' · ')
		},
		countText(c) {
			return categoryCount(this.$t.bind(this), this.fmt, c)
		},
		scheduleText(category) {
			return phoneSchedule(this.$t.bind(this), this.detail && this.detail.phone, category)
		},
		snapLabel(s) {
			return snapshotLabel(this.$t.bind(this), this.fmt, s)
		},
		verifyText(v) {
			if (v.running) return this.$t('backup.phones.verify_running')
			if (v.error) return this.$t('backup.phones.verify_error', { error: v.error })
			const key = v.missing || v.damaged ? 'backup.phones.verify_problems' : 'backup.phones.verify_ok'
			return this.$t(key, { when: this.fmt.relative(v.ended_at || v.started_at), checked: this.fmt.number(v.checked), bad: this.fmt.number(v.missing + v.damaged) })
		},
		exportName(x) {
			const cat = this.$t('backup.phones.cat.' + x.category)
			return x.name ? `${cat}: ${x.name}` : cat
		},

		// --- data -----------------------------------------------------
		async loadList() {
			this.listLoading = true
			this.listError = null
			try {
				this.devices = (await this.bkApi.listDevices()) || []
			} catch (e) {
				this.listError = e
			} finally {
				this.listLoading = false
			}
		},
		select(id) {
			if (this.selectedId === id && this.detail) return
			this.selectedId = id
			this.detail = null
			this.pending = false
			this.snapshot = 'latest'
			this.browsePath = ''
			this.loadDetail().then(() => this.loadBrowse())
		},
		async loadDetail() {
			const id = this.selectedId
			if (!id) return
			this.loading = true
			this.error = null
			try {
				const [detail, snaps] = await Promise.all([this.bkApi.getDevice(id), this.bkApi.deviceSnapshots(id)])
				if (id !== this.selectedId) return
				this.setDetail(detail)
				this.snapshots = snaps || []
				if (this.snapshot !== 'latest' && !this.snapshots.some(s => s.id === this.snapshot)) this.snapshot = 'latest'
			} catch (e) {
				if (id === this.selectedId) this.error = e
			} finally {
				this.loading = false
			}
		},
		setDetail(detail) {
			this.detail = detail
			this.form = { ...detail.settings }
			const i = this.devices.findIndex(d => d.id === detail.device.id)
			if (i >= 0) this.devices.splice(i, 1, detail.device)
			clearInterval(this.movePoll)
			if (detail.move && detail.move.state === 'moving') {
				this.movePoll = setInterval(() => this.loadDetail(), MOVE_POLL_MS)
			}
		},
		onChanged(props) {
			clearTimeout(this.reloadTimer)
			this.reloadTimer = setTimeout(() => {
				this.loadList()
				if (props.device_id && props.device_id === this.selectedId) {
					if (props.change === 'removed') {
						this.selectedId = ''
						this.detail = null
					} else {
						this.loadDetail()
					}
				}
			}, RELOAD_DEBOUNCE_MS)
		},
		async loadBrowse() {
			if (!this.selectedId) return
			const seq = ++this.browseSeq
			this.browseLoading = true
			this.browseError = null
			try {
				if (this.isFiles) {
					const res = await this.bkApi.browseDevice(this.selectedId, this.snapshot, { category: this.browseCategory, path: this.browsePath, includeDeleted: this.includeDeleted })
					if (seq !== this.browseSeq) return
					this.entries = sortEntries(res.entries)
					this.truncated = !!res.truncated
				} else {
					const res = await this.bkApi.deviceExports(this.selectedId, { category: this.browseCategory, snapshot: this.snapshot })
					if (seq !== this.browseSeq) return
					this.exports = (res || []).slice().sort((a, b) => String(b.taken_at).localeCompare(String(a.taken_at)))
				}
			} catch (e) {
				if (seq === this.browseSeq) {
					this.browseError = e
					this.entries = []
					this.exports = []
				}
			} finally {
				if (seq === this.browseSeq) this.browseLoading = false
			}
		},
		openDir(path) {
			this.browsePath = path
			this.loadBrowse()
		},

		// --- downloads ------------------------------------------------
		async download(req) {
			if (this.downloading) return
			this.downloading = true
			try {
				const { token } = await this.bkApi.createDeviceDownload(this.selectedId, req)
				const a = document.createElement('a')
				a.href = downloadUrl(token)
				a.rel = 'noopener'
				a.setAttribute('download', '')
				document.body.appendChild(a)
				a.click()
				a.remove()
			} catch (e) {
				this.toastError(e)
			} finally {
				this.downloading = false
			}
		},
		downloadFile(e) {
			return this.download({ category: this.browseCategory, path: e.path, snapshot: this.snapshot })
		},
		downloadExport(x) {
			return this.download({ exportId: x.id })
		},
		downloadFull() {
			return this.download({ category: this.browseCategory, full: true, snapshot: this.snapshot })
		},

		// --- location -------------------------------------------------
		chooseLocation() {
			this.openBackupWindow('storagePicker', {
				role: 'dest',
				selected: this.detail.device.dest,
				sourceEndpoint: null,
				onSelect: location => {
					if (!location) return
					const endpoint = { kind: location.kind, ref_id: location.ref_id, sub_path: '', label: location.label }
					if (location.match) endpoint.match = location.match
					this.openBackupWindow('folderPicker', {
						endpoint,
						startPath: '',
						allowCreate: true,
						onSelect: picked => {
							if (picked) this.startLocation(picked)
						}
					})
				}
			})
		},
		useDefault() {
			this.startLocation(null)
		},
		startLocation(location) {
			this.pending = true
			this.pendingLocation = location
			this.pendingMode = ''
			this.pendingError = null
		},
		cancelLocation() {
			this.pending = false
			this.pendingLocation = null
			this.pendingError = null
		},
		async applyLocation() {
			this.busy = true
			this.pendingError = null
			try {
				const detail = await this.bkApi.setDeviceLocation(this.selectedId, { location: this.pendingLocation, mode: this.pendingNeedsMode ? this.pendingMode : '' })
				this.setDetail(detail)
				this.pending = false
				this.note = this.$t('backup.phones.location_changed')
				this.toast(this.note)
				this.loadBrowse()
			} catch (e) {
				this.pendingError = e
			} finally {
				this.busy = false
			}
		},

		// --- actions --------------------------------------------------
		async saveSettings() {
			if (Object.keys(this.formErrors).length) return
			this.busy = true
			try {
				const settings = {}
				for (const k of Object.keys(SETTINGS_LIMITS)) settings[k] = this.form[k]
				this.setDetail(await this.bkApi.updateDevice(this.selectedId, { settings }))
				this.note = this.$t('backup.phones.saved')
				this.toast(this.note)
				this.loadDetail()
			} catch (e) {
				this.toastError(e)
			} finally {
				this.busy = false
			}
		},
		async verify() {
			this.busy = true
			try {
				await this.bkApi.verifyDevice(this.selectedId, { deep: false })
				await this.loadDetail()
			} catch (e) {
				this.toastError(e)
			} finally {
				this.busy = false
			}
		},
		revokeAccess() {
			const d = this.detail.device
			this.confirmWindow({
				title: this.$t('backup.phones.revoke_title', { name: d.name }),
				message: escapeHtml(this.$t('backup.phones.revoke_message', { name: d.name })),
				confirmText: this.$t('backup.phones.revoke'),
				type: 'is-danger',
				onConfirm: async () => {
					this.busy = true
					try {
						await this.bkApi.revokeDeviceAccess(d.id)
						await this.loadDetail()
					} catch (e) {
						this.toastError(e)
					} finally {
						this.busy = false
					}
				}
			})
		},
		remove() {
			const d = this.detail.device
			const path = this.detail.destination.path
			this.confirmWindow({
				title: this.$t('backup.settings.device_remove_title'),
				message: escapeHtml(this.$t('backup.settings.device_remove_message', { name: d.name, path })),
				confirmText: this.$t('backup.settings.device_remove_confirm'),
				type: 'is-danger',
				onConfirm: async () => {
					this.busy = true
					try {
						await this.bkApi.revokeDevice(d.id)
						this.note = this.$t('backup.settings.device_removed', { name: d.name })
						this.toast(this.note)
						this.selectedId = ''
						this.detail = null
						await this.loadList()
						if (this.devices[0]) this.select(this.devices[0].id)
					} catch (e) {
						this.toastError(e)
					} finally {
						this.busy = false
					}
				}
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-phones-body {
	display: grid;
	grid-template-columns: minmax(13rem, 16rem) minmax(0, 1fr);
	gap: var(--space-4);
	align-items: start;
}
.bk-phones.is-narrow .bk-phones-body {
	grid-template-columns: minmax(0, 1fr);
}
.bk-phone-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	margin: 0;
	padding: 0;
	list-style: none;
}
.bk-phone-item {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	width: 100%;
	padding: var(--space-2);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-md, 8px);
	background: var(--theme-card-bg);
	color: var(--theme-text-primary);
	font: inherit;
	text-align: left;
	cursor: pointer;
	&.active {
		border-color: var(--color-primary);
		box-shadow: inset 0 0 0 1px var(--color-primary);
	}
	&:focus-visible {
		outline: 2px solid var(--color-primary);
		outline-offset: 2px;
	}
}
.bk-phone-item-main {
	display: flex;
	flex: 1;
	flex-direction: column;
	min-width: 0;
}
.bk-phone-name {
	font-weight: 600;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.bk-phone-meta {
	font-size: var(--font-xs);
}
.bk-phone-detail {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	min-width: 0;
}
.bk-card {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}
.bk-card-title {
	margin-bottom: 0;
}
.bk-card-text {
	margin: 0;
	font-size: var(--font-sm);
}
.bk-phone-head {
	display: flex;
	align-items: flex-start;
	justify-content: space-between;
	gap: var(--space-2);
}
.bk-phone-mode {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: var(--space-3);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-md, 8px);
	background: var(--theme-bg-subtle);
}
.bk-radio {
	display: grid;
	grid-template-columns: auto minmax(0, 1fr);
	gap: var(--space-2);
	align-items: start;
	font-size: var(--font-sm);
	cursor: pointer;
	> span {
		display: flex;
		flex-direction: column;
	}
	input {
		margin-top: 0.2rem;
	}
}
.bk-form-actions {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
}
.bk-table-wrap {
	overflow-x: auto;
}
.bk-phone-table {
	width: 100%;
	border-collapse: collapse;
	font-size: var(--font-sm);
	th,
	td {
		padding: var(--space-1) var(--space-2);
		border-bottom: 1px solid var(--theme-card-border);
		text-align: left;
		vertical-align: middle;
	}
	thead th {
		font-size: var(--font-xs);
		font-weight: 500;
		color: var(--theme-text-secondary);
	}
	tbody th {
		font-weight: 500;
	}
	tbody tr:last-child th,
	tbody tr:last-child td {
		border-bottom: 0;
	}
}
.bk-phone-browse-bar {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
}
.bk-check {
	display: inline-flex;
	gap: var(--space-1);
	align-items: center;
	font-size: var(--font-sm);
}
.bk-crumbs {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1);
	align-items: center;
	font-size: var(--font-sm);
}
.bk-fields {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(12rem, 1fr));
	gap: var(--space-3) var(--space-4);
}
.bk-field {
	display: flex;
	flex-direction: column;
	gap: 0.125rem;
	label {
		font-size: var(--font-sm);
		font-weight: 500;
	}
	input[type='number'] {
		max-width: 8rem;
	}
	p {
		margin: 0;
	}
}
.bk-field-hint {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary);
}
.bk-field-error,
.bk-inline-error {
	margin: 0;
	font-size: var(--font-sm);
	color: var(--color-danger-fg);
}
</style>
