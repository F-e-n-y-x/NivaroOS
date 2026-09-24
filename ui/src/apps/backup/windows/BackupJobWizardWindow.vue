<!-- src/apps/backup/windows/BackupJobWizardWindow.vue -->
<!-- The job wizard (spec §12.4), a real desktop window: Start (new jobs
     only) → What → Where → When → Keep → Review. Opened with
     backupWindow(t, 'wizard', { jobId, preset, sourceEndpoint, destEndpoint, startStep }).
     - The stepper jumps back to any step reached; focus moves to the step
       heading on every change. Enter never submits a partial wizard.
     - Errors show as you go and, on Next, as a summary at the top of the
       step linking to each field (fields point at their error with
       aria-describedby). POST /validate runs debounced for the server's
       checks (space, inside-each-other, filters...).
     - The draft autosaves to localStorage; reopening offers to restore it.
       Closing with changes asks first.
     - Editing opens on Review with edit links. Saving sends the revision;
       a 409 offers to load the other change. -->
<template>
	<div class="backup-wizard" @keydown="onWindowKeydown">
		<div v-if="loading" class="bw-state" role="status">
			<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-24px" aria-hidden="true"></b-icon>
			<span>{{ $t('backup.loc.loading') }}</span>
		</div>
		<div v-else-if="loadError" class="bw-state" role="alert">
			<p class="bw-state-title">{{ loadError.title }}</p>
			<p>{{ loadError.cause }}</p>
			<div class="wz-row">
				<button type="button" class="wz-secondary" @click="closeWindow">{{ $t('Close') }}</button>
				<button type="button" class="wz-primary" @click="init">{{ $t('backup.action.try_again') }}</button>
			</div>
		</div>

		<template v-else-if="draft">
			<header class="bw-head">
				<wizard-stepper :steps="steps" :current="step" :reached="reached" :error-counts="stepperErrors" @go="goStep"></wizard-stepper>
			</header>

			<div ref="body" class="bw-body">
				<div v-if="restoreOffer" class="wz-note tone-info bw-banner" role="region" :aria-label="$t('backup.wizard.restore_title')">
					<b-icon icon="content-save-edit-outline" custom-size="mdi-18px" aria-hidden="true"></b-icon>
					<span class="bw-banner-text">
						<strong>{{ $t('backup.wizard.restore_title') }}</strong>
						{{ $t('backup.wizard.restore_text', { when: fmt.relative(restoreOffer.savedAt) }) }}
					</span>
					<span class="bw-banner-actions">
						<button type="button" class="wz-note-action" @click="restoreDraft">{{ $t('backup.wizard.restore_yes') }}</button>
						<button type="button" class="wz-note-action" @click="discardSaved">{{ $t('backup.wizard.restore_no') }}</button>
					</span>
				</div>

				<div v-if="summaryErrors.length" ref="errorSummary" class="bw-error-summary" role="alert" tabindex="-1" :aria-labelledby="idp + '-errsum'">
					<h3 :id="idp + '-errsum'">{{ $t('backup.wizard.errors_title', { n: summaryErrors.length }) }}</h3>
					<ul>
						<li v-for="e in summaryErrors" :key="e.field">
							<a :href="'#' + e.domId" @click.prevent="focusField(e)">{{ e.label }}: {{ e.message }}</a>
						</li>
					</ul>
				</div>

				<div v-if="saveError" class="wz-note tone-danger bw-banner" role="alert">
					<b-icon icon="alert-circle-outline" custom-size="mdi-18px" aria-hidden="true"></b-icon>
					<span class="bw-banner-text"><strong>{{ saveError.title }}</strong> {{ saveError.cause }} {{ saveError.fix }}</span>
				</div>
				<div v-if="conflict" class="wz-note tone-warn bw-banner" role="alert">
					<b-icon icon="call-split" custom-size="mdi-18px" aria-hidden="true"></b-icon>
					<span class="bw-banner-text"><strong>{{ $t('backup.err.revision_conflict.title') }}</strong> {{ $t('backup.err.revision_conflict.cause') }}</span>
					<span class="bw-banner-actions">
						<button type="button" class="wz-note-action" @click="loadConflict">{{ $t('backup.wizard.load_latest') }}</button>
					</span>
				</div>

				<!-- Start -->
				<section v-if="step === 'start'" class="bw-step" :aria-labelledby="idp + '-h-start'">
					<h2 :id="idp + '-h-start'" ref="heading" class="bw-title" tabindex="-1">{{ $t('backup.wizard.start.title') }}</h2>
					<p class="wz-sub">{{ $t('backup.wizard.start.sub') }}</p>
					<preset-grid ref="presets" v-model="startPreset" :presets="presets" :idp="idp" :labelledby="idp + '-h-start'" @choose="next"></preset-grid>
				</section>

				<!-- What -->
				<section v-else-if="step === 'what'" class="bw-step" :aria-labelledby="idp + '-h-what'">
					<h2 :id="idp + '-h-what'" ref="heading" class="bw-title" tabindex="-1">{{ $t('backup.wizard.what.title') }}</h2>
					<div class="wz-section">
						<h3 :id="idp + '-type-h'" class="wz-heading">{{ $t('backup.wizard.what.type_heading') }}</h3>
						<job-type-picker :value="draft.type" :allowed="allowedTypes" :idp="idp" :labelledby="idp + '-type-h'" @input="setType"></job-type-picker>
						<p v-if="autoTypeNote" class="wz-note tone-info" role="status">
							<b-icon icon="information-outline" custom-size="mdi-16px" aria-hidden="true"></b-icon><span>{{ autoTypeNote }}</span>
						</p>
					</div>
					<div class="wz-section">
						<h3 class="wz-heading">{{ $t('backup.wizard.what.source_heading') }}</h3>
						<source-picker :sources="draft.sources" :type="draft.type" :locations="locations" :idp="idp" :errors="shownErrors" :fmt="fmt"
							@pick="pickSource" @add="addSource" @remove="removeSource"></source-picker>
						<field-error :idp="idp" field="sources" :errors="shownErrors"></field-error>
						<p v-if="allErrors.sources === 'too_many' && draft.type !== 'archive'" class="wz-note tone-warn">
							<b-icon icon="alert-outline" custom-size="mdi-16px" aria-hidden="true"></b-icon>
							<span>{{ $t('backup.wizard.what.many_needs_archive') }}
								<button type="button" class="wz-note-action" @click="setType('archive')">{{ $t('backup.action.switch_archive') }}</button>
							</span>
						</p>
						<app-consistency-options v-if="hasHooks" :stop-apps="draft.stopApps" :app-mode="draft.appMode" :shutdown-vms="draft.shutdownVms"
							:idp="idp" :errors="shownErrors" @patch="applyPatch"></app-consistency-options>
					</div>
					<div class="wz-section">
						<h3 class="wz-heading">{{ $t('backup.wizard.what.skip_heading') }}</h3>
						<exclude-editor :exclude-presets="draft.excludePresets" :exclude="draft.exclude" :max-size-on="draft.maxSizeOn" :max-size-gb="draft.maxSizeGb"
							:idp="idp" :errors="shownErrors" @patch="applyPatch"></exclude-editor>
					</div>
				</section>

				<!-- Where -->
				<section v-else-if="step === 'where'" class="bw-step" :aria-labelledby="idp + '-h-where'">
					<h2 :id="idp + '-h-where'" ref="heading" class="bw-title" tabindex="-1">{{ $t('backup.wizard.where.title') }}</h2>
					<p v-if="destHint" class="wz-sub">{{ destHint }}</p>
					<location-card :dest="draft.dest" :location="destLocation" :idp="idp" :errors="shownErrors" :fmt="fmt"
						@choose="chooseDest" @browse="browseDest" @sub-path="setSubPath"></location-card>
					<p v-if="allErrors['dest.type']" :id="fieldDomId('dest.type')" class="wz-note tone-danger" tabindex="-1">
						<b-icon icon="alert-circle-outline" custom-size="mdi-16px" aria-hidden="true"></b-icon>
						<span>
							<strong>{{ $t('backup.err.appdata_cloud_needs_archive.title') }}.</strong> {{ $t('backup.wizard.where.cloud_needs_archive') }}
							<button type="button" class="wz-note-action" @click="setType('archive')">{{ $t('backup.action.switch_archive') }}</button>
						</span>
					</p>
					<p v-for="w in destNotes" :key="w.key" class="wz-note" :class="'tone-' + w.tone">
						<b-icon :icon="w.icon" custom-size="mdi-16px" aria-hidden="true"></b-icon><span>{{ $t(w.key) }}</span>
					</p>
					<ul v-if="destQuirks.length" class="bw-quirks" :aria-label="$t('backup.loc.quirks_label')">
						<li v-for="q in destQuirks" :key="q"><b-icon icon="information-outline" custom-size="mdi-14px" aria-hidden="true"></b-icon>{{ $t('backup.quirk.' + q) }}</li>
					</ul>
					<space-estimate v-if="draft.dest && draft.sources.length" :result="validateResult" :pending="validatePending" :unavailable="validateUnavailable" :fmt="fmt"></space-estimate>
				</section>

				<!-- When -->
				<section v-else-if="step === 'when'" class="bw-step" :aria-labelledby="idp + '-h-when'">
					<h2 :id="idp + '-h-when'" ref="heading" class="bw-title" tabindex="-1">{{ $t('backup.wizard.when.title') }}</h2>
					<div class="wz-section">
						<h3 class="wz-heading">{{ $t('backup.wizard.when.run_heading') }}</h3>
						<trigger-editor :schedule-on="draft.scheduleOn" :cron="draft.cron" :plug-on="draft.plugOn" :plug-gap-hours="draft.plugGapHours" :catch-up="draft.catchUp"
							:plug-drive="plugDrive" :preview-fn="cronPreview" :idp="idp" :errors="shownErrors" @patch="applyPatch" @validity="cronValid = $event"></trigger-editor>
					</div>
					<div class="wz-section">
						<h3 class="wz-heading">{{ $t('backup.wizard.when.only_heading') }}</h3>
						<condition-editor :type="draft.type" :dest-available="draft.destAvailable" :effective-unmet="effectiveUnmet" :wait-max-min="draft.waitMaxMin"
							:window-on="draft.windowOn" :window-start="draft.windowStart" :window-end="draft.windowEnd" :idp="idp" :errors="shownErrors" @patch="applyPatch"></condition-editor>
					</div>
					<div class="wz-section">
						<retry-policy :max="Number(draft.retryMax) || 0" :backoff-sec="draft.retryBackoffSec" :idp="idp" :fmt="fmt" @patch="applyPatch"></retry-policy>
					</div>
				</section>

				<!-- Keep -->
				<section v-else-if="step === 'keep'" class="bw-step" :aria-labelledby="idp + '-h-keep'">
					<h2 :id="idp + '-h-keep'" ref="heading" class="bw-title" tabindex="-1">{{ $t('backup.wizard.keep.title') }}</h2>
					<keep-editor :type="draft.type" :versions-days="draft.versionsDays" :keep-last="draft.keepLast" :delete-pct="draft.deletePct" :change-pct="draft.changePct"
						:empty-source-pct="draft.emptySourcePct" :preview-first="draft.previewFirst" :verify="draft.verify" :verify-blocked="verifyBlocked"
						:idp="idp" :errors="shownErrors" @patch="applyPatch"></keep-editor>
				</section>

				<!-- Review -->
				<section v-else-if="step === 'review'" class="bw-step" :aria-labelledby="idp + '-h-review'">
					<h2 :id="idp + '-h-review'" ref="heading" class="bw-title" tabindex="-1">{{ isEdit ? $t('backup.wizard.review.title_edit') : $t('backup.wizard.review.title') }}</h2>
					<p v-if="draft.needsAttention === 'imported'" class="wz-note tone-info">
						<b-icon icon="import" custom-size="mdi-16px" aria-hidden="true"></b-icon><span>{{ $t('backup.wizard.review.imported') }}</span>
					</p>
					<div class="wz-section">
						<label :for="fieldDomId('name')" class="wz-label">{{ $t('backup.wizard.review.name') }}</label>
						<input :id="fieldDomId('name')" class="wz-input bw-name" type="text" maxlength="200" autocomplete="off" :value="draft.name" :placeholder="autoName"
							:aria-invalid="shownErrors.name ? 'true' : 'false'" :aria-describedby="describedByFor('name', fieldDomId('name') + '-hint')" @input="setName($event.target.value)" />
						<p :id="fieldDomId('name') + '-hint'" class="wz-hint">{{ $t('backup.wizard.review.name_hint') }}</p>
						<field-error :idp="idp" field="name" :errors="shownErrors"></field-error>
					</div>
					<div class="wz-section">
						<h3 class="wz-heading">{{ $t('backup.wizard.review.summary_heading') }}</h3>
						<job-summary :job="job" :fmt="fmt" editable @edit="goStep"></job-summary>
						<space-estimate v-if="draft.dest && draft.sources.length" :result="validateResult" :pending="validatePending" :unavailable="validateUnavailable" :fmt="fmt"></space-estimate>
					</div>
					<div class="wz-section">
						<advanced-options :low-priority="draft.lowPriority" :max-duration-hours="draft.maxDurationHours" :include="draft.include" :copy-empty-dirs="draft.copyEmptyDirs"
							:allow-empty-source="draft.allowEmptySource" :notify-on-success="draft.notifyOnSuccess" :idp="idp" :errors="shownErrors" @patch="applyPatch"></advanced-options>
						<label v-if="!isEdit" class="wz-check">
							<input type="checkbox" :checked="draft.runNow" @change="applyPatch({ runNow: $event.target.checked })" />
							<span>{{ draft.previewFirst && draft.type !== 'archive' ? $t('backup.wizard.review.run_now_preview') : $t('backup.wizard.review.run_now') }}</span>
						</label>
					</div>
				</section>
			</div>

			<footer class="bw-foot">
				<button v-if="stepIndex > 0" type="button" class="wz-secondary" @click="back">
					<b-icon icon="arrow-left" custom-size="mdi-16px" aria-hidden="true"></b-icon><span>{{ $t('Back') }}</span>
				</button>
				<span class="bw-foot-spacer"></span>
				<button type="button" class="wz-secondary" @click="requestClose">{{ $t('Cancel') }}</button>
				<button v-if="step !== 'review'" type="button" class="wz-primary" :disabled="!canNext" :aria-describedby="canNext ? null : idp + '-next-why'" @click="next">
					<span>{{ $t('Next') }}</span><b-icon icon="arrow-right" custom-size="mdi-16px" aria-hidden="true"></b-icon>
				</button>
				<button v-else type="button" class="wz-primary" :disabled="saving" @click="save">
					<b-icon v-if="saving" icon="loading" custom-class="mdi-spin" custom-size="mdi-16px" aria-hidden="true"></b-icon>
					<span>{{ isEdit ? $t('backup.wizard.save') : $t('backup.wizard.create') }}</span>
				</button>
				<span v-if="!canNext" :id="idp + '-next-why'" class="sr-only">{{ $t('backup.field.invalid_cron') }}</span>
			</footer>
		</template>
	</div>
