<template>
	<div class="widget network is-relative">
		<div class="blur-background"></div>

		<div class="network widget-content">
			<!-- Header Start -->
			<div class="widget-header">
				<div class="widget-header-left">
					<div class="widget-badge is-net">
						<i class="mdi mdi-swap-vertical"></i>
					</div>
					<div class="widget-header-text">
						<span class="widget-title">{{ $t('Network') }}</span>
						<span class="widget-header-meta">
							<i class="mdi mdi-circle mr-1" style="font-size: 7px; color: #10b981; vertical-align: middle;"></i>
							{{ activeInterfaceName }}
						</span>
					</div>
				</div>
				<div class="widget-header-right">
					<div
						class="widget-icon-btn speedtest-btn"
						:class="{ 'is-active': isTesting || showingResults }"
						:title="isTesting ? $t('Testing... (Click to cancel)') : (showingResults ? $t('Back to Live Traffic') : $t('Start Speedtest'))"
						@click.stop="toggleSpeedtest"
					>
						<i
							class="mdi"
							:class="{
								'mdi-loading mdi-spin': isTesting,
								'mdi-speedometer': !isTesting
							}"
						></i>
					</div>

					<b-dropdown
						v-if="initNetwork.length > 0"
						v-model="networkId"
						:mobile-modal="false"
						animation="fade1"
						aria-role="list"
						class="network-dropdown"
						position="is-bottom-left"
					>
						<template #trigger="{ active }">
							<button type="button" class="net-interface-pill" :class="{ 'is-active': active }">
								<span>{{ activeInterfaceName }}</span>
								<i class="mdi" :class="active ? 'mdi-chevron-up' : 'mdi-chevron-down'"></i>
							</button>
						</template>
						<b-dropdown-item
							v-for="(item, index) in initNetwork"
							:key="'net' + index"
							:value="index"
							aria-role="listitem"
						>
							{{ item.name }}
						</b-dropdown-item>
					</b-dropdown>
				</div>
			</div>
			<!-- Header End -->

			<!-- Speed Metrics Bento Grid -->
			<div class="network-stats-grid">
				<div
					class="net-stat-card"
					:class="{
						'is-active-test is-down-active': isTesting && testPhase === 'download',
						'is-test-result': showingResults && !isTesting
					}"
				>
					<div class="net-stat-header">
						<div class="net-stat-icon is-down">
							<i class="mdi mdi-arrow-down-bold"></i>
						</div>
						<span class="net-stat-label">{{ downCardLabel }}</span>
					</div>
					<div class="net-stat-value">
						{{ displayDownSpeed }}<span class="unit">{{ displayDownUnit }}</span>
					</div>
				</div>

				<div
					class="net-stat-card"
					:class="{
						'is-active-test is-up-active': isTesting && testPhase === 'upload',
						'is-test-result': showingResults && !isTesting
					}"
				>
					<div class="net-stat-header">
						<div class="net-stat-icon is-up">
							<i class="mdi mdi-arrow-up-bold"></i>
						</div>
						<span class="net-stat-label">{{ upCardLabel }}</span>
					</div>
					<div class="net-stat-value">
						{{ displayUpSpeed }}<span class="unit">{{ displayUpUnit }}</span>
					</div>
				</div>
			</div>

			<!-- Bottom Area: Live Sparkline or Inline Speedtest Dashboard -->
			<div class="net-sparkline-box" :class="{ 'is-speedtest-mode': isTesting || showingResults }">
				<div v-if="isTesting || showingResults" class="net-speedtest-inline">
					<!-- Top Row: Ping & Target Mode -->
					<div class="net-speedtest-meta-row">
						<div class="speedtest-meta-item">
							<i class="mdi mdi-access-point ping-icon"></i>
							<span class="ping-lbl">{{ $t('Ping') }}:</span>
							<span class="ping-val">{{ testResults.ping !== null ? testResults.ping + ' ms' : '--' }}</span>
						</div>
						<div
							class="speedtest-mode-badge"
							:title="testMode === 'device' ? $t('Device ↔ NivaroOS WebUI (LAN)') : $t('Server ↔ Internet (WAN)')"
						>
							<i class="mdi" :class="testMode === 'device' ? 'mdi-cellphone-link' : 'mdi-web'"></i>
							<span>{{ testMode === 'device' ? 'LAN' : 'WAN' }}</span>
						</div>
					</div>

					<!-- Middle Row: Status & Quick Actions -->
					<div class="net-speedtest-ctrl-row">
						<div class="speedtest-status-line">
							<span v-if="isTesting" class="test-pulse-dot"></span>
							<i v-else-if="testPhase === 'done'" class="mdi mdi-check-circle done-icon"></i>
							<i v-else-if="testPhase === 'error'" class="mdi mdi-alert-circle error-icon"></i>
							<span class="status-msg">{{ phaseStatusText }}</span>
						</div>

						<div class="speedtest-actions">
							<button
								v-if="!isTesting"
								type="button"
								class="speedtest-mini-btn"
								:title="$t('Run speedtest again')"
								@click.stop="startInlineSpeedtest"
							>
								<i class="mdi mdi-refresh"></i>
							</button>
							<button
								type="button"
								class="speedtest-mini-btn"
								:title="$t('Back to live traffic chart')"
								@click.stop="exitSpeedtest"
							>
								<i class="mdi mdi-chart-bell-curve-cumulative"></i>
							</button>
						</div>
					</div>

					<!-- Bottom Progress Indicator -->
					<div v-if="isTesting" class="speedtest-progress-bar">
						<div class="speedtest-progress-fill" :style="{ width: testProgressPercent + '%' }"></div>
					</div>
				</div>

				<!-- Default Smooth Traffic Sparkline -->
				<vue-apex-charts
					v-else
					ref="chart"
					:options="chartOptions"
					:series="chartSeries"
					height="54"
					type="area"
				/>
			</div>
		</div>
	</div>
