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
							<i
								v-if="activeInterface"
								class="mdi mdi-circle mr-1 net-state-dot"
								:class="'is-' + activeInterfaceStateClass"
								:title="activeInterfaceStateLabel"
								aria-hidden="true"
							></i>
							<span v-if="activeInterface" class="is-sr-only">{{ activeInterfaceStateLabel }}</span>
							{{ activeInterfaceName }}
						</span>
					</div>
				</div>
				<div class="widget-header-right">
					<button
						type="button"
						class="widget-icon-btn speedtest-btn"
						:class="{ 'is-active': isTesting || showingResults }"
						:title="speedtestButtonLabel"
						:aria-label="speedtestButtonLabel"
						:aria-pressed="isTesting || showingResults ? 'true' : 'false'"
						@click.stop="toggleSpeedtest"
					>
						<i
							class="mdi"
							:class="{
								'mdi-loading mdi-spin': isTesting,
								'mdi-speedometer': !isTesting
							}"
							aria-hidden="true"
						></i>
					</button>

					<b-dropdown
						v-if="initNetwork.length > 1"
						:value="networkName"
						@change="selectInterface"
						:mobile-modal="false"
						animation="fade1"
						aria-role="list"
						class="network-dropdown"
						position="is-bottom-left"
					>
						<template #trigger="{ active }">
							<button
								type="button"
								class="net-interface-pill"
								:class="{ 'is-active': active }"
								:aria-label="$t('Network interface: {name}', { name: activeInterfaceName })"
							>
								<span>{{ activeInterfaceName }}</span>
								<i class="mdi" :class="active ? 'mdi-chevron-up' : 'mdi-chevron-down'"></i>
							</button>
						</template>
						<b-dropdown-item
							v-for="item in initNetwork"
							:key="'net-' + item.name"
							:value="item.name"
							aria-role="listitem"
						>
							<i class="mdi mdi-circle mr-1 net-state-dot" :class="'is-' + stateClass(item.state)" aria-hidden="true"></i>
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
							<span v-if="testResults.jitter !== null" class="ping-lbl ml-1" :title="$t('Jitter')">±{{ testResults.jitter }}</span>
						</div>
						<div class="speedtest-mode-toggle" role="group" :aria-label="$t('Speedtest type')">
							<button
								type="button"
								:class="{ 'is-on': testMode === 'server' }"
								:disabled="isTesting"
								:title="$t('Server ↔ Internet (nearest speedtest.net server)')"
								@click.stop="setTestMode('server')"
							>
								<i class="mdi mdi-web"></i> {{ $t('Internet') }}
							</button>
							<button
								type="button"
								:class="{ 'is-on': testMode === 'device' }"
								:disabled="isTesting"
								:title="$t('This device ↔ NivaroOS (LAN)')"
								@click.stop="setTestMode('device')"
							>
								<i class="mdi mdi-lan"></i> {{ $t('LAN') }}
							</button>
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

						<div v-if="testServer" class="speedtest-server" :title="testServer">{{ testServer }}</div>
						<div class="speedtest-actions">
							<button
								v-if="!isTesting"
								type="button"
								class="speedtest-mini-btn"
								:title="$t('Run speedtest again')"
								:aria-label="$t('Run speedtest again')"
								@click.stop="startInlineSpeedtest"
							>
								<i class="mdi mdi-refresh"></i>
							</button>
							<button
								type="button"
								class="speedtest-mini-btn"
								:title="$t('Back to live traffic chart')"
								:aria-label="$t('Back to live traffic chart')"
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

				<!-- Empty state: no interface reported at all (e.g. a container
				     host whose only NIC the backend could not classify). -->
				<div v-else-if="!initNetwork.length" class="net-empty-state">
					<i class="mdi mdi-lan-disconnect" aria-hidden="true"></i>
					<span>{{ $t('No network interface detected') }}</span>
				</div>

				<!-- Live traffic sparkline. Plain SVG on purpose: the previous
				     apexcharts area chart with a gradient fill appended a new
				     <linearGradient> (+3 <stop>) to its <defs> on every
				     updateSeries() and never removed the old ones, so the widget
				     grew by ~90 DOM nodes every 90s forever. Two <path>s whose
				     `d` attribute changes keep the node count constant. -->
				<svg
					v-else
					class="net-sparkline"
					:viewBox="`0 0 ${SPARK_W} ${SPARK_H}`"
					preserveAspectRatio="none"
					role="img"
					:aria-label="sparklineLabel"
				>
					<path class="net-spark-area is-down" :d="sparkPaths.downArea"></path>
					<path class="net-spark-area is-up" :d="sparkPaths.upArea"></path>
					<path class="net-spark-line is-down" :d="sparkPaths.downLine" vector-effect="non-scaling-stroke"></path>
					<path class="net-spark-line is-up" :d="sparkPaths.upLine" vector-effect="non-scaling-stroke"></path>
				</svg>
			</div>
		</div>
	</div>