</template>

<script>
import { backupMixin } from '../backupMixin'
import { backupWindow } from '../windows'
import { explainError } from '../messages'
import { fieldErrorKey } from '../errorCodes'
import { visiblePresets, presetById, autoTypeForDest, safeTypes, isAppOrVmSource } from '../presets'
import { plugTarget } from '../summaries'
import {
	STEPS, FIELD_STEP, newDraft, draftFromJob, draftToJob, validateDraft, errorsForStep, firstStepWithErrors, mapServerFieldErrors,
	blockingCheckErrors, defaultName, destFolderSubPath, effectiveWhenUnmet, syncHookChoices, hasAppOrVmSource
} from '../wizard/draft'
import { fieldId, describedBy } from '../wizard/fields'
import { windowFocusMixin } from '@/shared/storage/windowFocus'
import { findLocation, endpointFromLocation, riskyDestination } from '@/shared/storage/locations'
import WizardStepper from '../components/WizardStepper.vue'
import PresetGrid from '../components/PresetGrid.vue'
import JobTypePicker from '../components/JobTypePicker.vue'
import SourcePicker from '../components/SourcePicker.vue'
import ExcludeEditor from '../components/ExcludeEditor.vue'
import AppConsistencyOptions from '../components/AppConsistencyOptions.vue'
import LocationCard from '../components/LocationCard.vue'
import SpaceEstimate from '../components/SpaceEstimate.vue'
import TriggerEditor from '../components/TriggerEditor.vue'
import ConditionEditor from '../components/ConditionEditor.vue'
import RetryPolicy from '../components/RetryPolicy.vue'
import KeepEditor from '../components/KeepEditor.vue'
import AdvancedOptions from '../components/AdvancedOptions.vue'
import JobSummary from '../components/JobSummary.vue'
import FieldError from '../components/FieldError.vue'

