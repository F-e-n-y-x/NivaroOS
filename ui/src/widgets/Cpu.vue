<template>
	<div class="widget has-text-grey-100 cpu">
		<div class="blur-background"></div>
		<div class="widget-content pb-1">
			<!-- Header Start -->
			<div class="widget-header is-flex">
				<div class="widget-title is-flex-grow-1">
					{{ cpuVendor ? cpuVendor + " " : "" }}{{ $t("CPU Status") }}
				</div>
				<div v-if="percpu.length > 1" :class="{ active: showCores }" :title="$t('Toggle per-core usage')"
					class="widget-icon-button cores-toggle is-flex-shrink-0 mr-1" @click="toggleCores">
					<span class="cores-toggle-dot"></span>
					<span class="cores-toggle-dot"></span>
					<span class="cores-toggle-dot"></span>
					<span class="cores-toggle-dot"></span>
				</div>
				<div class="widget-icon-button is-flex-shrink-0" @click="showMoreInfo">
					<b-icon :class="{ open: showMore }" class="arrow-btn" icon="right-outline" pack="casa"></b-icon>
				</div>
			</div>
			<!-- Header End -->

			<div class="columns is-mobile mt-0 mb-1">
				<div class="column is-half has-text-centered">
					<radial-bar :extendContent="power + temperature" :extendContentClickable="true"
						:percent="parseInt(cpuSeries)" label="CPU" @extendContentClick="changeFormat"></radial-bar>
				</div>
				<div v-if="cpuModelShort" class="column is-half cpu-info is-flex is-flex-direction-column is-justify-content-center has-text-centered">
					<div class="is-size-7 model-wrap">{{ cpuModelShort }}</div>
					<div class="is-size-7 has-text-grey-400 mt-1">{{ cpuCores }}C / {{ percpu.length }}T</div>
					<div v-if="mhz" class="is-size-7 has-text-grey-400">{{ (mhz / 1000).toFixed(1) }} GHz</div>
				</div>
			</div>

			<div v-if="percpu.length > 1 && showCores" class="cores-grid mb-2 px-2">
				<div v-for="(p, index) in percpu" :key="'core-' + index" :title="`${$t('Core')} ${index}: ${Math.round(p)}%`"
					class="core-row">
					<div class="core-track">
						<div :style="{ width: p + '%' }" class="core-fill"></div>
					</div>
					<div class="core-pct">{{ Math.round(p) }}%</div>
				</div>
			</div>

			<div v-if="showMore">
				<div class="more-info pt-1 pb-1">
					<div v-for="(item, index) in containerCpuList" :key="item.title + index + '-cpu'">
						<div v-if="!isNaN(item.usage)" class="is-flex is-size-7 is-align-items-center mb-2">
							<div class="is-flex-grow-1 is-flex is-align-items-center is-clipped">
								<b-image :lazy="false" :src="item.icon"
									:src-fallback="require('@/assets/img/app/default.svg')"
									class="is-16x16 mr-2 is-flex-shrink-0"></b-image>
								<span class="one-line">{{ item.title }}</span>
							</div>
							<div class="is-flex-shrink-0">{{ item.usage }}%</div>
						</div>
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
import RadialBar from "@/components/widgets/RadialBar.vue";

export default {
	// eslint-disable-next-line vue/multi-word-component-names
	name: "cpu",
	icon: "system-outline",
	title: "CPU Status",
	gridCols: 3, // "normal" size - 3 icon-columns wide (see SideBar.vue)
	gridRows: 2, // "normal" size - 2 icon-rows tall
	initShow: true,
	mixins: [smoothReflow, mixin],
	components: {
		RadialBar,
	},

	data() {
		return {
			timmer: null,
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
			power: "0W / ",
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
		// Splits the raw /proc/cpuinfo model string into a vendor line and a
		// short model line, stripping vendor-specific noise (trademark
		// symbols, "CPU @ x.xxGHz", "N-Core Processor") so it reads the same
		// shape on Intel/AMD/ARM instead of just dumping the raw string.
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
		/**
		 * @description: Split a raw /proc/cpuinfo model string (which varies
		 * wildly by vendor) into a short vendor line and a short model line,
		 * so the widget shows the same shape on any PC instead of dumping
		 * the raw string.
		 * @param {string} raw
		 * @return {{vendor: string, model: string}}
		 */
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

		/**
		 * @description: Convert temperature from Celsius to Fahrenheit
		 * @param {*}
		 * @return {fahrenheit} Number
		 */
		celsiusToFahrenheit(celsius) {
			let fahrenheit = (celsius * 9) / 5 + 32;
			return fahrenheit;
		},

		changeFormat() {
			this.temperatureFormat = this.temperatureFormat == "°C" ? "°F" : "°C";
			localStorage.setItem("temperatureFormat", this.temperatureFormat);
		},
		/**
		 * @description: Update cpu usage
		 * @param {*}
		 * @return {*} void
		 */
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
					).toFixed(1) + "W / ";
			} else {
				this.power = "";
			}
		},
		/**
		 * @description: Get Docker apps cpu usage
		 * @param {*}
		 * @return {*} void
		 */
		getDockerUsage() {
			this.$api.container.getHardwareUsage().then((res) => {
				let id = 0;
				this.containerCpuList = res.data.data.map((item) => {
					let usage = 0;
					if (item.previous != null) {
						// Look at here  https://docs.docker.com/engine/api/v1.41/#operation/ContainerStats
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

		/**
		 * @description: Toggle more info
		 * @param {*}
		 * @return {*} void
		 */
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
		"casaos:system:utilization"(res) {
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
			transition: all 0.3s;

			&.open {
				transform: rotate(90deg);
			}
		}

		.cores-toggle {
			display: grid;
			grid-template-columns: repeat(2, 5px);
			grid-template-rows: repeat(2, 5px);
			gap: 2px;
			align-content: center;
			justify-content: center;

			.cores-toggle-dot {
				width: 5px;
				height: 5px;
				border-radius: 1px;
				background: rgba(255, 255, 255, 0.35);
				transition: background-color 0.2s;
			}

			&.active .cores-toggle-dot {
				background: rgba(255, 255, 255, 0.9);
			}
		}

		.more-info {
			border-top: 1px solid rgba(255, 255, 255, 0.1);
		}

		.cpu-info {
			min-width: 0;

			.model-wrap {
				overflow-wrap: break-word;
				line-height: 1.25;
			}
		}

		.cores-grid {
			display: grid;
			grid-template-columns: repeat(2, 1fr);
			gap: 0.375rem 1.5rem;

			.core-row {
				display: flex;
				align-items: center;
				gap: 0.05rem;
			}

			.core-track {
				flex-grow: 1;
				height: 5px;
				background: rgba(255, 255, 255, 0.12);
				border-radius: 3px;
				overflow: hidden;
			}

			.core-fill {
				height: 100%;
				background: rgba(255, 255, 255, 0.75);
				border-radius: 3px;
				transition: width 0.5s ease-in-out;
			}

			.core-pct {
				flex-shrink: 0;
				width: 1.6rem;
				text-align: right;
				font-size: 0.65rem;
				color: $grey-400;
			}
		}
	}
}
</style>
