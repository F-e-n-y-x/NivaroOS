<template>
	<div class="widget ram">
		<div class="blur-background"></div>
		<div class="widget-content">
			<!-- Header -->
			<div class="widget-header">
				<div class="widget-header-left">
					<div class="widget-badge is-ram">
						<i class="mdi mdi-memory"></i>
					</div>
					<div class="widget-header-text">
						<span class="widget-title">{{ $t("Memory") }}</span>
						<span class="widget-header-meta">
							{{ renderSize(totalMemory) }} {{ $t("Total") }}
						</span>
					</div>
				</div>
				<div class="widget-header-right">
					<button
						type="button"
						class="widget-icon-btn"
						:title="$t('Processes')"
						:aria-label="$t('Processes')"
						:aria-expanded="showMore ? 'true' : 'false'"
						@click="showMoreInfo"
					>
						<i class="mdi mdi-chevron-right arrow-btn" :class="{ open: showMore }" aria-hidden="true"></i>
					</button>
				</div>
			</div>

			<!-- Hero Grid: Left = Dial, Right = Structured Bento Stats -->
			<div class="widget-hero-grid">
				<div class="hero-gauge-box">
					<radial-bar
						:percent="parseInt(ramSeries)"
						label="RAM"
					></radial-bar>
				</div>
				<div class="hero-info-bento">
					<div class="bento-hero-title">
						{{ renderSize(usedMemory) }} <span class="has-text-weight-normal is-size-7 text-muted">{{ $t("Used") }}</span>
					</div>
					<div class="bento-hero-sub">
						{{ renderSize(freeMemory) }} {{ $t("Available") }}
					</div>
					<div class="ram-track-box">
						<div class="ram-progress-track">
							<div class="ram-progress-fill" :style="{ width: Math.min(100, Math.max(2, parseInt(ramSeries))) + '%' }"></div>
						</div>
					</div>
					<div class="bento-chips-row">
						<template v-if="dimmGroups.length">
							<span v-for="(g, index) in dimmGroups" :key="'dimm-' + index" class="bento-chip is-purple">
								<i class="mdi mdi-memory"></i> {{ g.count }}× {{ g.size }} {{ g.type }} {{ g.speed || '' }}
							</span>
						</template>
						<template v-else>
							<span class="bento-chip is-purple">
								<i class="mdi mdi-memory"></i> {{ $t('{size} system RAM', { size: renderSize(totalMemory) }) }}
							</span>
						</template>
					</div>
				</div>
			</div>

			<!-- Top Memory Consuming Processes -->
			<div v-if="showMore" class="more-info">
				<div class="process-section-title">{{ $t("Top Processes") }}</div>
				<div v-if="containerRamList.length === 0" class="has-text-centered is-size-7 py-2 text-muted">
					{{ $t("No active container processes") }}
				</div>
				<div v-for="(item, index) in containerRamList" :key="item.title + index + '-ram'">
					<div v-if="!isNaN(item.usage) && renderSize(item.usage).split(' ')[0] != 0" class="process-row">
						<div class="is-flex is-align-items-center is-clipped">
							<b-image
								:lazy="false"
								:src="item.icon"
								:src-fallback="$assetUrl(require('@/assets/img/app-icons/default.svg'))"
								class="is-16x16 mr-2 is-flex-shrink-0"
							></b-image>
							<span class="one-line process-name">{{ item.title }}</span>
						</div>
						<div class="is-flex-shrink-0 process-usage">{{ item.usage | renderSize }}</div>
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
import RadialBar from "@/shared/widgets/RadialBar.vue";
import { subscribeContainerUsage } from "@/utils/containerUsagePoller.js";

export default {
	// eslint-disable-next-line vue/multi-word-component-names
	name: "ram",
	icon: "system-outline",
	title: "RAM Status",
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
			totalMemory: 0,
			usedMemory: 0,
			// MemAvailable from the kernel (what can be allocated without
			// swapping, page cache included) - NOT total-used.
			availableMemory: null,
			ramSeries: 0,
			containerRamList: [],
			dimms: [],
		};
	},
	computed: {
		freeMemory() {
			if (typeof this.availableMemory === "number" && this.availableMemory >= 0) return this.availableMemory;
			const diff = this.totalMemory - this.usedMemory;
			return diff > 0 ? diff : 0;
		},
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
	watch: {
		// Follow a refetched hardware snapshot (Home.vue retries a failed load).
		"$store.state.hardwareInfo.mem"(mem) {
			if (!mem) return;
			if (mem.dimms) this.dimms = mem.dimms;
			this.updateCharts(mem);
		},
	},
	created() {
		const mem = this.$store.state.hardwareInfo.mem || {};
		this.dimms = mem.dimms || [];
		this.updateCharts(mem);
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
	},
	methods: {
		updateCharts(mem) {
			if (!mem) return;
			if (mem.total) this.totalMemory = mem.total;
			this.ramSeries = mem.usedPercent || 0;
			this.usedMemory = mem.used || 0;
			this.availableMemory = typeof mem.available === "number" ? mem.available : null;
		},
		applyUsage(res) {
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
	},
	sockets: {
		"nivaroos:system:utilization"(res) {
			if (document.hidden) return;
			let mem;
			try {
				mem = JSON.parse(res.Properties.sys_mem);
			} catch (e) {
				return;
			}
			this.updateCharts(mem);
		},
	},
};
</script>

<style lang="scss">
.widget {
	&.ram {
		.arrow-btn {
			display: inline-block;
			transition: transform 0.25s ease;

			&.open {
				transform: rotate(90deg);
			}
		}
	}
}
</style>