</template>

<script>
import { mixin } from '@/mixins/mixin';

const LAN_STREAMS = 6;
const LAN_DURATION = 10000;
const LAN_RAMP = 0.3; // ignore the first 30% (TCP ramp-up)
const LAN_OVERHEAD = 1.04; // OpenSpeedTest's header-overhead compensation
const LAN_UP_CHUNK = 16 * 1024 * 1024;

const round1 = v => Math.round(v * 10) / 10;

function readTestMode() {
	try {
		return localStorage.getItem('speedtestMode') === 'device' ? 'device' : 'server';
	} catch (e) {
		return 'server';
	}
}

// Ping = fastest sample (others include queueing), jitter = mean change
// between consecutive samples - the OpenSpeedTest / Ookla definitions.
function pingStats(samples) {
	if (!samples.length) return { ping: null, jitter: null };
	let diff = 0;
	for (let i = 1; i < samples.length; i++) diff += Math.abs(samples[i] - samples[i - 1]);
	return { ping: round1(Math.min(...samples)), jitter: samples.length > 1 ? round1(diff / (samples.length - 1)) : 0 };
}

function lanResult(slices) {
	if (!slices.length) return 0;
	const end = slices[slices.length - 1].at;
	const steady = slices.filter(s => s.at >= end * LAN_RAMP);
	const use = steady.length ? steady : slices;
	return (use.reduce((a, s) => a + s.mbps, 0) / use.length) * LAN_OVERHEAD;
}

async function lanDownloadStream(origin, signal, count, stop) {
	while (!stop()) {
		const res = await fetch(`${origin}/speedtest/download?_t=${Date.now()}_${Math.random()}`, { cache: 'no-store', signal });
		if (!res.ok || !res.body) throw new Error('download ' + res.status);
		const reader = res.body.getReader();
		for (;;) {
			if (stop()) {
				reader.cancel();
				return;
			}
			const { done, value } = await reader.read();
			if (done) break;
			count.bytes += value.length;
		}
	}
}

// XHR, not fetch: only XHR reports upload progress, so the count follows
// the bytes actually sent rather than jumping per finished request.
function lanUploadStream(origin, blob, signal, count, stop) {
	return new Promise((resolve, reject) => {
		const next = () => {
			if (stop() || signal.aborted) return resolve();
			const xhr = new XMLHttpRequest();
			let sent = 0;
			xhr.open('POST', `${origin}/speedtest/upload?_t=${Date.now()}_${Math.random()}`);
			xhr.upload.onprogress = e => {
				count.bytes += e.loaded - sent;
				sent = e.loaded;
				if (stop()) xhr.abort();
			};
			xhr.onload = next;
			xhr.onabort = resolve;
			xhr.onerror = () => reject(new Error('upload failed'));
			signal.addEventListener('abort', () => xhr.abort(), { once: true });
			xhr.send(blob);
		};
		next();
	});
}

