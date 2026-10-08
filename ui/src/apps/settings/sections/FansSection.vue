<template>
	<section class="settings-section fans-section">
		<div class="section-header">
			<h2 class="section-title">{{ $t('Fans & Cooling') }}</h2>
			<div v-if="status && status.controllable" class="header-actions">
				<b-button rounded size="is-small" :loading="busy === 'auto'" @click="allAuto">
					<i class="mdi mdi-backup-restore mr-1"></i>{{ $t('All fans to Auto') }}
				</b-button>
			</div>
		</div>

		<!-- Service missing / unreachable -->
		<div v-if="unavailable" class="setting-card">
			<div class="setting-row">
				<b-icon class="row-icon" icon="fan-off" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Fan control is not available') }}</div>
					<div class="setting-desc">{{ $t(loadError) }} {{ $t('Updating NivaroOS installs it. Your fans keep running under BIOS control.') }}</div>
				</div>
				<div class="row-control"><b-button rounded size="is-small" @click="load">{{ $t('Retry') }}</b-button></div>
			</div>
		</div>

		<div v-else-if="!status" class="setting-card">
			<div class="setting-row"><div class="row-label setting-desc">{{ $t('Loading fans...') }}</div></div>
		</div>

		<template v-else>
			<div v-if="status.emergency" class="fan-banner is-danger" role="alert">
				<i class="mdi mdi-thermometer-alert"></i>
				<div>
					<b>{{ $t('Emergency cooling') }}</b>
					{{ $t('A temperature reached its limit, so every fan NivaroOS drives runs at 100% until it is 5 °C lower.') }}
				</div>
			</div>
			<div v-if="error" class="fan-banner is-warn" role="alert">
				<i class="mdi mdi-alert-circle-outline"></i>
				<div>{{ error }}</div>
				<button class="banner-close" :aria-label="$t('Dismiss')" @click="error = ''"><i class="mdi mdi-close"></i></button>
			</div>

			<div v-for="n in status.notes" :key="n.code" class="fan-banner" :class="n.level === 'warn' ? 'is-warn' : 'is-info'">
				<i class="mdi" :class="n.level === 'warn' ? 'mdi-alert-outline' : 'mdi-information-outline'"></i>
				<div>
					<div>{{ $t(n.message) }}</div>
					<details v-if="n.guidance" class="guidance">
						<summary>{{ $t('Advanced') }}</summary>
						<p>{{ $t(n.guidance) }}</p>
					</details>
				</div>
			</div>

			<!-- Temperatures -->
			<h3 class="setting-card-title">{{ $t('Temperatures') }}</h3>
			<div class="setting-card temps-card">
				<div v-for="t in status.temps" :key="t.id" class="temp-tile" :class="'tone-' + tone(t)">
					<div class="temp-label">{{ t.label }}</div>
					<div class="temp-value">{{ t.c === null ? '—' : Math.round(t.c) + ' °C' }}</div>
					<div class="temp-meta">{{ $t('Emergency at {c} °C', { c: t.critical_c }) }}<template v-if="t.detail"> · {{ t.detail }}</template></div>
				</div>
			</div>

			<!-- Profiles -->
			<template v-if="status.controllable">
				<h3 class="setting-card-title">{{ $t('Fan profile') }}</h3>
				<div class="setting-card">
					<div class="setting-row">
						<b-icon class="row-icon" icon="tune-variant" pack="mdi" size="is-20"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ $t('Profile') }}</div>
							<div class="setting-desc">{{ presetDesc }}</div>
						</div>
						<div class="row-control">
							<div class="segmented-control">
								<button v-for="p in presets" :key="p.id" type="button" class="segmented-option"
									:class="{ active: status.preset === p.id }" :disabled="!!busy" @click="applyPreset(p.id)">
									{{ $t(p.label) }}
								</button>
							</div>
						</div>
					</div>
				</div>
			</template>

			<!-- Fans -->
			<h3 class="setting-card-title">{{ $t('Fans') }}</h3>
			<div class="setting-card">
				<div v-if="!status.fans.length" class="setting-row">
					<b-icon class="row-icon" icon="fan-off" pack="mdi" size="is-20"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('No fans found') }}</div>
						<div class="setting-desc">{{ $t('This machine exposes no fan NivaroOS can read. The BIOS keeps controlling them.') }}</div>
					</div>
					<div class="row-control"><b-button rounded size="is-small" :loading="busy === 'detect'" @click="detect">{{ $t('Look again') }}</b-button></div>
				</div>
				<div v-for="f in status.fans" :key="f.id" class="fan-item" :class="{ open: openId === f.id }">
					<button class="setting-row fan-row" type="button" :aria-expanded="openId === f.id ? 'true' : 'false'" @click="toggle(f.id)">
						<span class="fan-icon" :class="{ spinning: spinning(f) }" :style="spinStyle(f)"><i class="mdi" :class="f.class === 'gpu' ? 'mdi-expansion-card-variant' : 'mdi-fan'"></i></span>
						<span class="row-label">
							<span class="setting-title">{{ f.label }}</span>
							<span class="setting-desc">
								<span class="state-pill" :class="'st-' + f.state">{{ $t(stateLabel(f)) }}</span>
								<span class="chip-name">{{ f.chip }} · {{ f.channel }}</span>
							</span>
						</span>
						<span class="row-control fan-readout">
							<span v-if="f.rpm !== null && f.rpm > 0" class="rpm">{{ rpm(f.rpm) }}</span>
							<span v-if="f.pct !== null" class="pct">{{ f.pct }}%</span>
							<i class="mdi chevron" :class="openId === f.id ? 'mdi-chevron-up' : 'mdi-chevron-down'"></i>
						</span>
					</button>

					<div v-if="openId === f.id" class="fan-detail">
						<p v-if="f.alert" class="fan-alert"><i class="mdi mdi-alert"></i> {{ $t(f.alert) }}</p>
						<p v-if="f.readonly_reason" class="fan-readonly"><i class="mdi mdi-lock-outline"></i> {{ $t(f.readonly_reason) }}</p>

						<div class="detail-grid">
							<label class="detail-label">{{ $t('Name') }}</label>
							<div class="detail-control">
								<b-input v-model="draft.label" size="is-small" maxlength="40" :placeholder="f.default_label" class="name-input"
									@keyup.enter="save(f, { label: draft.label })" @blur="draft.label !== f.label && save(f, { label: draft.label })"></b-input>
							</div>

							<template v-if="f.controllable">
								<label class="detail-label">{{ $t('Mode') }}</label>
								<div class="detail-control">
									<div class="segmented-control">
										<button v-for="m in modes" :key="m.id" type="button" class="segmented-option" :class="{ active: f.mode === m.id }"
											:disabled="!!busy" @click="save(f, { mode: m.id })">{{ $t(m.label) }}</button>
									</div>
								</div>

								<template v-if="f.mode === 'fixed'">
									<label class="detail-label">{{ $t('Speed') }}</label>
									<div class="detail-control range-row">
										<input v-model.number="draft.fixed" type="range" :min="f.min_pct" max="100" step="1" class="range" :aria-label="$t('Fixed speed')"
											@change="save(f, { fixed_pct: draft.fixed })">
										<span class="range-value">{{ draft.fixed }}%</span>
									</div>
								</template>

								<template v-if="f.mode === 'curve'">
									<label class="detail-label">{{ $t('Follows') }}</label>
									<div class="detail-control">
										<b-select :model-value="f.source" size="is-small" @update:modelValue="(v) => save(f, { source: v })">
											<option v-for="s in status.sources" :key="s.id" :value="s.id">{{ $t(s.label) }}</option>
										</b-select>
									</div>
								</template>
							</template>
						</div>

						<fan-curve-editor v-if="f.mode === 'curve' && f.controllable" :model-value="f.curve" :min-pct="f.min_pct" :limits="limits"
							:current-temp="sourceTemp(f)" :critical="critFor(f)" :readonly="false"
							@change="(pts) => save(f, { curve: pts })"></fan-curve-editor>

						<details v-if="f.controllable" class="fan-advanced">
							<summary>{{ $t('Safety and advanced') }}</summary>
							<div class="detail-grid">
								<label class="detail-label">{{ $t('Minimum speed') }}</label>
								<div class="detail-control range-row">
									<input v-model.number="draft.min" type="range" :min="f.floor_pct" max="100" step="1" class="range" :aria-label="$t('Minimum speed')"
										@change="save(f, { min_pct: draft.min })">
									<span class="range-value">{{ draft.min }}%</span>
								</div>
								<label class="detail-label">{{ $t('Hysteresis') }}</label>
								<div class="detail-control range-row">
									<input v-model.number="draft.hyst" type="range" min="0" :max="limits.max_hysteresis_c" step="1" class="range" :aria-label="$t('Hysteresis')"
										@change="save(f, { hysteresis_c: draft.hyst })">
									<span class="range-value">{{ draft.hyst }} °C</span>
								</div>
								<template v-if="f.kind === 'hwmon'">
									<label class="detail-label">{{ $t('Speed sensor') }}</label>
									<div class="detail-control">
										<span class="setting-desc">{{ f.tach || $t('none') }}<template v-if="!f.has_tach"> · {{ $t('no speed signal seen, so stall detection is off') }}</template></span>
									</div>
								</template>
							</div>
							<p class="setting-desc safety-note">{{ $t('Never below {n}%. If a fan NivaroOS drives stops (0 RPM), or a temperature it follows can\'t be read, it goes back to Auto. Every fan also goes back to Auto whenever the fan service stops.', { n: f.floor_pct }) }}</p>
						</details>

						<div v-if="f.can_identify" class="identify-row">
							<b-button rounded size="is-small" :loading="busy === 'identify:' + f.id" :disabled="!!busy" @click="identify(f)">
								<i class="mdi mdi-magnify mr-1"></i>{{ $t('Identify') }}
							</b-button>
							<span class="setting-desc">{{ identifyResult[f.id] || $t('Briefly changes this fan\'s speed (about 7 s) to see which fan it is.') }}</span>
						</div>
					</div>
				</div>
			</div>

			<!-- Safety -->
			<template v-if="status.controllable">
				<h3 class="setting-card-title">{{ $t('Emergency temperatures') }}</h3>
				<div class="setting-card">
					<div class="setting-row">
						<b-icon class="row-icon" icon="thermometer-alert" pack="mdi" size="is-20"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ $t('CPU') }}</div>
							<div class="setting-desc">{{ $t('At this temperature every fan NivaroOS drives goes to 100%') }} ({{ status.limits.min_critical_c }}–{{ status.limits.max_critical_cpu_c }} °C)</div>
						</div>
						<div class="row-control">
							<b-input v-model.number="critCPU" type="number" size="is-small" class="crit-input" :min="status.limits.min_critical_c" :max="status.limits.max_critical_cpu_c"
								@keyup.enter="saveCritical" @blur="saveCritical"></b-input>
							<span class="unit">°C</span>
						</div>
					</div>
					<div v-if="hasGPU" class="setting-row">
						<b-icon class="row-icon" icon="expansion-card-variant" pack="mdi" size="is-20"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ $t('GPU') }}</div>
							<div class="setting-desc">({{ status.limits.min_critical_c }}–{{ status.limits.max_critical_gpu_c }} °C)</div>
						</div>
						<div class="row-control">
							<b-input v-model.number="critGPU" type="number" size="is-small" class="crit-input" :min="status.limits.min_critical_c" :max="status.limits.max_critical_gpu_c"
								@keyup.enter="saveCritical" @blur="saveCritical"></b-input>
							<span class="unit">°C</span>
						</div>
					</div>
				</div>
			</template>
		</template>
	</section>
