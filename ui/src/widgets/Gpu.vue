<template>
	<div class="widget has-text-grey-100 gpu">
		<div class="blur-background"></div>
		<div class="widget-content pb-1">
			<!-- Header Start -->
			<div class="widget-header is-flex">
				<div class="widget-title is-flex-grow-1">
					{{ $t("GPU Status") }}
				</div>
				<div class="widget-icon-button is-flex-shrink-0" @click="showMoreInfo">
					<b-icon :class="{ open: showMore }" class="arrow-btn" icon="right-outline" pack="casa"></b-icon>
				</div>
			</div>
			<!-- Header End -->

			<div v-if="!unavailable && gpuName" class="is-size-7 has-text-grey-400 one-line mb-2">
				{{ gpuName }} · {{ $t("Driver") }} {{ driverVersion }}
			</div>

			<div v-if="unavailable" class="has-text-centered is-size-7 py-4">
				{{ $t("GPU sidecar unavailable") }}
			</div>
			<div v-else class="columns is-mobile mt-0 mb-1">
				<div class="column is-half has-text-centered">
					<radial-bar :extendContent="powerAndTemperature" :extendContentClickable="true"
						:percent="parseInt(utilizationPercent)" label="GPU" @extendContentClick="showMoreInfo"></radial-bar>
				</div>
				<div class="column is-half has-text-centered">
					<radial-bar :extendContent="renderSize(memoryTotalBytes)" :percent="parseInt(memoryPercent)"
						label="VRAM"></radial-bar>
				</div>
			</div>
			<div v-if="showMore && !unavailable">
				<div class="more-info pt-1 pb-1">
					<div v-if="processes.length === 0" class="is-size-7 has-text-centered py-2">
						{{ $t("No processes using the GPU") }}
					</div>
					<div v-for="(item, index) in processes" :key="item.pid + '-' + index"
						class="is-flex is-size-7 is-align-items-center mb-2">
						<div class="is-flex-grow-1 is-flex is-align-items-center is-clipped">
							<span class="one-line">{{ item.command }} ({{ item.pid }})</span>
						</div>
						<div class="is-flex-shrink-0">{{ item.usage }}%</div>
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

const SIDECAR_URL = `http://${window.location.hostname}:28640/gpu-stats`;
const POLL_INTERVAL_MS = 2000;

export default {
	// eslint-disable-next-line vue/multi-word-component-names
	name: "gpu",
	icon: "system-outline",
	title: "GPU Status",
	gridCols: 3, // "normal" size - 3 icon-columns wide (see SideBar.vue)
	gridRows: 2, // "normal" size - 2 icon-rows tall
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
		powerAndTemperature() {
			return `${this.powerDraw.toFixed(0)}W / ${this.temperature.toFixed(0)}°C`;
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
			transition: all 0.3s;

			&.open {
				transform: rotate(90deg);
			}
		}

		.more-info {
			border-top: 1px solid rgba(255, 255, 255, 0.1);
		}
	}
}
</style>
