<template>
	<div class="widget gpu">
		<div class="blur-background"></div>
		<div class="widget-content">
			<!-- Header Start -->
			<div class="widget-header">
				<div class="widget-header-left">
					<div class="widget-badge is-gpu">
						<i class="mdi mdi-expansion-card"></i>
					</div>
					<div class="widget-header-text">
						<span class="widget-title">{{ $t("Graphics") }}</span>
						<span class="widget-header-meta" :title="gpuName">
							{{ gpuName ? gpuName + (driverVersion ? ' · v' + driverVersion : '') : (!unavailable ? $t("Discrete Graphics") : $t("Integrated Display")) }}
						</span>
					</div>
				</div>
				<div class="widget-header-right">
					<button
						v-if="!unavailable"
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
			<!-- Header End -->

			<!-- Hero Grid: Left = Dial (outside box), Right = VRAM Meter Card + Temp & Power Cards -->
			<div v-if="!unavailable" class="widget-hero-grid">
				<div class="hero-gauge-box">
					<radial-bar
						:percent="Math.round(utilizationPercent)"
						label="GPU"
					></radial-bar>
				</div>
				<div class="hero-info-bento">
					<!-- VRAM Meter Card with Live Progress Track -->
					<div class="vram-meter-card" :title="$t('VRAM Usage') + ` (${Math.round(memoryPercent)}%)`">
						<div class="vram-card-top">
							<span class="vram-label">{{ $t('VRAM') }}</span>
							<div class="vram-metrics">
								<span class="vram-usage-text">{{ renderSize(memoryUsedBytes) }} / {{ renderSize(memoryTotalBytes) }}</span>
								<span class="vram-pct-badge">{{ Math.round(memoryPercent) }}%</span>
							</div>
						</div>
						<div class="vram-progress-track">
							<div
								class="vram-progress-fill"
								:style="{ width: Math.min(100, Math.max(3, Math.round(memoryPercent))) + '%' }"
							></div>
						</div>
					</div>

					<!-- Bottom Specs Grid: Temp & Power (Driver detail removed) -->
					<div class="bento-specs-grid">
						<div class="spec-tile" :title="$t('Temperature')">
							<span class="spec-label">{{ $t('Temp') }}</span>
							<span class="spec-val">{{ temperatureDisplay }}</span>
						</div>
						<div class="spec-tile" :title="$t('Power Consumption')">
							<span class="spec-label">{{ $t('Power') }}</span>
							<span class="spec-val">{{ powerDraw > 0 ? powerDraw.toFixed(0) + 'W' : '—' }}</span>
						</div>
					</div>
				</div>
			</div>

			<!-- Sleek Fallback when no Discrete GPU is Detected -->
			<div v-else-if="driverSuggestion" class="gpu-unavailable-card">
				<i class="mdi mdi-download-circle-outline gpu-unavail-icon"></i>
				<div class="gpu-unavail-title">{{ $t("GPU driver not installed") }}</div>
				<div class="gpu-unavail-desc">
					{{ $t("Detected a {vendor} GPU with no working driver.", { vendor: driverSuggestionLabel }) }}
				</div>
				<button type="button" class="widget-icon-btn install-driver-btn" @click="installDriver">
					{{ $t("Install Driver") }}
				</button>
				<div v-if="installError" class="gpu-unavail-desc install-error">{{ installError }}</div>
			</div>
			<div v-else class="gpu-unavailable-card">
				<i class="mdi mdi-monitor-dashboard gpu-unavail-icon"></i>
				<div class="gpu-unavail-title">{{ $t("Integrated Graphics") }}</div>
				<div class="gpu-unavail-desc">{{ $t("No dedicated GPU with live stats was found on this system.") }}</div>
			</div>

			<!-- Top GPU Processes -->
			<div v-if="showMore && !unavailable" class="more-info">
				<div class="process-section-title">{{ $t("Top GPU Processes") }}</div>
				<div v-if="processes.length === 0" class="has-text-centered is-size-7 py-2 text-muted">
					{{ $t("No active GPU processes") }}
				</div>
				<div v-for="(item, index) in processes" :key="item.pid + '-' + index" class="process-row">
					<div class="is-flex-grow-1 is-flex is-align-items-center is-clipped">
						<span class="one-line process-name">{{ item.command }} ({{ item.pid }})</span>
					</div>
					<div class="is-flex-shrink-0 process-usage">{{ item.usage }}%</div>
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

