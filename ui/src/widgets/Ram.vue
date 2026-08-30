<template>
	<div class="widget has-text-grey-100 ram">
		<div class="blur-background"></div>
		<div class="widget-content pb-1">
			<!-- Header Start -->
			<div class="widget-header is-flex">
				<div class="widget-title is-flex-grow-1">
					{{ $t("RAM Status") }}
				</div>
				<div class="widget-icon-button is-flex-shrink-0" @click="showMoreInfo">
					<b-icon :class="{ open: showMore }" class="arrow-btn" icon="right-outline" pack="casa"></b-icon>
				</div>
			</div>
			<!-- Header End -->

			<div class="columns is-mobile mt-0 mb-1">
				<div class="column is-half has-text-centered">
					<radial-bar :extendContent="renderSize(usedMemory) + ' / ' + renderSize(totalMemory)"
						:extendContentClickable="true" :percent="parseInt(ramSeries)" label="RAM"
						@extendContentClick="showMoreInfo"></radial-bar>
				</div>
				<div v-if="dimmGroups.length" class="column is-half dimm-info is-flex is-flex-direction-column is-justify-content-center has-text-centered">
					<div v-for="(g, index) in dimmGroups" :key="'dimm-group-' + index" class="dimm-group">
						<div class="is-size-7">{{ g.count }} × {{ g.size }}</div>
						<div class="is-size-7 has-text-grey-400">{{ g.type }}</div>
						<div class="is-size-7 has-text-grey-400">{{ g.speed }}</div>
						<div v-if="g.partNumber" class="is-size-7 has-text-grey-400 one-line">{{ g.partNumber }}</div>
					</div>
				</div>
			</div>

			<div v-if="showMore">
				<div class="more-info pt-1 pb-1">
					<div v-for="(item, index) in containerRamList" :key="item.title + index + '-ram'">
						<div v-if="!isNaN(item.usage) && renderSize(item.usage).split(' ')[0] != 0"
							class="is-flex is-size-7 is-align-items-center mb-2">
							<div class="is-flex-grow-1 is-flex-shrink-1 is-flex is-align-items-center is-clipped">
								<b-image :src="item.icon" :src-fallback="require('@/assets/img/app/default.svg')"
									class="is-16x16 mr-2 is-flex-shrink-0"></b-image>
								<span class="one-line">{{ item.title }}</span>
							</div>
							<div class="is-flex-shrink-0">{{ item.usage | renderSize }}</div>
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
import has from "lodash/has";
import slice from "lodash/slice";
import { mixin } from "@/mixins/mixin";
import RadialBar from "@/components/widgets/RadialBar.vue";

export default {
	// eslint-disable-next-line vue/multi-word-component-names
	name: "ram",
	icon: "system-outline",
	title: "RAM Status",
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
			totalMemory: 0,
			usedMemory: 0,
			ramSeries: 0,
			containerRamList: [],
			dimms: [],
		};
	},
	computed: {
		// Groups identical DIMMs (matched kits) into one labeled
		// type/speed/slots/part block instead of repeating the same
		// size/type/speed per slot, and instead of one run-on line.
		dimmGroups() {
			const groups = [];
			for (const d of this.dimms) {
				const existing = groups.find(
					(g) => g.size === d.size && g.type === d.type && g.speed === d.speed && g.partNumber === d.part_number
				);
				if (existing) {
					existing.count++;
				} else {
					groups.push({ size: d.size, type: d.type, speed: d.speed, partNumber: d.part_number, count: 1 });
				}
			}
			return groups.map((g) => ({
				type: g.type,
				speed: g.speed,
				size: g.size,
				count: g.count,
				partNumber:
					g.partNumber && g.partNumber !== "Unknown" && g.partNumber !== "Not Specified" ? g.partNumber : "",
			}));
		},
	},
	created() {
		this.totalMemory = this.$store.state.hardwareInfo.mem.total;
		this.dimms = this.$store.state.hardwareInfo.mem.dimms || [];
		this.updateCharts(this.$store.state.hardwareInfo.mem);
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
		 * @description: Update memory usage
		 * @param {*}
		 * @return {*} void
		 */
		updateCharts(mem) {
			this.ramSeries = mem.usedPercent;
			this.usedMemory = mem.used;
		},
		/**
		 * @description: Get Docker apps memory usage
		 * @param {*}
		 * @return {*} void
		 */
		getDockerUsage() {
			this.$api.container.getHardwareUsage().then((res) => {
				this.containerRamList = res.data.data.map((item) => {
					let id = 0;
					const getCacheValue = (item) => {
						if (has(item.data.memory_stats.stats, "inactive_file")) {
							return item.data.memory_stats.stats.inactive_file;
						} else if (has(item.data.memory_stats.stats, "cache")) {
							return item.data.memory_stats.stats.cache;
						} else if (has(item.data.memory_stats.stats, "total_inactive_file")) {
							return item.data.memory_stats.stats.total_inactive_file;
						} else {
							return 0;
						}
					};
					const used_memory = "stats" in item.data.memory_stats ? item.data.memory_stats.usage - getCacheValue(item) : NaN;
					id++;
					return {
						id: id,
						usage: isNaN(used_memory) ? 0 : used_memory,
						icon: item.icon,
						title: item.title,
					};
				});
				this.containerRamList = slice(orderBy(this.containerRamList, ["usage"], ["desc"]), 0, 8);
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
				this.$messageBus("widget_ram", "open");
			} else {
				this.$messageBus("widget_ram", "close");
			}
		},
	},
	sockets: {
		"casaos:system:utilization"(res) {
			let data = res.Properties;
			let mem = JSON.parse(data.sys_mem);
			this.updateCharts(mem);
		},
	},
};
</script>

<style lang="scss">
.widget {
	&.ram {
		.arrow-btn {
			transition: all 0.3s;

			&.open {
				transform: rotate(90deg);
			}
		}

		.more-info {
			border-top: 1px solid rgba(255, 255, 255, 0.1);
		}

		.dimm-info {
			min-width: 0;
		}

		.dimm-group + .dimm-group {
			margin-top: 0.25rem;
			padding-top: 0.25rem;
			border-top: 1px solid rgba(255, 255, 255, 0.1);
		}
	}
}
</style>