const AUTOSAVE_PREFIX = 'nivaroos_backup_wizard_draft:'
const AUTOSAVE_MAX_AGE_MS = 7 * 24 * 3600 * 1000
const AUTOSAVE_DELAY_MS = 800
const VALIDATE_DELAY_MS = 700

// Which wizard field a draft property belongs to (touched = its error
// shows before Next is pressed).
const PATCH_FIELD = {
	type: 'type', sources: 'sources', stopApps: 'hooks', shutdownVms: 'hooks', exclude: 'filters.exclude', include: 'filters.include',
	maxSizeOn: 'filters.max_size_bytes', maxSizeGb: 'filters.max_size_bytes', dest: 'dest', cron: 'cron', scheduleOn: 'cron',
	plugOn: 'plug.volume', plugGapHours: 'plug.min_gap_hours', windowStart: 'window.start', windowEnd: 'window.end', windowOn: 'window.end',
	waitMaxMin: 'wait_max_min', retryMax: 'retry.max', versionsDays: 'retention.versions_days', keepLast: 'retention.keep_last',
	deletePct: 'guards.delete_pct', changePct: 'guards.change_pct', emptySourcePct: 'guards.empty_source_pct', name: 'name',
	maxDurationHours: 'options.max_duration_sec'
}

let seq = 0

