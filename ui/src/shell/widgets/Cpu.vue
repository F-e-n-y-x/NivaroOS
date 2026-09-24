<template>
	<div class="widget cpu">
		<div class="blur-background"></div>
		<div class="widget-content">
			<!-- Header -->
			<div class="widget-header">
				<div class="widget-header-left">
					<div class="widget-badge is-cpu">
						<i class="mdi mdi-cpu-64-bit"></i>
					</div>
					<div class="widget-header-text">
						<span class="widget-title">{{ $t("Processor") }}</span>
						<span class="widget-header-meta" :title="cpuModelShort">
							{{ cpuModelShort || (cpuVendor ? $t('{vendor} processor', { vendor: cpuVendor }) : $t('System CPU')) }}
						</span>
					</div>
				</div>
				<div class="widget-header-right">
					<button
						v-if="percpu.length > 1"
						type="button"
						:class="{ active: showCores }"
						:title="$t('Toggle per-core usage')"
						:aria-label="$t('Toggle per-core usage')"
						:aria-pressed="showCores ? 'true' : 'false'"
						class="widget-icon-btn"
						@click="toggleCores"
					>
						<i class="mdi mdi-view-grid-outline" aria-hidden="true"></i>
					</button>
					<button
						type="button"
						class="widget-icon-btn"
						:title="$t('Processes')"
						:aria-label="$t('Processes')"
						:aria-expanded="showMore ? 'true' : 'false'"
						@click="showMoreInfo"
					>
						<b-icon :class="{ open: showMore }" class="arrow-btn" icon="right-outline" pack="casa"></b-icon>
					</button>
				</div>
			</div>

			<!-- Hero Grid: Left = Dial, Right = 2x2 Bento Spec Grid -->
			<div class="widget-hero-grid">
				<div class="hero-gauge-box">
					<radial-bar
						:percent="parseInt(cpuSeries)"
						label="CPU"
					></radial-bar>
				</div>
				<div class="hero-info-bento">
					<div class="bento-specs-grid">
						<div class="spec-tile" :title="$t('Clock Speed')">
							<span class="spec-label">{{ $t('Clock') }}</span>
							<span class="spec-val">{{ mhz ? (mhz / 1000).toFixed(1) + ' GHz' : $t('Dynamic') }}</span>
						</div>
						<div class="spec-tile" :title="$t('Cores and Threads')">
							<span class="spec-label">{{ $t('Cores') }}</span>
							<span class="spec-val">{{ coresLabel }}</span>
						</div>
						<button
							type="button"
							class="spec-tile spec-tile-btn"
							:title="$t('Temperature / Power (click to switch °C/°F)')"
							:aria-label="$t('Temperature {temp}, power {power}. Switch between °C and °F', { temp: temperature, power: power || '—' })"
							@click="changeFormat"
						>
							<span class="spec-label">{{ $t('Thermals') }}</span>
							<span class="spec-val">{{ temperature }}{{ power ? ' · ' + power : '' }}</span>
						</button>
						<div class="spec-tile" :title="$t('Load State')">
							<span class="spec-label">{{ $t('Load') }}</span>
							<span class="spec-val">{{ loadState }}</span>
						</div>
					</div>
				</div>
			</div>

			<!-- Realtime Cores Visualizer Matrix -->
			<div v-if="percpu.length > 1 && showCores" class="mini-cores-visualizer">
				<div
					v-for="(p, index) in percpu"
					:key="'core-' + index"
					class="mini-core-item"
					:title="`${$t('Core')} ${index + 1}: ${Math.round(p)}%`"
				>
					<div class="mini-core-track">
						<div class="mini-core-fill" :style="{ height: Math.min(100, Math.max(6, p)) + '%' }"></div>
					</div>
					<span class="mini-core-label">{{ Math.round(p) }}%</span>
				</div>
			</div>

			<!-- Top Container CPU Processes -->
			<div v-if="showMore" class="more-info">
				<div class="process-section-title">{{ $t("Top Processes") }}</div>
				<div v-if="containerCpuList.length === 0" class="has-text-centered is-size-7 py-2 text-muted">
					{{ $t("No active container processes") }}
				</div>
				<div v-for="(item, index) in containerCpuList" :key="item.title + index + '-cpu'">
					<div v-if="!isNaN(item.usage)" class="process-row">
						<div class="is-flex is-align-items-center is-clipped">
							<b-image
								:lazy="false"
								:src="item.icon"
								:src-fallback="$assetUrl(require('@/assets/img/app-icons/default.svg'))"
								class="is-16x16 mr-2 is-flex-shrink-0"
							></b-image>
							<span class="one-line process-name">{{ item.title }}</span>
						</div>
						<div class="is-flex-shrink-0 process-usage">{{ item.usage }}%</div>
					</div>
				</div>
			</div>
		</div>
	</div>