</template>

<script>
import fansApi from '@/service/fans'
import FanCurveEditor from '@/apps/settings/FanCurveEditor.vue'
import { DEFAULT_LIMITS, PRESETS, fanStateLabel, formatRpm, tempTone } from '@/utils/fans/curve'

// Search index (see SettingsApp.vue).
export const ROWS = [
	{ label: 'Fans & Cooling', keywords: 'fan speed rpm cooling noise quiet temperature cpu gpu curve pwm' },
	{ label: 'Fan profile', keywords: 'quiet balanced performance auto preset' },
	{ label: 'Emergency temperatures', keywords: 'overheat critical thermal' }
]

const POLL_MS = 2000

export default {
	name: 'fans-section',
	components: { FanCurveEditor },
	props: { api: { type: Object, default: () => fansApi } },
	data() {
		return {
			status: null,
			unavailable: false,
			loadError: '',
			error: '',
			busy: '',
			openId: null,
			draft: { label: '', fixed: 50, min: 20, hyst: 3 },
			identifyResult: {},
			critCPU: 85,
			critGPU: 83,
			timer: null,
			presets: PRESETS,
			modes: [
				{ id: 'auto', label: 'Auto' },
				{ id: 'fixed', label: 'Fixed' },
				{ id: 'curve', label: 'Curve' }
			]
		}
	},
	computed: {
		limits() {
			return (this.status && this.status.limits) || DEFAULT_LIMITS
		},
		hasGPU() {
			return !!(this.status && this.status.temps.some((t) => t.id.startsWith('gpu:')))
		},
		presetDesc() {
			const p = PRESETS.find((x) => x.id === this.status.preset)
			return this.$t(p ? p.desc : 'Custom: fans are set one by one')
		}
	},
	created() {
		this.load()
		this.timer = setInterval(() => {
			if (!document.hidden && !this.busy) this.load(true)
		}, POLL_MS)
	},
	beforeUnmount() {
		clearInterval(this.timer)
	},
	methods: {
		async load(quiet) {
			try {
				this.setStatus(await this.api.status())
				this.unavailable = false
			} catch (e) {
				if (e.unavailable) {
					this.unavailable = true
					this.loadError = e.message
				} else if (!quiet) {
					this.error = e.message
				}
			}
		},
		setStatus(s) {
			const first = !this.status
			this.status = s
			if (first || document.activeElement === document.body) {
				this.critCPU = s.critical_cpu_c
				this.critGPU = s.critical_gpu_c
			}
			if (this.openId && !s.fans.some((f) => f.id === this.openId)) this.openId = null
		},
		fillDraft(f) {
			this.draft = { label: f.label === f.default_label ? '' : f.label, fixed: f.fixed_pct, min: f.min_pct, hyst: f.hysteresis_c }
		},
		toggle(id) {
			this.openId = this.openId === id ? null : id
			const f = this.status.fans.find((x) => x.id === id)
			if (f) this.fillDraft(f)
		},
		async run(tag, fn) {
			this.busy = tag
			this.error = ''
			try {
				const s = await fn()
				if (s && s.fans) this.setStatus(s)
				return s
			} catch (e) {
				this.error = e.message
				return null
			} finally {
				this.busy = ''
			}
		},
		async save(f, changes) {
			await this.run('save', () => this.api.updateFan(f.id, changes))
			const nf = this.status.fans.find((x) => x.id === f.id)
			if (nf) this.fillDraft(nf)
		},
		applyPreset(id) {
			return this.run('preset', () => this.api.applyPreset(id))
		},
		allAuto() {
			return this.run('auto', () => this.api.allAuto())
		},
		detect() {
			return this.run('detect', () => this.api.detect())
		},
		async identify(f) {
			const res = await this.run('identify:' + f.id, () => this.api.identify(f.id))
			if (res) this.identifyResult[f.id] = res.message
			this.load(true)
		},
		saveCritical() {
			const s = this.status
			if (this.critCPU === s.critical_cpu_c && this.critGPU === s.critical_gpu_c) return
			return this.run('crit', () => this.api.setCritical(this.critCPU, this.hasGPU ? this.critGPU : undefined)).then(() => {
				this.critCPU = this.status.critical_cpu_c
				this.critGPU = this.status.critical_gpu_c
			})
		},
		sourceTemp(f) {
			const src = f.source === 'max' ? null : f.source || 'cpu'
			if (!src) {
				const all = this.status.temps.map((t) => t.c).filter((c) => c !== null)
				return all.length ? Math.max(...all) : null
			}
			const t = this.status.temps.find((x) => x.id === src)
			return t ? t.c : null
		},
		critFor(f) {
			return (f.source || '').startsWith('gpu:') ? this.status.critical_gpu_c : this.status.critical_cpu_c
		},
		tone(t) {
			return tempTone(t.c, t.critical_c)
		},
		stateLabel(f) {
			return fanStateLabel(f)
		},
		rpm(v) {
			return formatRpm(v)
		},
		spinning(f) {
			return (f.rpm !== null && f.rpm > 0) || (f.rpm === null && f.pct > 0)
		},
		spinStyle(f) {
			const speed = f.rpm ? Math.min(1, f.rpm / 2500) : (f.pct || 0) / 100
			return { animationDuration: `${(2.4 - 1.9 * speed).toFixed(2)}s` }
		}
	}
}
</script>