function readSaved(key) {
	try {
		const raw = localStorage.getItem(key)
		if (!raw) return null
		const v = JSON.parse(raw)
		if (!v || v.v !== 1 || !v.draft || Date.now() - v.savedAt > AUTOSAVE_MAX_AGE_MS) return null
		return v
	} catch (e) {
		return null
	}
}

function writeSaved(key, value) {
	try {
		if (value) localStorage.setItem(key, JSON.stringify(value))
		else localStorage.removeItem(key)
	} catch (e) {
		// Private mode or full storage: autosave is a convenience only.
	}
}

export default {
	name: 'BackupJobWizardWindow',
	components: {
		WizardStepper, PresetGrid, JobTypePicker, SourcePicker, ExcludeEditor, AppConsistencyOptions, LocationCard, SpaceEstimate,
		TriggerEditor, ConditionEditor, RetryPolicy, KeepEditor, AdvancedOptions, JobSummary, FieldError
	},
	mixins: [backupMixin, windowFocusMixin],
	props: {
		winId: { type: String, default: '' },
		jobId: { type: String, default: null },
		// presets.js id: skips the Start step
		preset: { type: String, default: '' },
		sourceEndpoint: { type: Object, default: null },
		destEndpoint: { type: Object, default: null },
		startStep: { type: String, default: '' }
	},
	data() {
		return {
			idp: `bw${++seq}`,
			loading: true,
			loadError: null,
			locations: [],
			settings: null,
			draft: null,
			initialJson: '',
			appliedPreset: '',
			startPreset: 'scratch',
			step: 'what',
			reached: 'what',
			showErrors: {},
			touched: {},
			serverErrors: {},
			validateResult: null,
			validatePending: false,
			validateUnavailable: false,
			cronValid: true,
			saving: false,
			saveError: null,
			conflict: null,
			restoreOffer: null,
			autoTypeNote: '',
			closing: false
		}
	},
	computed: {
		isEdit() {
			return !!this.jobId
		},
		autosaveKey() {
			return AUTOSAVE_PREFIX + (this.jobId || 'new')
		},
		steps() {
			return this.isEdit || this.preset ? STEPS.filter(s => s !== 'start') : STEPS
		},
		stepIndex() {
			return this.steps.indexOf(this.step)
		},
		presets() {
			return visiblePresets(this.locations)
		},
		job() {
			return draftToJob(this.draft)
		},
		jobJson() {
			return this.draft ? JSON.stringify(this.job) : ''
		},
		dirty() {
			return !!this.draft && this.jobJson !== this.initialJson
		},
		clientErrors() {
			return validateDraft(this.draft)
		},
		allErrors() {
			return { ...this.serverErrors, ...blockingCheckErrors(this.validateResult), ...this.clientErrors }
		},
		// Errors shown now: touched fields, and every field of a step the
		// user tried to leave.
		shownErrors() {
			const out = {}
			for (const [f, code] of Object.entries(this.allErrors)) {
				const step = FIELD_STEP[f] || 'review'
				if (this.touched[f] || this.showErrors[step]) out[f] = code
			}
			return out
		},
		summaryErrors() {
			if (!this.showErrors[this.step]) return []
			return Object.entries(errorsForStep(this.allErrors, this.step)).map(([field, code]) => ({
				field,
				domId: this.fieldDomId(field),
				label: this.$t('backup.wizard.field.' + field.replace(/\./g, '_')),
				message: this.$t(fieldErrorKey(code))
			}))
		},
		stepperErrors() {
			const out = {}
			for (const s of this.steps) if (this.showErrors[s]) out[s] = Object.keys(errorsForStep(this.allErrors, s)).length
			return out
		},
		destLocation() {
			return this.draft && this.draft.dest ? findLocation(this.locations, this.draft.dest) : null
		},
		sourceLocation() {
			return this.draft && this.draft.sources[0] ? findLocation(this.locations, this.draft.sources[0]) : null
		},
		allowedTypes() {
			return safeTypes(this.draft.sources, this.draft.dest && this.draft.dest.kind)
		},
		hasHooks() {
			return hasAppOrVmSource(this.draft)
		},
		verifyBlocked() {
			const q = (this.destLocation && this.destLocation.quirks) || []
			return q.includes('no_hash')
		},
		destQuirks() {
			return (this.destLocation && this.destLocation.quirks) || []
		},
		destNotes() {
			const out = []
			const loc = this.destLocation
			if (!loc) return out
			const risk = riskyDestination(loc, this.sourceLocation)
			if (risk === 'same_disk') out.push({ key: 'backup.loc.same_disk_note', tone: 'warn', icon: 'alert-outline' })
			for (const w of loc.warnings || []) out.push({ key: 'backup.warn.' + w, tone: 'warn', icon: 'alert-outline' })
			if (!loc.online && loc.kind === 'usb') out.push({ key: 'backup.loc.offline_usb_note', tone: 'info', icon: 'information-outline' })
			return out
		},
		destHint() {
			const p = presetById(this.appliedPreset)
			if (!p || this.draft.dest) return ''
			return p.destHint === 'any' ? '' : this.$t('backup.wizard.where.hint_' + p.destHint)
		},
		plugDrive() {
			const t = plugTarget(this.job, this.draft.plugVolume ? { volume: this.draft.plugVolume } : null)
			return t ? t.label || t.ref_id : ''
		},
		effectiveUnmet() {
			return effectiveWhenUnmet(this.draft)
		},
		autoName() {
			return defaultName(this.draft) || this.$t('backup.wizard.review.name_placeholder')
		},
		canNext() {
			return !(this.step === 'when' && this.draft.scheduleOn && !this.cronValid)
		}
	},
	watch: {
		jobJson(v, old) {
			if (!old || this.loading) return
			this.scheduleAutosave()
			this.scheduleValidate()
		}
	},
	created() {
		this.init()
	},
	beforeDestroy() {
		clearTimeout(this.autosaveTimer)
		clearTimeout(this.validateTimer)
		// Keep what was typed if the window goes away without Cancel
		// (desktop reload, session end); Cancel/Discard clear it.
		if (!this.closing && this.dirty) this.saveDraftNow()
	},
	methods: {
		fieldDomId(field) {
			return fieldId(this.idp, field)
		},
		describedByFor(field, ...more) {
			return describedBy(this.idp, field, this.shownErrors, ...more)
		},
		cronPreview(cron) {
			return this.bkApi.cronPreview(cron)
		},

		// ---- loading -------------------------------------------------
		async init() {
			this.loading = true
			this.loadError = null
			const [locations, settings, job] = await Promise.all([
				this.bkApi.locations().catch(() => []),
				this.isEdit ? Promise.resolve(null) : this.bkApi.getSettings().catch(() => null),
				this.isEdit ? this.bkApi.getJob(this.jobId).catch(e => ({ __error: e })) : Promise.resolve(null)
			])
			this.locations = Array.isArray(locations) ? locations : []
			this.settings = settings
			if (job && job.__error) {
				this.loadError = explainError(this.$t.bind(this), job.__error.code)
				this.loading = false
				return
			}
			if (this.isEdit) {
				this.draft = draftFromJob(job, { settings })
				// Opened by id alone (Files, a notification) the title has no name yet.
				if (this.winId && job && job.name) this.$store.commit('UPDATE_WINDOW_PROPS', { id: this.winId, title: this.$t('backup.window.wizard_edit', { name: job.name }) })
			} else {
				this.appliedPreset = this.preset && presetById(this.preset) ? this.preset : ''
				this.draft = this.freshDraft(this.appliedPreset)
			}
			this.initialJson = JSON.stringify(draftToJob(this.draft))
			const wanted = this.steps.includes(this.startStep) ? this.startStep : this.isEdit ? 'review' : this.steps[0]
			this.step = wanted
			this.reached = this.isEdit ? 'review' : wanted
			this.offerRestore()
			this.loading = false
			this.scheduleValidate(0)
			this.focusHeading()
		},
		freshDraft(presetId) {
			const d = newDraft({ settings: this.settings, preset: presetById(presetId), locations: this.locations, sourceEndpoint: this.sourceEndpoint, destEndpoint: this.destEndpoint })
			this.fillDefaults(d)
			return d
		},
		// Name and destination folder follow the source and destination
		// until the user types their own.
		fillDefaults(d) {
			if (d.dest && !d.destPathTouched) d.dest.sub_path = destFolderSubPath(d)
		},

		// ---- autosave ------------------------------------------------
		offerRestore() {
			const saved = readSaved(this.autosaveKey)
			if (!saved) return
			if (this.isEdit && saved.draft.revision !== this.draft.revision) {
				writeSaved(this.autosaveKey, null)
				return
			}
			if (JSON.stringify(draftToJob(saved.draft)) === this.initialJson) return
			this.restoreOffer = saved
		},
		restoreDraft() {
			const s = this.restoreOffer
			this.restoreOffer = null
			if (!s) return
			this.draft = s.draft
			this.appliedPreset = s.appliedPreset || ''
			this.step = this.steps.includes(s.step) ? s.step : this.step
			this.reached = this.steps.includes(s.reached) ? s.reached : this.step
			this.focusHeading()
		},
		discardSaved() {
			this.restoreOffer = null
			writeSaved(this.autosaveKey, null)
			this.focusHeading()
		},
		scheduleAutosave() {
			clearTimeout(this.autosaveTimer)
			this.autosaveTimer = setTimeout(() => this.saveDraftNow(), AUTOSAVE_DELAY_MS)
		},
		saveDraftNow() {
			if (!this.draft || this.restoreOffer) return
			if (!this.dirty) {
				writeSaved(this.autosaveKey, null)
				return
			}
			writeSaved(this.autosaveKey, { v: 1, savedAt: Date.now(), step: this.step, reached: this.reached, appliedPreset: this.appliedPreset, draft: this.draft })
		},

		// ---- server checks -------------------------------------------
		scheduleValidate(delay = VALIDATE_DELAY_MS) {
			clearTimeout(this.validateTimer)
			if (!this.draft || !this.draft.sources.length || !this.draft.dest) {
				this.validateResult = null
				this.validatePending = false
				return
			}
			this.validatePending = true
			this.validateTimer = setTimeout(() => this.runValidate(), delay)
		},
		async runValidate() {
			const job = this.job
			const json = this.jobJson
			try {
				const res = await this.bkApi.validate(job)
				if (json !== this.jobJson) return
				this.validateResult = res
				this.validateUnavailable = false
				this.serverErrors = mapServerFieldErrors(res && res.field_errors, job)
			} catch (e) {
				if (json !== this.jobJson) return
				if (e.code === 'validation') {
					this.serverErrors = mapServerFieldErrors(e.fieldErrors, job)
				} else {
					this.validateResult = null
					this.validateUnavailable = true
				}
			} finally {
				if (json === this.jobJson) this.validatePending = false
			}
		},
		async settleValidate() {
			if (!this.validatePending) return
			clearTimeout(this.validateTimer)
			const run = this.runValidate()
			await Promise.race([run, new Promise(resolve => setTimeout(resolve, 8000))])
		},

		// ---- editing -------------------------------------------------
		applyPatch(patch) {
			const d = this.draft
			for (const [k, v] of Object.entries(patch)) {
				this.$set(d, k, v)
				const f = PATCH_FIELD[k]
				if (f) {
					this.$set(this.touched, f, true)
					if (this.serverErrors[f]) this.$delete(this.serverErrors, f)
				}
			}
			this.saveError = null
			if ('sources' in patch) syncHookChoices(d)
			if ('sources' in patch || 'dest' in patch || 'name' in patch) this.fillDefaults(d)
		},
		setType(t) {
			const d = this.draft
			this.autoTypeNote = ''
			const patch = { type: t, typeTouched: true }
			// Mirror previews its first run unless the user said otherwise.
			if (!d.previewTouched && !this.isEdit) patch.previewFirst = t === 'mirror'
			this.applyPatch(patch)
		},
		setName(v) {
			this.applyPatch({ name: v, nameTouched: true })
		},
		setSubPath(v) {
			this.applyPatch({ dest: { ...this.draft.dest, sub_path: v }, destPathTouched: true })
			this.$set(this.touched, 'dest.sub_path', true)
		},
		// A folder picked by browsing that is one of the location's presets
		// (AppData/<app>, VMs/<vm>) gets the preset, so hooks and the
		// cloud rule apply to it too.
		withPreset(ep) {
			const loc = findLocation(this.locations, ep)
			const p = loc && (loc.presets || []).find(x => x.sub_path === ep.sub_path)
			const out = { ...ep }
			if (p) out.preset = p.id
			else delete out.preset
			return out
		},
		setSource(index, ep) {
			const list = this.draft.sources.slice()
			if (index === null || index === undefined) {
				if (this.draft.type === 'archive') list.push(ep)
				else list.splice(0, list.length, ep)
			} else {
				list.splice(index, 1, ep)
			}
			this.applyPatch({ sources: list })
			this.autoSwitchType()
		},
		addSource(ep) {
			if (this.draft.sources.some(s => s.kind === ep.kind && s.ref_id === ep.ref_id && s.sub_path === ep.sub_path)) return
			this.setSource(null, ep)
		},
		removeSource(i) {
			this.applyPatch({ sources: this.draft.sources.filter((_, j) => j !== i) })
			this.$nextTick(() => {
				const el = document.getElementById(this.fieldDomId('sources'))
				if (el) el.focus()
			})
		},
		// App data or VMs toward a cloud must be an Archive. While the user
		// hasn't picked a type (or a preset asks for it), switch and say so.
		autoSwitchType() {
			const d = this.draft
			if (!d.dest) return
			const byPreset = !d.typeTouched ? autoTypeForDest(presetById(this.appliedPreset), d.dest.kind) : null
			if (byPreset && byPreset !== d.type) {
				this.applyPatch({ type: byPreset, previewFirst: byPreset === 'mirror' && !d.previewTouched ? true : d.previewFirst })
				this.autoTypeNote = this.$t(byPreset === 'archive' ? 'backup.wizard.what.auto_archive' : 'backup.wizard.what.auto_mirror')
				return
			}
			if (!d.typeTouched && d.dest.kind === 'cloud' && d.sources.some(isAppOrVmSource) && d.type !== 'archive') {
				this.applyPatch({ type: 'archive' })
				this.autoTypeNote = this.$t('backup.wizard.what.auto_archive')
			}
		},
		pickSource(index) {
			const current = index === null || index === undefined ? null : this.draft.sources[index]
			this.openBackupWindow('storagePicker', {
				role: 'source',
				selected: current,
				onSelect: loc => {
					if (this._isDestroyed) return
					this.openBackupWindow('folderPicker', {
						endpoint: endpointFromLocation(loc),
						startPath: current && current.kind === loc.kind && current.ref_id === loc.ref_id ? current.sub_path : '',
						allowCreate: false,
						returnFocus: () => document.getElementById(this.fieldDomId('sources')),
						onSelect: ep => {
							if (!this._isDestroyed) this.setSource(index, this.withPreset(ep))
						}
					})
				}
			})
		},
		chooseDest() {
			this.openBackupWindow('storagePicker', {
				role: 'dest',
				selected: this.draft.dest,
				sourceEndpoint: this.draft.sources[0] || null,
				chooseLabel: this.$t('backup.loc.use_location'),
				onSelect: loc => {
					if (this._isDestroyed) return
					const keepPath = this.draft.destPathTouched && this.draft.dest && this.draft.dest.kind === loc.kind && this.draft.dest.ref_id === loc.ref_id
					const ep = endpointFromLocation(loc, keepPath ? this.draft.dest.sub_path : '')
					const patch = { dest: ep, destPathTouched: keepPath }
					// No checksums there (TeraBox): verify can't run.
					if ((loc.quirks || []).includes('no_hash')) patch.verify = false
					this.applyPatch(patch)
					this.autoSwitchType()
				}
			})
		},
		browseDest() {
			const dest = this.draft.dest
			if (!dest) return
			this.openBackupWindow('folderPicker', {
				endpoint: dest,
				startPath: dest.sub_path,
				allowCreate: true,
				returnFocus: () => document.getElementById(this.fieldDomId('dest.sub_path')),
				onSelect: ep => {
					if (!this._isDestroyed) this.setSubPath(ep.sub_path)
				}
			})
		},

		// ---- navigation ----------------------------------------------
		focusHeading() {
			this.$nextTick(() => {
				if (this.$refs.body) this.$refs.body.scrollTop = 0
				if (this.step === 'start' && this.$refs.presets) this.$refs.presets.focus()
				else if (this.$refs.heading) this.$refs.heading.focus()
			})
		},
		goStep(s) {
			if (!this.steps.includes(s) || this.steps.indexOf(s) > this.steps.indexOf(this.reached)) return
			this.step = s
			this.focusHeading()
		},
		back() {
			if (this.stepIndex > 0) this.goStep(this.steps[this.stepIndex - 1])
		},
		async next() {
			if (!this.canNext) return
			if (this.step === 'start') {
				if (this.startPreset !== this.appliedPreset) {
					this.appliedPreset = this.startPreset
					this.draft = this.freshDraft(this.startPreset)
					this.touched = {}
					this.showErrors = {}
				}
			} else {
				if (this.step === 'where') await this.settleValidate()
				const errors = errorsForStep(this.allErrors, this.step)
				if (Object.keys(errors).length) {
					this.$set(this.showErrors, this.step, true)
					this.$nextTick(() => this.$refs.errorSummary && this.$refs.errorSummary.focus())
					return
				}
				this.$set(this.showErrors, this.step, false)
			}
			const nextStep = this.steps[this.stepIndex + 1]
			if (!nextStep) return
			this.step = nextStep
			if (this.steps.indexOf(nextStep) > this.steps.indexOf(this.reached)) this.reached = nextStep
			this.focusHeading()
		},
		focusField(e) {
			const el = document.getElementById(e.domId)
			if (el) {
				if (el.scrollIntoView) el.scrollIntoView({ block: 'center' })
				el.focus()
			}
		},

		// ---- saving --------------------------------------------------
		async save() {
			if (this.saving) return
			this.saveError = null
			await this.settleValidate()
			const bad = firstStepWithErrors(this.allErrors)
			if (bad) {
				for (const s of this.steps) this.$set(this.showErrors, s, true)
				if (bad !== this.step) {
					this.step = bad
					this.focusHeading()
				}
				this.$nextTick(() => this.$refs.errorSummary && this.$refs.errorSummary.focus())
				return
			}
			this.saving = true
			const job = this.job
			try {
				const saved = this.isEdit ? await this.bkApi.updateJob(job) : await this.bkApi.createJob(job)
				this.closing = true
				writeSaved(this.autosaveKey, null)
				this.toast(this.$t(this.isEdit ? 'backup.wizard.saved' : 'backup.wizard.created', { name: saved.name || job.name }))
				const runNow = !this.isEdit && this.draft.runNow
				const previewFirst = job.options.preview_first
				this.closeWindow()
				if (runNow) this.startFirstRun(saved, previewFirst)
			} catch (e) {
				if (e.code === 'revision_conflict' && e.current) {
					this.conflict = e.current
				} else if (e.code === 'validation') {
					this.serverErrors = mapServerFieldErrors(e.fieldErrors, job)
					for (const s of this.steps) this.$set(this.showErrors, s, true)
					const s = firstStepWithErrors(this.allErrors)
					if (s && s !== this.step) {
						this.step = s
						this.focusHeading()
					}
					this.$nextTick(() => this.$refs.errorSummary && this.$refs.errorSummary.focus())
				} else {
					this.saveError = explainError(this.$t.bind(this), e.code)
				}
			} finally {
				this.saving = false
			}
		},
		// The first run: a preview when the job asks for one (it pauses at
		// waiting_user for the decision), else straight to its progress.
		async startFirstRun(job, previewFirst) {
			try {
				const res = await this.bkApi.runJob(job.id, { preview: false })
				const params = { runId: res.run_id, jobId: job.id, jobName: job.name }
				this.$store.commit('OPEN_WINDOW', backupWindow(this.$t.bind(this), previewFirst ? 'preview' : 'run', params))
			} catch (e) {
				this.toastError(e)
			}
		},
		loadConflict() {
			const current = this.conflict
			if (!current) return
			this.conflict = null
			this.draft = draftFromJob(current, { settings: this.settings })
			this.initialJson = JSON.stringify(draftToJob(this.draft))
			this.touched = {}
			this.serverErrors = {}
			this.focusHeading()
		},

		// ---- closing -------------------------------------------------
		// Called by the window's close button and Esc (DesktopWindow asks
		// requestClose() first when a component has one).
		requestClose() {
			if (!this.dirty || !this.draft) {
				this.closing = true
				// An unanswered "Restore your unsaved job?" keeps the draft
				// for next time.
				if (!this.restoreOffer) writeSaved(this.autosaveKey, null)
				this.closeWindow()
				return
			}
			this.confirmWindow({
				title: this.$t('backup.wizard.close_title'),
				message: this.$t('backup.wizard.close_text'),
				confirmText: this.$t('backup.wizard.close_discard'),
				cancelText: this.$t('backup.wizard.close_keep'),
				type: 'is-danger',
				icon: 'file-remove-outline',
				onConfirm: () => {
					this.closing = true
					writeSaved(this.autosaveKey, null)
					this.closeWindow()
				}
			})
		},
		confirmWindow(options) {
			const id = `backup-wizard-confirm-${Date.now()}`
			const width = 420
			const height = 230
			this.$store.commit('OPEN_WINDOW', {
				id,
				title: options.title,
				component: 'ConfirmDialogWindow',
				props: { id, isDialog: true, hasIcon: true, iconPack: 'mdi', ...options },
				width,
				height
			})
		}
	}
}
</script>