</template>

<script>
import smoothReflow from "vue-smooth-reflow";
import orderBy from "lodash/orderBy";
import slice from "lodash/slice";
import { mixin } from "@/mixins/mixin";
import RadialBar from "@/shared/widgets/RadialBar.vue";
import { subscribeContainerUsage } from "@/utils/containerUsagePoller.js";

// °C/°F is one setting for every widget (CPU and GPU): stored in
// localStorage and broadcast with a window event so a switch in one widget
// updates the others at once (the native "storage" event covers other tabs).
const TEMPERATURE_KEY = "temperatureFormat";
const TEMPERATURE_EVENT = "nivaroos:temperature-format";
function readTemperatureFormat() {
	try {
		return localStorage.getItem(TEMPERATURE_KEY) === "°F" ? "°F" : "°C";
	} catch (e) {
		return "°C";
	}
}
function writeTemperatureFormat(format) {
	try {
		localStorage.setItem(TEMPERATURE_KEY, format);
	} catch (e) { /* private mode */ }
	window.dispatchEvent(new CustomEvent(TEMPERATURE_EVENT, { detail: format }));
}

export default {
	// eslint-disable-next-line vue/multi-word-component-names
	name: "cpu",
	icon: "system-outline",
	title: "CPU Status",
	gridCols: 3,
	gridRows: 2,
	initShow: true,
	mixins: [smoothReflow, mixin],
	components: {
		RadialBar,
	},

	data() {
		return {
			unsubscribeUsage: null,
			showMore: false,
			showCores: localStorage.getItem("cpuShowCores") !== "false",
			cpuCores: 0,
			modelName: "",
			mhz: 0,
			cpuSeries: 0,
			percpu: [],
			containerCpuList: [],
			temperatureFormat: readTemperatureFormat(),
			// null = the board exposes no usable CPU sensor (VMs, LXC,
			// some ARM boards) -> shown as "—", never as a fake 0°C.
			orgTemperature: null,
			power: "",
			lastEnergy: null,
		};
	},
	computed: {
		temperature() {
			const c = this.orgTemperature;
			if (c === null || c === undefined || !isFinite(c) || c <= 0) return "—";
			return this.temperatureFormat === "°F"
				? this.celsiusToFahrenheit(c) + "°F"
				: Math.round(c) + "°C";
		},
		loadState() {
			const p = parseInt(this.cpuSeries);
			if (p < 30) return this.$t("Light");
			if (p < 75) return this.$t("Moderate");
			return this.$t("Heavy");
		},
		cpuModelParts() {
			return this.splitCpuModelName(this.modelName);
		},
		// "6C / 12T"; just the thread count until the core count is known.
		coresLabel() {
			const threads = this.percpu.length || this.cpuCores;
			if (!this.cpuCores) return threads ? `${threads}T` : "—";
			return `${this.cpuCores}C / ${threads}T`;
		},
		cpuVendor() {
			return this.cpuModelParts.vendor;
		},
		cpuModelShort() {
			return this.cpuModelParts.model;
		},
	},
	watch: {
		// Home.vue refetches the hardware snapshot (retry after a failed
		// first load); follow it instead of keeping the first copy forever.
		"$store.state.hardwareInfo.cpu": {
			handler(cpu) {
				if (!cpu) return;
				this.cpuCores = cpu.num || this.cpuCores;
				this.modelName = cpu.model_name || this.modelName;
				if (cpu.mhz) this.mhz = cpu.mhz;
			},
		},
	},
	created() {
		const cpu = this.$store.state.hardwareInfo.cpu || {};
		this.cpuCores = cpu.num || 0;
		this.modelName = cpu.model_name || "";
		this.mhz = cpu.mhz || 0;
		this.updateCharts(cpu);
		window.addEventListener(TEMPERATURE_EVENT, this.onTemperatureFormat);
		window.addEventListener("storage", this.onTemperatureStorage);
	},
	mounted() {
		this.$smoothReflow({
			el: ".widget",
			property: ["height"],
		});
	},
	beforeDestroy() {
		if (this.unsubscribeUsage) {
			this.unsubscribeUsage();
		}
		window.removeEventListener(TEMPERATURE_EVENT, this.onTemperatureFormat);
		window.removeEventListener("storage", this.onTemperatureStorage);
	},
	methods: {
		splitCpuModelName(raw) {
			if (!raw) return { vendor: "", model: "" };
			let s = raw
				.replace(/\(R\)/gi, "")
				.replace(/\(TM\)/gi, "")
				.replace(/\bCPU\b/gi, "")
				.replace(/@\s*[\d.]+\s*[MG]Hz/gi, "")
				.replace(/\d+(st|nd|rd|th)\s+Gen\s+/gi, "")
				.replace(/\d+-Core Processor/gi, "")
				.replace(/\s{2,}/g, " ")
				.trim();

			const knownVendors = ["AMD", "Intel", "Apple", "Qualcomm", "Ampere", "ARM"];
			let vendor = "";
			for (const v of knownVendors) {
				if (s.toLowerCase().startsWith(v.toLowerCase())) {
					vendor = v;
					s = s.slice(v.length).trim();
					break;
				}
			}
			return { vendor, model: s };
		},

		celsiusToFahrenheit(celsius) {
			let fahrenheit = (celsius * 9) / 5 + 32;
			return Math.round(fahrenheit);
		},

		changeFormat() {
			this.temperatureFormat = this.temperatureFormat === "°C" ? "°F" : "°C";
			writeTemperatureFormat(this.temperatureFormat);
		},

		onTemperatureFormat(e) {
			if (e && e.detail) this.temperatureFormat = e.detail;
		},

		onTemperatureStorage(e) {
			if (e && e.key === TEMPERATURE_KEY) this.temperatureFormat = readTemperatureFormat();
		},

		updateCharts(cpu) {
			if (!cpu) return;
			this.cpuSeries = cpu.percent || 0;
			this.percpu = cpu.percpu || [];
			if (cpu.num) this.cpuCores = cpu.num;
			if (cpu.mhz) this.mhz = cpu.mhz;
			const t = cpu.temperature;
			this.orgTemperature = typeof t === "number" && t > 0 ? t : null;
			this.updatePower(cpu.power);
		},

		// power = { value: energy counter in µJ | null, timestamp: ms,
		// max: counter range in µJ } (older backends: strings, seconds).
		// Watts = Δenergy / Δt; guards against no RAPL (value null/"0"),
		// Δt <= 0 and the counter wrapping at max_energy_range_uj.
		updatePower(p) {
			const value = p && p.value !== null && p.value !== undefined && p.value !== "" ? Number(p.value) : NaN;
			let ts = p ? Number(p.timestamp) : NaN;
			if (!isFinite(value) || value <= 0 || !isFinite(ts)) {
				this.lastEnergy = null;
				this.power = "";
				return;
			}
			if (ts < 1e12) ts *= 1000; // legacy backend sent seconds
			const max = p && Number(p.max) > 0 ? Number(p.max) : 0;
			const prev = this.lastEnergy;
			this.lastEnergy = { value, ts };
			if (!prev) return;
			const dt = (ts - prev.ts) / 1000;
			if (!(dt > 0)) return;
			let dE = value - prev.value;
			if (dE < 0) {
				if (!max) return; // wrapped but range unknown: skip sample
				dE += max;
			}
			const watts = dE / 1e6 / dt;
			if (!isFinite(watts) || watts < 0 || watts > 5000) return;
			this.power = watts.toFixed(1) + "W";
		},

		applyUsage(res) {
			let id = 0;
			this.containerCpuList = res.data.data.map((item) => {
				let usage = 0;
				if (item.previous != null) {
					const cpu_delta =
						item.data.cpu_stats.cpu_usage.total_usage - item.previous.cpu_stats.cpu_usage.total_usage;
					const system_cpu_delta =
						item.data.cpu_stats.system_cpu_usage - item.previous.cpu_stats.system_cpu_usage + 1;
					usage = Math.floor((cpu_delta / system_cpu_delta) * 1000) / 10;
				}
				id++;
				return {
					id: id,
					usage: isNaN(usage) || usage < 0 ? 0 : usage,
					icon: item.icon,
					title: item.title,
				};
			});
			this.containerCpuList = slice(orderBy(this.containerCpuList, ["usage"], ["desc"]), 0, 8);
		},

		showMoreInfo() {
			this.showMore = !this.showMore;
			if (this.showMore) {
				this.unsubscribeUsage = subscribeContainerUsage((res) => this.applyUsage(res));
			} else {
				if (this.unsubscribeUsage) {
					this.unsubscribeUsage();
					this.unsubscribeUsage = null;
				}
			}
		},

		toggleCores() {
			this.showCores = !this.showCores;
			localStorage.setItem("cpuShowCores", this.showCores);
		},

	},
	sockets: {
		"nivaroos:system:utilization"(res) {
			if (document.hidden) return;
			let cpu;
			try {
				cpu = JSON.parse(res.Properties.sys_cpu);
			} catch (e) {
				return;
			}
			this.updateCharts(cpu);
		},
	},
};
</script>

<style lang="scss">
.widget {
	&.cpu {
		.spec-tile-btn {
			font: inherit;
			text-align: left;
			cursor: pointer;
			width: 100%;
			margin: 0;

			&:focus-visible {
				outline: 2px solid var(--color-primary-fg, #1d4ed8);
				outline-offset: 1px;
			}
		}

		.arrow-btn {
			transition: transform 0.25s ease;

			&.open {
				transform: rotate(90deg);
			}
		}
	}
}
</style>