</template>

<script>
import { mixin } from '@/mixins/mixin';

export default {
	mixins: [mixin],
	// eslint-disable-next-line vue/multi-word-component-names
	name: 'network',
	icon: "network-outline",
	title: "Network Status",
	gridCols: 3,
	gridRows: 2,
	initShow: true,
	components: {
		VueApexCharts: () => import("vue-apexcharts")
	},
	data() {
		return {
			isTesting: false,
			showingResults: false,
			testPhase: 'idle', // 'idle' | 'ping' | 'download' | 'upload' | 'server_running' | 'done' | 'error'
			testMode: 'device', // 'device' | 'server'
			testResults: {
				ping: null,
				download: null,
				upload: null
			},
			liveSpeedMbps: 0,
			testError: null,
			abortController: null,
			initNetwork: [],
			networkId: 0,
			networks: [],
			currentUpSpeed: 0,
			currentDownSpeed: 0,
			chartOptions: {
				chart: {
					type: 'area',
					height: 54,
					sparkline: {
						enabled: true
					},
					animations: {
						enabled: false
					},
					toolbar: {
						show: false
					}
				},
				colors: ['#2563eb', '#10b981'],
				fill: {
					type: 'gradient',
					gradient: {
						shadeIntensity: 1,
						opacityFrom: 0.4,
						opacityTo: 0.05,
						stops: [0, 90, 100]
					}
				},
				stroke: {
					curve: 'smooth',
					width: 1.5
				},
				markers: {
					size: 0
				},
				tooltip: {
					enabled: false
				},
				grid: {
					show: false,
					padding: {
						top: 2,
						bottom: 2,
						left: 0,
						right: 0
					}
				}
			}
		};
	},
	computed: {
		activeInterfaceName() {
			if (!this.initNetwork || !this.initNetwork[this.networkId]) return 'Interface';
			return this.initNetwork[this.networkId].name || 'Interface';
		},
		chartSeries() {
			if (!this.networks || !this.networks[this.networkId]) {
				return [
					{ name: 'Down', data: [0, 0, 0, 0, 0] },
					{ name: 'Up', data: [0, 0, 0, 0, 0] }
				];
			}
			return this.networks[this.networkId];
		},
		displayDownSpeed() {
			if (this.isTesting) {
				if (this.testPhase === 'download') {
					return this.liveSpeedMbps.toFixed(1);
				}
				if (this.testPhase === 'upload' || this.testPhase === 'done') {
					return this.testResults.download !== null ? this.testResults.download.toFixed(1) : '--';
				}
				return '--';
			}
			if (this.showingResults && this.testResults.download !== null) {
				return this.testResults.download.toFixed(1);
			}
			return this.formatSpeed(this.currentDownSpeed);
		},
		displayDownUnit() {
			if (this.isTesting || this.showingResults) {
				return 'Mbps';
			}
			return this.speedUnit(this.currentDownSpeed);
		},
		displayUpSpeed() {
			if (this.isTesting) {
				if (this.testPhase === 'upload') {
					return this.liveSpeedMbps.toFixed(1);
				}
				if (this.testPhase === 'done') {
					return this.testResults.upload !== null ? this.testResults.upload.toFixed(1) : '--';
				}
				return '--';
			}
			if (this.showingResults && this.testResults.upload !== null) {
				return this.testResults.upload.toFixed(1);
			}
			return this.formatSpeed(this.currentUpSpeed);
		},
		displayUpUnit() {
			if (this.isTesting || this.showingResults) {
				return 'Mbps';
			}
			return this.speedUnit(this.currentUpSpeed);
		},
		downCardLabel() {
			if (this.isTesting && this.testPhase === 'download') return this.$t('Testing...');
			if (this.showingResults || (this.isTesting && (this.testPhase === 'upload' || this.testPhase === 'done'))) return this.$t('Test Down');
			return this.$t('Download');
		},
		upCardLabel() {
			if (this.isTesting && this.testPhase === 'upload') return this.$t('Testing...');
			if (this.showingResults || (this.isTesting && this.testPhase === 'done')) return this.$t('Test Up');
			return this.$t('Upload');
		},
		testProgressPercent() {
			switch (this.testPhase) {
				case 'ping': return 20;
				case 'download': return 60;
				case 'upload': return 88;
				case 'server_running': return 65;
				case 'done': return 100;
				default: return 0;
			}
		},
		phaseStatusText() {
			switch (this.testPhase) {
				case 'ping': return this.$t('Measuring Latency...');
				case 'download': return this.$t('Testing Download...');
				case 'upload': return this.$t('Testing Upload...');
				case 'server_running': return this.$t('Running Server WAN Test...');
				case 'done': return this.$t('Speedtest Complete');
				case 'error': return this.testError || this.$t('Test Failed');
				default: return this.$t('Ready');
			}
		}
	},
	created() {
		this.initNetwork = this.$store.state.hardwareInfo.net || [];
		if (localStorage.getItem('networkId')) {
			this.networkId = parseInt(localStorage.getItem('networkId'), 10) || 0;
		}
	},
	watch: {
		networkId(val, oldVal) {
			if (val !== oldVal) {
				localStorage.setItem('networkId', val);
			}
		}
	},
	beforeDestroy() {
		if (this.abortController) {
			this.abortController.abort();
		}
	},
	methods: {
		formatSpeed(kb) {
			const bytes = (parseFloat(kb) || 0) * 1024;
			if (bytes <= 0) return '0';
			if (bytes < 1024) return bytes.toFixed(0);
			const k = 1024;
			const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
			const i = Math.floor(Math.log(bytes) / Math.log(k));
			const val = parseFloat((bytes / Math.pow(k, i)).toFixed(1));
			return isNaN(val) ? '0' : val;
		},
		speedUnit(kb) {
			const bytes = (parseFloat(kb) || 0) * 1024;
			if (bytes < 1024) return 'B/s';
			const k = 1024;
			const sizes = ['B/s', 'KB/s', 'MB/s', 'GB/s', 'TB/s'];
			const i = Math.floor(Math.log(bytes) / Math.log(k));
			return sizes[i] || 'KB/s';
		},
		buildDatas(data) {
			if (!data || data.length === 0) return;
			data.forEach((el, index) => {
				if (this.networks[index] === undefined) {
					this.networks[index] = [
						{
							name: 'Down',
							data: [0],
							cacheData: 0,
							cacheTime: 0
						},
						{
							name: 'Up',
							data: [0],
							cacheData: 0,
							cacheTime: 0
						}
					];
				}

				// Recv Data (Down)
				if (this.networks[index][0].data.length >= 40) {
					this.networks[index][0].data.shift();
				}
				if (this.networks[index][0].cacheData > 0) {
					const timeGap = this.networks[index][0].cacheTime === 0 ? 2 : el.time - this.networks[index][0].cacheTime;
					this.networks[index][0].data.push(this.covertToKB((el.bytesRecv - this.networks[index][0].cacheData) / timeGap));
				}
				this.networks[index][0].cacheData = el.bytesRecv;
				this.networks[index][0].cacheTime = el.time;

				// Send Data (Up)
				if (this.networks[index][1].data.length >= 40) {
					this.networks[index][1].data.shift();
				}
				if (this.networks[index][1].cacheData > 0) {
					const timeGap = this.networks[index][1].cacheTime === 0 ? 2 : el.time - this.networks[index][1].cacheTime;
					this.networks[index][1].data.push(this.covertToKB((el.bytesSent - this.networks[index][1].cacheData) / timeGap));
				}
				this.networks[index][1].cacheData = el.bytesSent;
				this.networks[index][1].cacheTime = el.time;
			});

			this.networkId = this.networkId > this.networks.length - 1 ? 0 : this.networkId;
			if (!this.isTesting && !this.showingResults) {
				this.$refs.chart?.updateSeries(this.networks[this.networkId]);
			}
			if (this.networks && this.networks[this.networkId]) {
				const downSpeed = this.networks[this.networkId][0].data[this.networks[this.networkId][0].data.length - 1];
				const upSpeed = this.networks[this.networkId][1].data[this.networks[this.networkId][1].data.length - 1];
				this.currentDownSpeed = isNaN(downSpeed) ? 0 : downSpeed;
				this.currentUpSpeed = isNaN(upSpeed) ? 0 : upSpeed;
			}
		},
		covertToKB(bytes) {
			const kb = (bytes / 1024).toFixed(0);
			return kb > 0 ? parseFloat(kb) : 0;
		},

		// Speedtest Control
		toggleSpeedtest() {
			if (this.isTesting) {
				this.cancelSpeedtest();
			} else if (this.showingResults) {
				this.exitSpeedtest();
			} else {
				this.startInlineSpeedtest();
			}
		},
		exitSpeedtest() {
			this.isTesting = false;
			this.showingResults = false;
			this.testPhase = 'idle';
			this.$nextTick(() => {
				if (this.networks && this.networks[this.networkId]) {
					this.$refs.chart?.updateSeries(this.networks[this.networkId]);
				}
			});
		},
		cancelSpeedtest() {
			if (this.abortController) {
				this.abortController.abort();
			}
			this.isTesting = false;
			this.showingResults = false;
			this.testPhase = 'idle';
			this.$nextTick(() => {
				if (this.networks && this.networks[this.networkId]) {
					this.$refs.chart?.updateSeries(this.networks[this.networkId]);
				}
			});
		},

		async startInlineSpeedtest() {
			if (this.isTesting) return;
			this.isTesting = true;
			this.showingResults = true;
			this.testPhase = 'ping';
			this.testMode = 'device';
			this.testError = null;
			this.liveSpeedMbps = 0;
			this.testResults = { ping: null, download: null, upload: null };
			this.abortController = new AbortController();

			const gatewayOrigin = `${window.location.protocol}//${window.location.host}`;

			// 1. Latency Measurement (Device <-> WebUI Gateway)
			const pings = [];
			for (let i = 0; i < 4; i++) {
				if (!this.isTesting) return;
				const t0 = performance.now();
				try {
					const res = await fetch(`${gatewayOrigin}/speedtest/ping?_t=${Date.now()}_${i}`, {
						cache: 'no-store',
						signal: this.abortController.signal
					});
					if (!res.ok) throw new Error('status ' + res.status);
					pings.push(performance.now() - t0);
				} catch (err) {
					if (err.name === 'AbortError') return;
				}
			}

			if (!this.isTesting) return;

			// If device LAN ping failed, seamlessly fallback to server internet speedtest!
			if (pings.length === 0) {
				await this.runServerSpeedtest();
				return;
			}

			const avgPing = pings.reduce((a, b) => a + b, 0) / pings.length;
			this.testResults.ping = Math.round(avgPing * 10) / 10;

			// 2. Download Test (Device <- WebUI Gateway)
			this.testPhase = 'download';
			this.liveSpeedMbps = 0;
			const dlStart = performance.now();
			let dlBytes = 0;
			try {
				const resp = await fetch(`${gatewayOrigin}/speedtest/download?_t=${Date.now()}`, {
					cache: 'no-store',
					signal: this.abortController.signal
				});
				if (!resp.ok || !resp.body) throw new Error('download error');
				const reader = resp.body.getReader();
				const maxDurationMs = 3500;

				while (true) {
					if (!this.isTesting) {
						reader.cancel();
						return;
					}
					const { done, value } = await reader.read();
					if (done) break;
					dlBytes += value.length;
					const elapsedSec = (performance.now() - dlStart) / 1000;
					if (elapsedSec > 0 && this.isTesting) {
						this.liveSpeedMbps = Math.round(((dlBytes * 8) / (elapsedSec * 1000000)) * 10) / 10;
					}
					if (performance.now() - dlStart >= maxDurationMs) {
						reader.cancel();
						break;
					}
				}
				const finalDlSec = (performance.now() - dlStart) / 1000;
				this.testResults.download = Math.round(((dlBytes * 8) / (finalDlSec * 1000000)) * 10) / 10;
			} catch (err) {
				if (err.name === 'AbortError' || !this.isTesting) return;
				// Fallback to server internet speedtest
				await this.runServerSpeedtest();
				return;
			}

			if (!this.isTesting) return;

			// 3. Upload Test (Device -> WebUI Gateway)
			this.testPhase = 'upload';
			this.liveSpeedMbps = 0;
			const upStart = performance.now();
			let upBytes = 0;
			try {
				const chunkSize = 512 * 1024;
				const chunk = new Uint8Array(chunkSize);
				for (let i = 0; i < chunkSize; i++) chunk[i] = i & 0xff;
				const maxDurationMs = 3000;

				while (performance.now() - upStart < maxDurationMs) {
					if (!this.isTesting) return;
					await fetch(`${gatewayOrigin}/speedtest/upload?_t=${Date.now()}`, {
						method: 'POST',
						headers: { 'Content-Type': 'application/octet-stream' },
						body: chunk,
						signal: this.abortController.signal
					});
					upBytes += chunkSize;
					const elapsedSec = (performance.now() - upStart) / 1000;
					if (elapsedSec > 0 && this.isTesting) {
						this.liveSpeedMbps = Math.round(((upBytes * 8) / (elapsedSec * 1000000)) * 10) / 10;
					}
				}
				const finalUpSec = (performance.now() - upStart) / 1000;
				this.testResults.upload = Math.round(((upBytes * 8) / (finalUpSec * 1000000)) * 10) / 10;
			} catch (err) {
				if (err.name === 'AbortError' || !this.isTesting) return;
			}

			if (!this.isTesting) return;
			this.testPhase = 'done';
			this.isTesting = false;
		},

		async runServerSpeedtest() {
			this.testMode = 'server';
			this.testPhase = 'server_running';
			this.liveSpeedMbps = 0;
			try {
				const res = await this.$api.sys.getSpeedtest();
				if (!this.isTesting) return;
				if (res.data && res.data.success === 200 && res.data.data) {
					const d = res.data.data;
					this.testResults.ping = d.ping_ms;
					this.testResults.download = d.download_mbps;
					this.testResults.upload = d.upload_mbps;
					this.testPhase = 'done';
				} else {
					throw new Error('invalid server speedtest data');
				}
			} catch (err) {
				if (!this.isTesting) return;
				this.testError = err.message || 'Speedtest failed';
				this.testPhase = 'error';
			} finally {
				this.isTesting = false;
			}
		}
	},
	sockets: {
		"nivaroos:system:utilization"(res) {
			let data = res.Properties;
			this.initNetwork = JSON.parse(data.sys_net);
			this.buildDatas(this.initNetwork);
		}
	}
};
</script>

