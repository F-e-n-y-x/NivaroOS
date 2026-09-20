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
							{{ cpuModelShort || (cpuVendor ? cpuVendor + ' Architecture' : 'System CPU') }}
						</span>
					</div>
				</div>
				<div class="widget-header-right">
					<div
						v-if="percpu.length > 1"
						:class="{ active: showCores }"
						:title="$t('Toggle per-core usage')"
						class="widget-icon-btn"
						@click="toggleCores"
					>
						<i class="mdi mdi-view-grid-outline"></i>
					</div>
					<div class="widget-icon-btn" :title="$t('Processes')" @click="showMoreInfo">
						<b-icon :class="{ open: showMore }" class="arrow-btn" icon="right-outline" pack="casa"></b-icon>
					</div>
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
							<span class="spec-val">{{ mhz ? (mhz / 1000).toFixed(1) + ' GHz' : 'Dynamic' }}</span>
						</div>
						<div class="spec-tile" :title="$t('Cores and Threads')">
							<span class="spec-label">{{ $t('Cores') }}</span>
							<span class="spec-val">{{ cpuCores }}C / {{ percpu.length || cpuCores }}T</span>
						</div>
						<div class="spec-tile" :title="$t('Temperature / Power') + ' (Click to switch °C/°F)'" @click="changeFormat" style="cursor: pointer;">
							<span class="spec-label">{{ $t('Thermals') }}</span>
							<span class="spec-val">{{ temperature || 'Normal' }}{{ powerClean ? ' · ' + powerClean : '' }}</span>
						</div>
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
								:src-fallback="require('@/assets/img/app-icons/default.svg')"
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
			timer: null,
			showMore: false,
			showCores: localStorage.getItem("cpuShowCores") !== "false",
			cpuCores: 0,
			modelName: "",
			mhz: 0,
			cpuSeries: 0,
			percpu: [],
			containerCpuList: [],
			temperatureFormat: localStorage.getItem("temperatureFormat")
				? localStorage.getItem("temperatureFormat")
				: "°C",
			orgTemperature: 0,
			power: "",
			powerList: [],
		};
	},
	computed: {
		temperature() {
			const temp =
				this.temperatureFormat == "°C"
					? this.orgTemperature + "°C"
					: this.celsiusToFahrenheit(this.orgTemperature) + "°F";
			return temp;
		},
		powerClean() {
			if (!this.power) return "";
			return this.power.replace(" / ", "").trim();
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
		cpuVendor() {
			return this.cpuModelParts.vendor;
		},
		cpuModelShort() {
			return this.cpuModelParts.model;
		},
	},
	created() {
		this.cpuCores = this.$store.state.hardwareInfo.cpu.num;
		this.modelName = this.$store.state.hardwareInfo.cpu.model_name || "";
		this.mhz = this.$store.state.hardwareInfo.cpu.mhz || 0;
		this.updateCharts(this.$store.state.hardwareInfo.cpu);
		this.getDockerUsage();
		this.timer = setInterval(() => {
			if (this.showMore) {
				this.getDockerUsage();
			}
		}, 1000);
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
			this.temperatureFormat = this.temperatureFormat == "°C" ? "°F" : "°C";
			localStorage.setItem("temperatureFormat", this.temperatureFormat);
		},

		updateCharts(cpu) {
			this.cpuSeries = cpu.percent;
			this.percpu = cpu.percpu || [];
			this.pushPower(cpu.power);
			this.orgTemperature = cpu.temperature == undefined ? 0 : cpu.temperature;
			if (this.powerList.length == 2 && (cpu.model === "intel" || cpu.model === "amd")) {
				this.power =
					(
						(this.powerList[1].value - this.powerList[0].value) /
						1000000 /
						(this.powerList[1].timestamp - this.powerList[0].timestamp)
					).toFixed(1) + "W";
			} else {
				this.power = "";
			}
		},

		getDockerUsage() {
			this.$api.container.getHardwareUsage().then((res) => {
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
			});
		},

		showMoreInfo() {
			this.showMore = !this.showMore;
			if (this.showMore) {
				this.$messageBus("widget_cpu", "open");
			} else {
				this.$messageBus("widget_cpu", "close");
			}
		},

		toggleCores() {
			this.showCores = !this.showCores;
			localStorage.setItem("cpuShowCores", this.showCores);
		},

		pushPower(power) {
			if (this.powerList.length >= 2) {
				this.powerList.shift();
			}
			this.powerList.push(power);
		},
	},
	sockets: {
		"nivaroos:system:utilization"(res) {
			let data = res.Properties;
			let cpu = JSON.parse(data.sys_cpu);
			this.updateCharts(cpu);
		},
	},
};
</script>

<style lang="scss">
.widget {
	&.cpu {
		.arrow-btn {
			transition: transform 0.25s ease;

			&.open {
				transform: rotate(90deg);
			}
		}
	}
}
</style>