// Sparkline geometry (viewBox units; the SVG stretches to the box).
const SPARK_W = 200;
const SPARK_H = 54;
const SPARK_POINTS = 40;
const NETWORK_KEY = 'networkName';
const LEGACY_NETWORK_KEY = 'networkId';

function readStoredInterface() {
	try {
		return localStorage.getItem(NETWORK_KEY) || '';
	} catch (e) {
		return '';
	}
}

// Builds a line + closed-area path for `values` scaled to `max`.
function sparkPath(values, max) {
	const n = values.length;
	if (n < 2 || !max) {
		return { line: `M0 ${SPARK_H - 1} L${SPARK_W} ${SPARK_H - 1}`, area: '' };
	}
	// Spread whatever history exists over the full width (as the old
	// apexcharts sparkline did) instead of leaving the box mostly empty.
	const step = SPARK_W / (n - 1);
	const x0 = 0;
	let line = '';
	for (let i = 0; i < n; i++) {
		const x = (x0 + step * i).toFixed(1);
		const y = (SPARK_H - 1 - (values[i] / max) * (SPARK_H - 4)).toFixed(1);
		line += (i ? ' L' : 'M') + x + ' ' + y;
	}
	const area = `${line} L${SPARK_W} ${SPARK_H} L${x0.toFixed(1)} ${SPARK_H} Z`;
	return { line, area };
}