import { instance as http } from "@/service/service.js";

// The gpu-sidecar is reached same-origin through the gateway
// (/v1/gpu/<endpoint> -> 127.0.0.1:28640/<endpoint>) with the normal auth
// header from the shared axios instance - no extra port to open, no
// mixed-content failure behind HTTPS.
const GPU_STATS_URL = "/v1/gpu/gpu-stats";
const DRIVER_STATUS_URL = "/v1/gpu/driver-status";
const REQUEST_TIMEOUT_MS = 8000;
// Run directly in a real terminal (see installDriver()) rather than through
// gpu-sidecar's own /driver-install HTTP endpoint - a silent background
// request can't show the admin a multi-minute package install, or let them
// answer a prompt (license text, "keep local config file?", etc.).
const DRIVER_SCRIPT_PATH = "/usr/local/bin/nivaroos-gpu-driver-install.sh";
// nvidia-smi is run per request: fast while the process list is open,
// slower while collapsed, and backing off exponentially (up to 5 min) on a
// machine without a GPU/driver instead of hammering the sidecar forever.
const POLL_INTERVAL_EXPANDED_MS = 2000;
const POLL_INTERVAL_COLLAPSED_MS = 6000;
const POLL_BACKOFF_MAX_MS = 5 * 60 * 1000;

const TEMPERATURE_KEY = "temperatureFormat";
const TEMPERATURE_EVENT = "nivaroos:temperature-format";
function readTemperatureFormat() {
	try {
		return localStorage.getItem(TEMPERATURE_KEY) === "°F" ? "°F" : "°C";
	} catch (e) {
		return "°C";
	}
}