<style lang="scss" scoped>
@import '../components/wizard-form.scss';

.backup-wizard {
	display: flex;
	flex-direction: column;
	height: 100%;
	min-width: 0;
	background: var(--theme-bg-window);
	color: var(--theme-text-primary);
}

.bw-state {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: var(--space-2);
	padding: var(--space-6);
	text-align: center;
	font-size: var(--font-sm);
	color: var(--theme-text-secondary);
}

.bw-state-title {
	font-weight: 600;
	color: var(--color-danger-fg);
}

.bw-head {
	flex-shrink: 0;
	padding: var(--space-2) var(--space-4) 0;
	border-bottom: 1px solid var(--theme-card-border);
}

.bw-body {
	// The containing block of the visually hidden labels inside it: they
	// would otherwise escape to the window and give it a second scrollbar.
	position: relative;
	flex: 1 1 auto;
	min-height: 0;
	overflow-y: auto;
	overflow-x: hidden;
	padding: var(--space-4) var(--space-5) var(--space-6);

	@media (max-width: 480px) {
		padding: var(--space-3) var(--space-4) var(--space-5);
	}
}

.bw-step {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	min-width: 0;
}

.bw-title {
	margin: 0 0 var(--space-1);
	font-size: var(--font-lg);
	font-weight: 700;
	color: var(--theme-text-primary);

	&:focus {
		outline: none;
	}
	&:focus-visible {
		@include picker-focus-ring;
	}
}