export default {
	mixins: [mixin],
	// eslint-disable-next-line vue/multi-word-component-names
	name: 'network',
	icon: "network-outline",
	title: "Network Status",
	gridCols: 3,
	gridRows: 2,
	initShow: true,
	data() {
		return {
			SPARK_W,
			SPARK_H,
			isTesting: false,
			showingResults: false,
			testPhase: 'idle', // 'idle' | 'ping' | 'download' | 'upload' | 'server_running' | 'done' | 'error'
			testMode: readTestMode(), // 'device' (LAN) | 'server' (Internet)
			testResults: {
				ping: null,
				jitter: null,
				download: null,
				upload: null
			},
			testServer: '',
			liveSpeedMbps: 0,
			testError: null,
			abortController: null,
			initNetwork: [],
			// The interface is identified by NAME, not by its position in
			// the list: the order changes when a NIC is hot-plugged or
			// renamed, and an index silently switched to (and mixed the
			// history of) another NIC.
			networkName: readStoredInterface(),
			// Only the selected NIC's samples are reactive; every NIC's
			// history lives in the non-reactive this.history map (created()).
			downSeries: [],
			upSeries: [],
			currentUpSpeed: 0,
			currentDownSpeed: 0
		};
	},
	computed: {
		activeInterface() {
			return this.initNetwork.find(n => n.name === this.networkName) || null;
		},
		activeInterfaceName() {
			if (this.activeInterface) return this.activeInterface.name;
			return this.initNetwork.length ? this.$t('Interface') : this.$t('No interface');
		},
		activeInterfaceStateClass() {
			return this.stateClass(this.activeInterface && this.activeInterface.state);
		},
		activeInterfaceStateLabel() {
			const st = this.activeInterfaceStateClass;
			if (st === 'up') return this.$t('Link up');
			if (st === 'down') return this.$t('Link down');
			return this.$t('Link state unknown');
		},
		speedtestButtonLabel() {
			if (this.isTesting) return this.$t('Testing... (Click to cancel)');
			return this.showingResults ? this.$t('Back to Live Traffic') : this.$t('Start Speedtest');
		},
		sparkPaths() {
			const max = Math.max(1, ...this.downSeries, ...this.upSeries);
			const down = sparkPath(this.downSeries, max);
			const up = sparkPath(this.upSeries, max);
			return { downLine: down.line, downArea: down.area, upLine: up.line, upArea: up.area };
		},
		sparklineLabel() {
			return this.$t('Live traffic on {name}: {down} down, {up} up', {
				name: this.activeInterfaceName,
				down: `${this.formatSpeed(this.currentDownSpeed)} ${this.speedUnit(this.currentDownSpeed)}`,
				up: `${this.formatSpeed(this.currentUpSpeed)} ${this.speedUnit(this.currentUpSpeed)}`
			});
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
				case 'server_running': return 10;
				case 'done': return 100;
				default: return 0;
			}
		},
		phaseStatusText() {
			switch (this.testPhase) {
				case 'ping': return this.$t('Measuring Latency...');
				case 'download': return this.$t('Testing Download...');
				case 'upload': return this.$t('Testing Upload...');
				case 'server_running': return this.$t('Finding nearest server...');
				case 'done': return this.$t('Speedtest Complete');
				case 'error': return this.testError || this.$t('Test Failed');
				default: return this.$t('Ready');
			}
		}
	},
	created() {
		// name -> { down: [], up: [], recv, sent, time } (non-reactive).
		this.history = Object.create(null);
		const initial = this.$store.state.hardwareInfo.net || [];
		this.setInterfaces(initial);
		this.buildDatas(initial);
	},
	beforeDestroy() {
		if (this.abortController) {
			this.abortController.abort();
		}
	},
	methods: {
		stateClass(state) {
			const st = String(state || '').trim().toLowerCase();
			if (st === 'up') return 'up';
			if (st === 'down' || st === 'lowerlayerdown' || st === 'notpresent') return 'down';
			return 'unknown';
		},
		// Takes the backend list, migrates the old index-based selection
		// and picks a sensible default (first interface whose link is up).
		setInterfaces(list) {
			this.initNetwork = Array.isArray(list) ? list.filter(n => n && n.name) : [];
			if (!this.initNetwork.length) return;
			if (!this.networkName) {
				try {
					const legacy = localStorage.getItem(LEGACY_NETWORK_KEY);
					if (legacy !== null) {
						const byIndex = this.initNetwork[parseInt(legacy, 10)];
						if (byIndex) {
							this.networkName = byIndex.name;
							localStorage.setItem(NETWORK_KEY, byIndex.name);
						}
						localStorage.removeItem(LEGACY_NETWORK_KEY);
					}
				} catch (e) { /* storage unavailable */ }
			}
			if (!this.activeInterface) {
				// Stored NIC is gone (unplugged/renamed): show another one
				// without overwriting the stored choice, so it comes back
				// when the NIC does.
				const up = this.initNetwork.find(n => this.stateClass(n.state) === 'up');
				this.networkName = (up || this.initNetwork[0]).name;
				this.syncSeries();
			}
		},
		selectInterface(name) {
			if (!name || name === this.networkName) return;
			this.networkName = name;
			try { localStorage.setItem(NETWORK_KEY, name); } catch (e) { /* private mode */ }
			this.syncSeries();
		},
		syncSeries() {
			const h = this.history && this.history[this.networkName];
			this.downSeries = h ? h.down.slice() : [];
			this.upSeries = h ? h.up.slice() : [];
			this.currentDownSpeed = this.downSeries.length ? this.downSeries[this.downSeries.length - 1] : 0;
			this.currentUpSpeed = this.upSeries.length ? this.upSeries[this.upSeries.length - 1] : 0;
		},
		formatSpeed(kb) {
			const bytes = (parseFloat(kb) || 0) * 1024;
			if (bytes <= 0) return '0';
			if (bytes < 1024) return bytes.toFixed(0);
			const k = 1024;
			const i = Math.min(4, Math.floor(Math.log(bytes) / Math.log(k)));
			const val = parseFloat((bytes / Math.pow(k, i)).toFixed(1));
			return isNaN(val) ? '0' : val;
		},
		speedUnit(kb) {
			const bytes = (parseFloat(kb) || 0) * 1024;
			if (bytes < 1024) return 'B/s';
			const k = 1024;
			const sizes = ['B/s', 'KB/s', 'MB/s', 'GB/s', 'TB/s'];
			const i = Math.floor(Math.log(bytes) / Math.log(k));
			return sizes[Math.min(i, sizes.length - 1)] || 'KB/s';
		},
		// Samples are cumulative byte counters + a unix-seconds timestamp;
		// the rate is the delta between two samples of the SAME interface.
		buildDatas(data) {
			if (!data || data.length === 0) return;
			const seen = new Set();
			data.forEach(el => {
				if (!el || !el.name) return;
				seen.add(el.name);
				let h = this.history[el.name];
				if (!h) {
					h = this.history[el.name] = { down: [], up: [], recv: null, sent: null, time: null };
				}
				const recv = Number(el.bytesRecv) || 0;
				const sent = Number(el.bytesSent) || 0;
				const time = Number(el.time) || Date.now() / 1000;
				if (h.time !== null) {
					const dt = time - h.time;
					// dt<=0: duplicate/out-of-order sample. A counter going
					// backwards means the NIC was reset/re-created - skip that
					// sample instead of plotting a garbage rate.
					if (dt > 0 && recv >= h.recv && sent >= h.sent) {
						h.down.push(this.covertToKB((recv - h.recv) / dt));
						h.up.push(this.covertToKB((sent - h.sent) / dt));
						if (h.down.length > SPARK_POINTS) h.down.shift();
						if (h.up.length > SPARK_POINTS) h.up.shift();
					}
				}
				h.recv = recv;
				h.sent = sent;
				h.time = time;
			});
			// Forget NICs that disappeared so a hot-plugged replacement with
			// the same name starts from a clean baseline.
			Object.keys(this.history).forEach(name => {
				if (!seen.has(name)) delete this.history[name];
			});
			// Nothing to redraw while the page is in the background; the
			// counters above keep the baseline right for when it returns.
			if (document.hidden) return;
			this.syncSeries();
		},
		covertToKB(bytes) {
			const kb = Math.round(bytes / 1024);
			return kb > 0 && isFinite(kb) ? kb : 0;
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
			this.syncSeries();
		},
		cancelSpeedtest() {
			if (this.abortController) {
				this.abortController.abort();
			}
			this.isTesting = false;
			this.showingResults = false;
			this.testPhase = 'idle';
			this.syncSeries();
		},

		setTestMode(mode) {
			if (this.isTesting) return;
			this.testMode = mode;
			try { localStorage.setItem('speedtestMode', mode); } catch (e) { /* private mode */ }
			this.testResults = { ping: null, jitter: null, download: null, upload: null };
			this.testServer = '';
			this.testPhase = 'idle';
		},

		startInlineSpeedtest() {
			if (this.isTesting) return;
			this.isTesting = true;
			this.showingResults = true;
			this.testError = null;
			this.liveSpeedMbps = 0;
			this.testResults = { ping: null, jitter: null, download: null, upload: null };
			this.testServer = '';
			this.abortController = new AbortController();
			const run = this.testMode === 'server' ? this.runServerSpeedtest() : this.runLanSpeedtest();
			run.catch(err => {
				if (err && err.name === 'AbortError') return;
				if (!this.isTesting) return;
				this.testError = (err && err.message) || this.$t('Test Failed');
				this.testPhase = 'error';
			}).finally(() => {
				this.isTesting = false;
			});
		},

		// LAN test (this device <-> NivaroOS), measured like OpenSpeedTest:
		// several parallel streams for a fixed time, throughput sampled in
		// slices, ramp-up slices ignored, +4% for TCP/HTTP header overhead.
		// No size or speed cap: streams re-request until the time is up.
		async runLanSpeedtest() {
			const origin = `${window.location.protocol}//${window.location.host}`;
			const signal = this.abortController.signal;

			this.testPhase = 'ping';
			this.testServer = 'NivaroOS · ' + window.location.hostname;
			const pings = [];
			for (let i = 0; i < 21; i++) {
				if (!this.isTesting) return;
				const t0 = performance.now();
				const res = await fetch(`${origin}/speedtest/ping?_t=${Date.now()}_${i}`, { cache: 'no-store', signal });
				await res.text();
				if (i > 0) pings.push(performance.now() - t0); // first one opens the connection
			}
			const { ping, jitter } = pingStats(pings);
			this.testResults.ping = ping;
			this.testResults.jitter = jitter;

			this.testPhase = 'download';
			this.testResults.download = await this.measureStreams(signal, (count, stop) => lanDownloadStream(origin, signal, count, stop));
			if (!this.isTesting) return;

			this.testPhase = 'upload';
			const payload = new Uint8Array(LAN_UP_CHUNK);
			// crypto.getRandomValues is limited to 64 KiB per call.
			for (let o = 0; o < payload.length; o += 65536) crypto.getRandomValues(payload.subarray(o, o + 65536));
			const blob = new Blob([payload]);
			this.testResults.upload = await this.measureStreams(signal, (count, stop) => lanUploadStream(origin, blob, signal, count, stop));
			if (!this.isTesting) return;
			this.testPhase = 'done';
		},

		// Runs LAN_STREAMS streams for LAN_DURATION ms, updating the live
		// number; resolves with the result in Mbps.
		async measureStreams(signal, startStream) {
			this.liveSpeedMbps = 0;
			const count = { bytes: 0 };
			let stopped = false;
			const stop = () => stopped;
			const streams = [];
			for (let i = 0; i < LAN_STREAMS; i++) streams.push(startStream(count, stop).catch(() => {}));
			const slices = [];
			const t0 = performance.now();
			let lastBytes = 0;
			let lastT = t0;
			await new Promise(resolve => {
				const timer = setInterval(() => {
					const now = performance.now();
					const dt = (now - lastT) / 1000;
					if (dt > 0) slices.push({ at: now - t0, mbps: ((count.bytes - lastBytes) * 8) / dt / 1e6 });
					lastBytes = count.bytes;
					lastT = now;
					this.liveSpeedMbps = round1(lanResult(slices));
					if (!this.isTesting || now - t0 >= LAN_DURATION) {
						clearInterval(timer);
						resolve();
					}
				}, 200);
			});
			stopped = true;
			await Promise.race([Promise.all(streams), new Promise(r => setTimeout(r, 1500))]);
			if (!count.bytes) throw new Error(this.$t('No data could be transferred'));
			return round1(lanResult(slices));
		},

		// Internet test runs on the server against the nearest speedtest.net
		// server (picked automatically per run); we poll its progress.
		async runServerSpeedtest() {
			this.testPhase = 'server_running';
			try {
				await this.$api.sys.startSpeedtest();
			} catch (err) {
				// 409: one is already running (another tab) - follow it.
				if (!(err.response && err.response.status === 409)) throw new Error(err.response?.data?.message || err.message);
			}
			for (;;) {
				if (!this.isTesting) return;
				await new Promise(r => setTimeout(r, 400));
				const res = await this.$api.sys.getSpeedtestStatus();
				const st = res.data && res.data.data;
				if (!st || !this.isTesting) continue;
				const r = st.result || {};
				if (r.server) this.testServer = r.server;
				if (r.ping_ms) {
					this.testResults.ping = r.ping_ms;
					this.testResults.jitter = r.jitter_ms;
				}
				if (r.download_mbps) this.testResults.download = r.download_mbps;
				if (st.phase === 'ping') this.testPhase = 'ping';
				else if (st.phase === 'download' || st.phase === 'upload') {
					this.testPhase = st.phase;
					this.liveSpeedMbps = st.live_mbps || 0;
				} else if (st.phase === 'done') {
					this.testResults.upload = r.upload_mbps;
					this.testPhase = 'done';
					return;
				} else if (st.phase === 'error') {
					throw new Error(st.error || this.$t('Speedtest failed'));
				}
			}
		}
	},
	sockets: {
		"nivaroos:system:utilization"(res) {
			let list;
			try {
				list = JSON.parse(res.Properties.sys_net) || [];
			} catch (e) {
				return;
			}
			// Keep the counters current even while hidden (cheap), but skip
			// re-rendering the interface list when nobody can see it.
			if (!document.hidden) this.setInterfaces(list);
			this.buildDatas(list);
		}
	}
};
</script>