export default {
	// eslint-disable-next-line vue/multi-word-component-names
	name: "gpu",
	icon: "system-outline",
	title: "GPU Status",
	gridCols: 3,
	gridRows: 2,
	initShow: true,
	mixins: [smoothReflow, mixin],
	components: {
		RadialBar,
	},

	data() {
		return {
			timer: null,
			inFlight: false,
			failures: 0,
			driverChecked: false,
			showMore: false,
			unavailable: false,
			gpuName: "",
			driverVersion: "",
			utilizationPercent: 0,
			memoryUsedBytes: 0,
			memoryTotalBytes: 0,
			temperature: 0,
			powerDraw: 0,
			processes: [],
			driverSuggestion: null,
			installError: "",
			temperatureFormat: readTemperatureFormat(),
		};
	},
	computed: {
		memoryPercent() {
			if (!this.memoryTotalBytes) return 0;
			return (this.memoryUsedBytes / this.memoryTotalBytes) * 100;
		},
		temperatureDisplay() {
			if (!(this.temperature > 0)) return "—";
			if (this.temperatureFormat === "°F") {
				return Math.round((this.temperature * 9) / 5 + 32) + "°F";
			}
			return Math.round(this.temperature) + "°C";
		},
		driverSuggestionLabel() {
			const labels = { nvidia: "NVIDIA", amd: "AMD", intel: "Intel" };
			return labels[this.driverSuggestion] || this.driverSuggestion;
		},
	},
	created() {
		this.poll();
		window.addEventListener(TEMPERATURE_EVENT, this.onTemperatureFormat);
		window.addEventListener("storage", this.onTemperatureStorage);
		document.addEventListener("visibilitychange", this.onVisibility);
	},
	mounted() {
		this.$smoothReflow({
			el: ".widget",
			property: ["height"],
		});
	},
	beforeDestroy() {
		this.destroyed_ = true;
		clearTimeout(this.timer);
		window.removeEventListener(TEMPERATURE_EVENT, this.onTemperatureFormat);
		window.removeEventListener("storage", this.onTemperatureStorage);
		document.removeEventListener("visibilitychange", this.onVisibility);
	},
	methods: {
		schedule() {
			clearTimeout(this.timer);
			if (this.destroyed_) return;
			let delay = this.showMore ? POLL_INTERVAL_EXPANDED_MS : POLL_INTERVAL_COLLAPSED_MS;
			if (this.failures > 0) {
				delay = Math.min(POLL_BACKOFF_MAX_MS, POLL_INTERVAL_COLLAPSED_MS * Math.pow(2, this.failures - 1));
			}
			this.timer = setTimeout(this.poll, delay);
		},

		poll() {
			// Hidden tab: don't poll at all; onVisibility() resumes.
			if (document.hidden || this.inFlight || this.destroyed_) return;
			this.inFlight = true;
			http.get(GPU_STATS_URL, { timeout: REQUEST_TIMEOUT_MS })
				.then((res) => {
					const data = res && res.data;
					if (!data || typeof data !== "object" || data.error || !data.name) {
						throw new Error((data && data.error) || "no gpu");
					}
					this.failures = 0;
					this.unavailable = false;
					this.driverSuggestion = null;
					this.gpuName = data.name || "";
					this.driverVersion = data.driver_version || "";
					this.utilizationPercent = data.utilization_percent || 0;
					this.memoryUsedBytes = (data.memory_used_mib || 0) * 1024 * 1024;
					this.memoryTotalBytes = (data.memory_total_mib || 0) * 1024 * 1024;
					this.temperature = data.temperature_c || 0;
					this.powerDraw = data.power_draw_w || 0;
					const procs = (data.processes || []).map((p) => ({
						pid: p.pid,
						command: p.command,
						usage: Math.round(p.utilization_percent || 0),
					}));
					this.processes = slice(orderBy(procs, ["usage"], ["desc"]), 0, 8);
				})
				.catch(() => {
					this.unavailable = true;
					this.failures++;
					// Once per failure streak, not on every backed-off retry.
					if (!this.driverChecked) this.checkDriverStatus();
				})
				.finally(() => {
					this.inFlight = false;
					this.schedule();
				});
		},

		checkDriverStatus() {
			this.driverChecked = true;
			http.get(DRIVER_STATUS_URL, { timeout: REQUEST_TIMEOUT_MS })
				.then((res) => {
					const gpus = (res && res.data && res.data.gpus) || [];
					const broken = gpus.find((g) => !g.driver_working);
					this.driverSuggestion = broken ? broken.vendor : null;
				})
				.catch(() => {
					this.driverSuggestion = null;
				});
		},

		// Retry right away (and re-check the driver) when the page comes
		// back - e.g. after the driver install terminal was used.
		onVisibility() {
			if (document.hidden) return;
			if (this.unavailable) {
				this.failures = Math.min(this.failures, 1);
				this.driverChecked = false;
			}
			clearTimeout(this.timer);
			this.poll();
		},

		onTemperatureFormat(e) {
			if (e && e.detail) this.temperatureFormat = e.detail;
		},

		onTemperatureStorage(e) {
			if (e && e.key === TEMPERATURE_KEY) this.temperatureFormat = readTemperatureFormat();
		},

		// Opens a real terminal with the install command pre-typed, rather
		// than running it silently over a background request - a package
		// install can prompt, take minutes, or fail in a way a single toast
		// can't explain. The terminal is the same /v1/sys/wsterm session as
		// the desktop's Terminal app - it runs as the machine's regular
		// desktop user, not root, so `sudo` prompting for a password there is
		// the expected, secure flow.
		installDriver() {
			this.installError = "";
			const vendorFlag = this.driverSuggestion ? ` --vendor=${this.driverSuggestion}` : "";
			try {
				this.$store.commit("OPEN_WINDOW", {
					id: "terminal-gpu-driver-" + Date.now(),
					title: this.$t("Install GPU Driver"),
					component: "TerminalPanel",
					props: { initCommand: `sudo bash ${DRIVER_SCRIPT_PATH}${vendorFlag}` },
					width: 820,
					height: 480,
				});
				// Re-check periodically while the install runs, from a short
				// backoff again instead of up to 5 minutes.
				this.failures = 1;
				this.driverChecked = false;
				this.schedule();
			} catch (e) {
				this.installError = this.$t("Could not open a terminal - see system logs.");
			}
		},

		showMoreInfo() {
			this.showMore = !this.showMore;
			if (!this.unavailable) this.schedule();
		},
	},
};
</script>

<style lang="scss">
.widget {
	&.gpu {
		.arrow-btn {
			transition: transform 0.25s ease;

			&.open {
				transform: rotate(90deg);
			}
		}
	}
}
</style>