<style lang="scss" scoped>
.fan-banner {
	display: flex;
	gap: 0.75rem;
	align-items: flex-start;
	padding: 0.75rem 1rem;
	margin-bottom: 1rem;
	border-radius: 12px;
	font-size: 0.825rem;
	line-height: 1.4;
	border: 1px solid var(--theme-card-border);
	background: var(--theme-card-subtle);
	color: var(--theme-text-primary);

	> .mdi {
		font-size: 1.15rem;
		line-height: 1.2;
	}

	&.is-danger {
		background: var(--color-danger-soft, rgba(239, 68, 68, 0.1));
		border-color: var(--color-danger, #ef4444);
		> .mdi { color: var(--color-danger-fg, #dc2626); }
	}

	&.is-warn {
		background: var(--color-warning-soft, rgba(245, 158, 11, 0.1));
		> .mdi { color: var(--color-warning-fg, #d97706); }
	}

	&.is-info > .mdi {
		color: var(--color-primary-fg, #2563eb);
	}

	.banner-close {
		margin-left: auto;
		border: none;
		background: transparent;
		color: var(--theme-text-muted);
		cursor: pointer;
	}

	.guidance {
		margin-top: 0.35rem;
		color: var(--theme-text-secondary);

		summary {
			cursor: pointer;
			font-weight: 500;
		}

		p {
			margin-top: 0.35rem;
		}
	}
}

.temps-card {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
}

.temp-tile {
	padding: 0.9rem 1.25rem;
	border-right: 1px solid var(--theme-card-border);

	&:last-child {
		border-right: none;
	}

	.temp-label {
		font-size: 0.775rem;
		color: var(--theme-text-muted);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.temp-value {
		font-size: 1.5rem;
		font-weight: 600;
		font-variant-numeric: tabular-nums;
		color: var(--theme-text-primary);
		line-height: 1.3;
	}

	.temp-meta {
		font-size: 0.7rem;
		color: var(--theme-text-muted);
	}

	&.tone-warn .temp-value { color: var(--color-warning-fg, #d97706); }
	&.tone-danger .temp-value { color: var(--color-danger-fg, #dc2626); }
}

.setting-row .segmented-control {
	margin-bottom: 0;
}

.fan-item {
	border-bottom: 1px solid var(--theme-card-border);

	&:last-child {
		border-bottom: none;
	}

	&.open {
		background: var(--theme-card-subtle);
	}
}

.fan-row {
	width: 100%;
	border: none;
	border-bottom: none !important;
	background: transparent;
	text-align: left;
	cursor: pointer;
	font: inherit;
	color: inherit;

	.row-label {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}

	.setting-desc {
		display: flex;
		flex-wrap: wrap;
		gap: 0.4rem;
		align-items: center;
	}
}

.fan-icon {
	flex-shrink: 0;
	width: 2rem;
	height: 2rem;
	margin-right: 0.85rem;
	border-radius: 10px;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	background: var(--color-primary-soft, rgba(37, 99, 235, 0.1));
	color: var(--color-primary-fg, #2563eb);
	font-size: 1.15rem;

	&.spinning .mdi-fan {
		animation: fan-spin linear infinite;
		animation-duration: inherit;
	}
}

@keyframes fan-spin {
	to { transform: rotate(360deg); }
}

@media (prefers-reduced-motion: reduce) {
	.fan-icon.spinning .mdi-fan { animation: none; }
}

.state-pill {
	font-size: 0.7rem;
	font-weight: 500;
	padding: 0.05rem 0.5rem;
	border-radius: 999px;
	background: var(--theme-pill-bg, rgba(0, 0, 0, 0.05));
	color: var(--theme-text-secondary);

	&.st-manual { background: var(--color-primary-soft); color: var(--color-primary-fg); }
	&.st-emergency { background: var(--color-danger-soft); color: var(--color-danger-fg); }
	&.st-readonly { opacity: 0.8; }
}

.chip-name {
	font-variant-numeric: tabular-nums;
}

.fan-readout {
	gap: 0.75rem;
	font-variant-numeric: tabular-nums;
	font-size: 0.825rem;

	.rpm { color: var(--theme-text-primary); font-weight: 500; }
	.pct { color: var(--theme-text-muted); min-width: 2.5rem; text-align: right; }
	.chevron { color: var(--theme-text-muted); font-size: 1.1rem; }
}

.fan-detail {
	padding: 0.25rem 1.25rem 1.1rem 4.1rem;
}

.fan-alert,
.fan-readonly {
	font-size: 0.8rem;
	margin-bottom: 0.75rem;
	color: var(--theme-text-secondary);
}

.fan-alert .mdi { color: var(--color-warning-fg, #d97706); }

.detail-grid {
	display: grid;
	grid-template-columns: 8rem 1fr;
	gap: 0.6rem 1rem;
	align-items: center;
	margin-bottom: 0.9rem;
}

.detail-label {
	font-size: 0.8rem;
	color: var(--theme-text-muted);
}

.detail-control .segmented-control {
	margin-bottom: 0;
}

.name-input {
	max-width: 16rem;
}

.range-row {
	display: flex;
	align-items: center;
	gap: 0.75rem;
	max-width: 22rem;
}

.range {
	flex: 1;
	accent-color: var(--color-primary, #2563eb);
}

.range-value {
	min-width: 3rem;
	font-size: 0.825rem;
	font-weight: 500;
	font-variant-numeric: tabular-nums;
}

.fan-advanced {
	margin-top: 0.9rem;

	summary {
		cursor: pointer;
		font-size: 0.8rem;
		font-weight: 500;
		color: var(--theme-text-secondary);
		margin-bottom: 0.6rem;
	}

	.safety-note {
		font-size: 0.75rem;
		color: var(--theme-text-muted);
	}
}

.identify-row {
	display: flex;
	align-items: center;
	gap: 0.75rem;
	margin-top: 0.9rem;
	font-size: 0.775rem;
	color: var(--theme-text-muted);
}

.crit-input {
	width: 5rem;
}

.unit {
	margin-left: 0.4rem;
	font-size: 0.825rem;
	color: var(--theme-text-muted);
}

@media (max-width: 640px) {
	.fan-detail {
		padding-left: 1.25rem;
	}

	.detail-grid {
		grid-template-columns: 1fr;
		gap: 0.3rem;
	}

	.temp-tile {
		border-right: none;
		border-bottom: 1px solid var(--theme-card-border);
	}
}
</style>