<style lang="scss">
.speedtest-btn {
	color: var(--theme-desktop-glass-icon, #475569);
	font-size: 0.95rem;

	&.is-active {
		color: var(--color-primary-fg, #1d4ed8) !important;
		background: rgba(37, 99, 235, 0.12) !important;
	}

	&:hover {
		color: var(--color-primary-fg, #1d4ed8) !important;
		background: rgba(37, 99, 235, 0.1) !important;
	}
}

.net-state-dot {
	font-size: 7px;
	vertical-align: middle;

	&.is-up { color: var(--color-success-fg, #047857); }
	&.is-down { color: var(--color-danger-fg, #b91c1c); }
	&.is-unknown { color: var(--theme-desktop-glass-text-sub, #475569); }
}

.net-sparkline {
	display: block;
	width: 100%;
	height: 54px;
	overflow: visible;

	.net-spark-area {
		stroke: none;
		&.is-down { fill: rgba(37, 99, 235, 0.16); }
		&.is-up { fill: rgba(16, 185, 129, 0.14); }
	}

	.net-spark-line {
		fill: none;
		stroke-width: 1.5;
		stroke-linejoin: round;
		stroke-linecap: round;
		&.is-down { stroke: var(--color-primary-fg, #1d4ed8); }
		&.is-up { stroke: var(--color-success-fg, #047857); }
	}
}

.net-empty-state {
	height: 54px;
	display: flex;
	align-items: center;
	justify-content: center;
	gap: 6px;
	font-size: 0.68rem;
	color: var(--theme-desktop-glass-text-sub, #475569);

	.mdi {
		font-size: 0.95rem;
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

	&:focus-visible {
		outline: 2px solid var(--color-primary-fg, #1d4ed8);
		outline-offset: 1px;
	}

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
					color: var(--color-primary-fg, #1d4ed8) !important;
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
			color: var(--color-primary-fg, #1d4ed8);
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

	.speedtest-mode-toggle {
		display: inline-flex;
		border-radius: 6px;
		overflow: hidden;
		background: rgba(37, 99, 235, 0.08);

		button {
			display: inline-flex;
			align-items: center;
			gap: 3px;
			padding: 2px 7px;
			border: 0;
			background: transparent;
			color: var(--theme-desktop-glass-text-sub, #475569);
			font-size: 0.62rem;
			font-weight: 700;
			cursor: pointer;

			&.is-on {
				background: #1d4ed8;
				color: #fff;
			}

			&:disabled {
				cursor: default;
			}

			&:focus-visible {
				outline: 2px solid var(--color-primary-fg, #1d4ed8);
				outline-offset: -2px;
			}
		}
	}

	.speedtest-mode-badge {
		display: inline-flex;
		align-items: center;
		gap: 3px;
		padding: 1px 6px;
		border-radius: 4px;
		background: rgba(37, 99, 235, 0.1);
		color: var(--color-primary-fg, #1d4ed8);
		font-size: 0.6rem;
		font-weight: 700;
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}
}

.net-speedtest-ctrl-row {
	.speedtest-server {
		flex: 1 1 auto;
		min-width: 0;
		overflow: hidden;
		white-space: nowrap;
		text-overflow: ellipsis;
		text-align: right;
		font-size: 0.6rem;
		color: var(--theme-desktop-glass-text-sub, #64748b);
	}

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
			color: var(--color-success-fg, #047857);
			font-size: 0.78rem;
			flex-shrink: 0;
		}

		.error-icon {
			color: var(--color-danger-fg, #b91c1c);
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
				color: var(--color-primary-fg, #1d4ed8);
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