<style lang="scss">
.speedtest-btn {
	color: var(--theme-desktop-glass-icon, #475569);
	font-size: 0.95rem;

	&.is-active {
		color: #2563eb !important;
		background: rgba(37, 99, 235, 0.12) !important;
	}

	&:hover {
		color: #2563eb !important;
		background: rgba(37, 99, 235, 0.1) !important;
	}
}

.net-interface-pill {
	display: inline-flex;
	align-items: center;
	gap: 4px;
	padding: 2px 7px;
	border-radius: 6px;
	background: var(--theme-card-subtle, rgba(0, 0, 0, 0.03));
	border: 1px solid var(--theme-desktop-glass-border, rgba(0, 0, 0, 0.08));
	color: var(--theme-desktop-glass-text, #0f172a);
	font-size: 0.72rem;
	font-weight: 600;
	cursor: pointer;
	transition: all 0.15s ease;

	&:hover,
	&.is-active {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.08));
		color: var(--theme-desktop-glass-text, #0f172a);
	}

	.mdi {
		font-size: 0.9rem;
		color: var(--theme-desktop-glass-text-sub, #475569);
	}
}

.network-dropdown {
	.dropdown-menu {
		min-width: 6.5rem;

		.dropdown-content {
			max-width: 8.5rem;
			border-radius: var(--radius-card, 10px);
			padding: 0.35rem !important;
			background: var(--theme-dropdown-bg, #ffffff);
			backdrop-filter: blur(16px);
			-webkit-backdrop-filter: blur(16px);
			box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.15), 0 0 0 1px var(--theme-desktop-glass-border, rgba(0, 0, 0, 0.08));

			.dropdown-item {
				padding: 0.35rem 0.6rem;
				border-radius: var(--radius-sm, 6px);
				transition: all 0.15s ease;
				overflow: hidden;
				white-space: nowrap;
				text-overflow: ellipsis;
				color: var(--theme-desktop-glass-text, #0f172a);
				font-size: 0.75rem;
				font-weight: 500;
				margin-bottom: 2px;

				&:hover {
					background: var(--theme-card-hover, rgba(0, 0, 0, 0.06)) !important;
					color: var(--theme-desktop-glass-text, #0f172a);
				}

				&.is-active {
					background: rgba(37, 99, 235, 0.1) !important;
					color: #2563eb !important;
					font-weight: 600;
				}
			}
		}
	}
}

/* ── Inline Speedtest Styling ─────────────────────────────────────── */
.net-stat-card {
	transition: all 0.25s cubic-bezier(0.4, 0, 0.2, 1);

	&.is-active-test {
		transform: translateY(-1px);

		&.is-down-active {
			border-color: rgba(37, 99, 235, 0.5) !important;
			box-shadow: 0 0 10px rgba(37, 99, 235, 0.2), inset 0 0 8px rgba(37, 99, 235, 0.08);
			background: rgba(37, 99, 235, 0.06) !important;
		}

		&.is-up-active {
			border-color: rgba(16, 185, 129, 0.5) !important;
			box-shadow: 0 0 10px rgba(16, 185, 129, 0.2), inset 0 0 8px rgba(16, 185, 129, 0.08);
			background: rgba(16, 185, 129, 0.06) !important;
		}
	}

	&.is-test-result {
		border-color: var(--theme-desktop-glass-border, rgba(0, 0, 0, 0.1));
		background: var(--theme-card-subtle, rgba(0, 0, 0, 0.04));
	}
}

.net-sparkline-box {
	&.is-speedtest-mode {
		background: var(--theme-card-subtle, rgba(0, 0, 0, 0.03));
		border: 1px solid var(--theme-desktop-glass-border, rgba(0, 0, 0, 0.07));
		border-radius: 8px;
		display: flex;
		flex-direction: column;
		justify-content: space-between;
		padding: 5px 8px 4px;
	}
}

.net-speedtest-inline {
	display: flex;
	flex-direction: column;
	justify-content: space-between;
	height: 100%;
	width: 100%;
	position: relative;
}

.net-speedtest-meta-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	line-height: 1;

	.speedtest-meta-item {
		display: flex;
		align-items: center;
		gap: 4px;
		font-size: 0.68rem;
		font-weight: 600;
		color: var(--theme-desktop-glass-text, #0f172a);

		.ping-icon {
			font-size: 0.75rem;
			color: #3b82f6;
		}

		.ping-lbl {
			font-size: 0.62rem;
			font-weight: 500;
			color: var(--theme-desktop-glass-text-sub, #64748b);
		}

		.ping-val {
			font-size: 0.68rem;
			font-weight: 700;
			color: var(--theme-desktop-glass-text, #0f172a);
		}
	}

	.speedtest-mode-badge {
		display: inline-flex;
		align-items: center;
		gap: 3px;
		padding: 1px 6px;
		border-radius: 4px;
		background: rgba(37, 99, 235, 0.1);
		color: #2563eb;
		font-size: 0.6rem;
		font-weight: 700;
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}
}

.net-speedtest-ctrl-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 6px;

	.speedtest-status-line {
		display: flex;
		align-items: center;
		gap: 4px;
		font-size: 0.64rem;
		font-weight: 500;
		color: var(--theme-desktop-glass-text-sub, #64748b);
		overflow: hidden;
		white-space: nowrap;
		text-overflow: ellipsis;
		max-width: 180px;

		.test-pulse-dot {
			width: 6px;
			height: 6px;
			border-radius: 50%;
			background: #3b82f6;
			animation: testPulse 1.2s infinite ease-in-out;
			flex-shrink: 0;
		}

		.done-icon {
			color: #10b981;
			font-size: 0.78rem;
			flex-shrink: 0;
		}

		.error-icon {
			color: #ef4444;
			font-size: 0.78rem;
			flex-shrink: 0;
		}
	}

	.speedtest-actions {
		display: flex;
		align-items: center;
		gap: 4px;
		flex-shrink: 0;

		.speedtest-mini-btn {
			width: 20px;
			height: 20px;
			border-radius: 4px;
			border: 1px solid var(--theme-desktop-glass-border, rgba(0, 0, 0, 0.08));
			background: var(--theme-card-subtle, rgba(0, 0, 0, 0.04));
			color: var(--theme-desktop-glass-icon, #475569);
			display: inline-flex;
			align-items: center;
			justify-content: center;
			cursor: pointer;
			font-size: 0.78rem;
			padding: 0;
			transition: all 0.15s ease;

			&:hover {
				background: rgba(37, 99, 235, 0.12);
				color: #2563eb;
				border-color: rgba(37, 99, 235, 0.3);
			}
		}
	}
}

.speedtest-progress-bar {
	width: 100%;
	height: 2px;
	background: rgba(0, 0, 0, 0.06);
	border-radius: 1px;
	overflow: hidden;
	margin-top: 1px;

	.speedtest-progress-fill {
		height: 100%;
		background: linear-gradient(90deg, #2563eb, #10b981);
		transition: width 0.3s ease;
	}
}

@keyframes testPulse {
	0%, 100% {
		transform: scale(0.85);
		opacity: 0.6;
	}
	50% {
		transform: scale(1.3);
		opacity: 1;
	}
}
</style>