.bw-banner {
	flex-wrap: wrap;
	margin-bottom: var(--space-3);
}

.bw-banner-text {
	flex: 1 1 12rem;
	min-width: 0;
}

.bw-banner-actions {
	display: inline-flex;
	flex-wrap: wrap;
	gap: var(--space-2);
}

.bw-error-summary {
	margin-bottom: var(--space-4);
	padding: var(--space-3) var(--space-4);
	border: 2px solid var(--color-danger-fg);
	border-radius: var(--radius-sm);
	background: var(--status-danger-bg, var(--theme-danger-soft));

	h3 {
		margin: 0 0 var(--space-1);
		font-size: var(--font-sm);
		font-weight: 700;
		color: var(--status-danger-fg, var(--color-danger-fg));
	}
	ul {
		margin: 0;
		padding-left: var(--space-5);
	}
	a {
		color: var(--status-danger-fg, var(--color-danger-fg));
		font-size: var(--font-sm);
		text-decoration: underline;
	}
	a:focus-visible,
	&:focus-visible {
		@include picker-focus-ring;
	}
	&:focus {
		outline: none;
	}
}

.bw-quirks {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
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

.bw-name {
	width: 100%;
	max-width: 30rem;
}

.bw-foot {
	flex-shrink: 0;
	position: sticky;
	bottom: 0;
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-4);
	padding-bottom: calc(var(--space-3) + env(safe-area-inset-bottom, 0px));
	border-top: 1px solid var(--theme-card-border);
	background: var(--theme-bg-window);
}

.bw-foot-spacer {
	flex: 1 1 auto;
}
</style>
