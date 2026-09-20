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
					<div v-if="!unavailable" class="widget-icon-btn" :title="$t('Processes')" @click="showMoreInfo">
						<b-icon :class="{ open: showMore }" class="arrow-btn" icon="right-outline" pack="casa"></b-icon>
					</div>
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
							<span class="spec-val">{{ powerDraw.toFixed(0) }}W</span>
						</div>
					</div>
				</div>
			</div>

			<!-- Sleek Fallback when no Discrete GPU is Detected -->
			<div v-else class="gpu-unavailable-card">
				<i class="mdi mdi-monitor-dashboard gpu-unavail-icon"></i>
				<div class="gpu-unavail-title">{{ $t("Integrated Graphics") }}</div>
				<div class="gpu-unavail-desc">{{ $t("No discrete GPU telemetry sidecar detected on system.") }}</div>
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

const SIDECAR_URL = `http://${window.location.hostname}:28640/gpu-stats`;
const POLL_INTERVAL_MS = 2000;

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
		};
	},
	computed: {
		memoryPercent() {
			if (!this.memoryTotalBytes) return 0;
			return (this.memoryUsedBytes / this.memoryTotalBytes) * 100;
		},
		temperatureDisplay() {
			const format = localStorage.getItem("temperatureFormat") || "°C";
			if (format === "°F") {
				return Math.round((this.temperature * 9) / 5 + 32) + "°F";
			}
			return Math.round(this.temperature) + "°C";
		},
		powerAndTemperature() {
			return `${this.powerDraw.toFixed(0)}W · ${this.temperatureDisplay}`;
		},
	},
	created() {
		this.poll();
		this.timer = setInterval(this.poll, POLL_INTERVAL_MS);
	},
	mounted() {
		this.$smoothReflow({
			el: ".widget",
			property: ["height"],
		});
	},
	beforeDestroy() {
		clearInterval(this.timer);
	},
	methods: {
		poll() {
			fetch(SIDECAR_URL)
				.then((res) => {
					if (!res.ok) throw new Error("sidecar error");
					return res.json();
				})
				.then((data) => {
					this.unavailable = false;
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
				});
		},

		showMoreInfo() {
			this.showMore = !this.showMore;
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
