<template>
	<div class="widget network is-relative">
		<div class="blur-background"></div>

		<div class="widget-content">
			<div class="widget-header">
				<div class="widget-header-left">
					<div class="widget-badge is-net">
						<i class="mdi mdi-swap-vertical" aria-hidden="true"></i>
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
						:class="{ 'is-active': speedMode }"
						:title="speedtestButtonLabel"
						:aria-label="speedtestButtonLabel"
						:aria-pressed="speedMode ? 'true' : 'false'"
						@click.stop="toggleSpeedtest"
					>
						<i class="mdi" :class="isTesting ? 'mdi-loading mdi-spin' : 'mdi-speedometer'" aria-hidden="true"></i>
					</button>
					<button
						type="button"
						class="widget-icon-btn"
						:title="$t('Interfaces')"
						:aria-label="$t('Interfaces')"
						:aria-expanded="showMore ? 'true' : 'false'"
						aria-controls="net-more"
						@click="showMore = !showMore"
					>
						<i class="mdi mdi-chevron-right arrow-btn" :class="{ open: showMore }" aria-hidden="true"></i>
					</button>
				</div>
			</div>

			<!-- Hero: the two numbers that matter -->
			<div class="net-hero">
				<div class="net-rate is-down" :class="{ 'is-testing': isTesting && testPhase === 'download' }">
					<span class="net-rate-label"><i class="mdi mdi-arrow-down" aria-hidden="true"></i>{{ downCardLabel }}</span>
					<span class="net-rate-value">{{ displayDownSpeed }}<span class="unit">{{ displayDownUnit }}</span></span>
				</div>
				<div class="net-rate is-up" :class="{ 'is-testing': isTesting && testPhase === 'upload' }">
					<span class="net-rate-label"><i class="mdi mdi-arrow-up" aria-hidden="true"></i>{{ upCardLabel }}</span>
					<span class="net-rate-value">{{ displayUpSpeed }}<span class="unit">{{ displayUpUnit }}</span></span>
				</div>
			</div>

			<!-- Speedtest panel (replaces the chart while open) -->
			<div v-if="speedMode" class="net-speedtest">
				<div class="speedtest-mode-toggle" role="group" :aria-label="$t('Speedtest type')">
						<button
							type="button"
							:class="{ 'is-on': testMode === 'server' }"
							:aria-pressed="testMode === 'server' ? 'true' : 'false'"
							:disabled="isTesting"
							:title="$t('Server ↔ Internet (nearest speedtest.net server)')"
							@click.stop="setTestMode('server')"
						>
							<i class="mdi mdi-web" aria-hidden="true"></i> {{ $t('Internet') }}
						</button>
						<button
							type="button"
							:class="{ 'is-on': testMode === 'device' }"
							:aria-pressed="testMode === 'device' ? 'true' : 'false'"
							:disabled="isTesting"
							:title="$t('This device ↔ NivaroOS (LAN)')"
							@click.stop="setTestMode('device')"
						>
							<i class="mdi mdi-lan" aria-hidden="true"></i> {{ $t('LAN') }}
						</button>
				</div>

				<div class="net-speedtest-row">
					<span class="speedtest-status-line" role="status">
						<span v-if="isTesting" class="test-pulse-dot" aria-hidden="true"></span>
						<i v-else-if="testPhase === 'done'" class="mdi mdi-check-circle done-icon" aria-hidden="true"></i>
						<i v-else-if="testPhase === 'error'" class="mdi mdi-alert-circle error-icon" aria-hidden="true"></i>
						<span class="status-msg">{{ phaseStatusText }}</span>
					</span>
					<button v-if="!isTesting" type="button" class="speedtest-start-btn" @click.stop="startInlineSpeedtest">
						<i :class="['mdi', hasTestRun ? 'mdi-refresh' : 'mdi-play']" aria-hidden="true"></i>
						<span>{{ hasTestRun ? $t('Test again') : $t('Start test') }}</span>
					</button>
					<button v-else type="button" class="speedtest-start-btn is-stop" @click.stop="cancelSpeedtest">
						<i class="mdi mdi-stop" aria-hidden="true"></i>
						<span>{{ $t('Stop') }}</span>
					</button>
				</div>

				<div class="speedtest-meta">
					{{ $t('Ping') }}
					<b>{{ testResults.ping !== null ? testResults.ping + ' ms' : '--' }}</b>
					<span v-if="testResults.jitter !== null" :title="$t('Jitter')">±{{ testResults.jitter }}</span>
					<template v-if="testServer"> · {{ testServer }}</template>
				</div>

				<div v-if="isTesting" class="speedtest-progress-bar" aria-hidden="true">
					<div class="speedtest-progress-fill" :style="{ width: testProgressPercent + '%' }"></div>
				</div>
			</div>

			<!-- No interface reported at all (e.g. a container host whose only
			     NIC the backend could not classify). -->
			<div v-else-if="!initNetwork.length" class="net-empty-state">
				<i class="mdi mdi-lan-disconnect" aria-hidden="true"></i>
				<span>{{ $t('No network interface detected') }}</span>
			</div>

			<template v-else>
				<!-- Live traffic sparkline. Plain SVG on purpose: the previous
				     apexcharts area chart appended a new <linearGradient> to its
				     <defs> on every updateSeries() and never removed the old
				     ones (~90 DOM nodes every 90s). Paths whose `d` changes keep
				     the node count constant. -->
				<svg
					class="net-sparkline"
					:viewBox="`0 0 ${SPARK_W} ${SPARK_H}`"
					preserveAspectRatio="none"
					role="img"
					:aria-label="sparklineLabel"
				>
					<path class="net-spark-area" :d="sparkPaths.downArea"></path>
					<path class="net-spark-line is-down" :d="sparkPaths.downLine" vector-effect="non-scaling-stroke"></path>
					<path class="net-spark-line is-up" :d="sparkPaths.upLine" vector-effect="non-scaling-stroke"></path>
				</svg>

				<dl class="net-facts">
					<template v-if="lanIp">
						<dt>{{ $t('LAN') }}</dt>
						<dd><button type="button" class="net-copy" :title="$t('Copy {ip}', { ip: lanIp })" :aria-label="$t('Copy LAN IP {ip}', { ip: lanIp })" @click="copyIp(lanIp)">{{ lanIp }}<i class="mdi mdi-content-copy" aria-hidden="true"></i></button></dd>
					</template>
					<template v-if="tailscaleIp">
						<dt>{{ $t('Tailscale') }}</dt>
						<dd><button type="button" class="net-copy" :title="$t('Copy {ip}', { ip: tailscaleIp })" :aria-label="$t('Copy Tailscale IP {ip}', { ip: tailscaleIp })" @click="copyIp(tailscaleIp)">{{ tailscaleIp }}<i class="mdi mdi-content-copy" aria-hidden="true"></i></button></dd>
					</template>
					<template v-if="activeInterface">
						<dt>{{ $t('Since boot') }}</dt>
						<dd :title="$t('Received and sent on {name} since the last boot', { name: activeInterfaceName })">
							↓ {{ renderSize(Number(activeInterface.bytesRecv) || 0) }} · ↑ {{ renderSize(Number(activeInterface.bytesSent) || 0) }}
						</dd>
					</template>
				</dl>
			</template>

			<!-- Every interface; pick the one the widget follows -->
			<div v-if="showMore" id="net-more" class="more-info">
				<div class="process-section-title">{{ $t('Interfaces') }}</div>
				<div v-if="!initNetwork.length" class="has-text-centered is-size-7 py-2 text-muted">{{ $t('No interface') }}</div>
				<button
					v-for="item in initNetwork"
					:key="'net-' + item.name"
					type="button"
					class="process-row net-iface-row"
					:class="{ 'is-selected': item.name === networkName }"
					:aria-pressed="item.name === networkName ? 'true' : 'false'"
					:title="$t('Show {name} in the widget', { name: item.name })"
					@click="selectInterface(item.name)"
				>
					<i class="mdi mdi-circle net-state-dot" :class="'is-' + stateClass(item.state)" aria-hidden="true"></i>
					<span class="net-iface-name">
						<span class="process-name">{{ item.name }}</span>
						<span v-if="ipOf(item.name)" class="net-iface-ip">{{ ipOf(item.name) }}</span>
					</span>
					<span class="process-usage">
						<span>↓ {{ rateText(rates[item.name] && rates[item.name].down) }}</span>
						<span>↑ {{ rateText(rates[item.name] && rates[item.name].up) }}</span>
					</span>
				</button>
			</div>
		</div>
	</div>
</template>

<script>
import copy from 'clipboard-copy';
import { mixin } from '@/mixins/mixin';
import { formatRate, stateClass, defaultInterface, ipOf, sparkPath } from '@/utils/netWidget';

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
const SPARK_H = 40;
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
			showMore: false,
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
			currentDownSpeed: 0,
			// name -> { down, up } latest KB/s, for the interface list.
			rates: {},
			// GET /sys/network-interfaces: [{ interface, ip }], fetched once.
			netIfaces: [],
			tailscaleIp: ''
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
		speedMode() {
			return this.isTesting || this.showingResults;
		},
		lanIp() {
			const ip = this.ipOf(this.networkName);
			return ip === this.tailscaleIp ? '' : ip;
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
		// A finished (or failed) run - the start button then reads "Test again".
		hasTestRun() {
			return this.testPhase === 'done' || this.testPhase === 'error';
		},
		speedtestButtonLabel() {
			if (this.isTesting) return this.$t('Testing... (Click to cancel)');
			return this.showingResults ? this.$t('Back to Live Traffic') : this.$t('Speedtest');
		},
		sparkPaths() {
			const max = Math.max(1, ...this.downSeries, ...this.upSeries);
			const down = sparkPath(this.downSeries, max, SPARK_W, SPARK_H);
			const up = sparkPath(this.upSeries, max, SPARK_W, SPARK_H);
			return { downLine: down.line, downArea: down.area, upLine: up.line };
		},
		sparklineLabel() {
			return this.$t('Live traffic on {name}: {down} down, {up} up', {
				name: this.activeInterfaceName,
				down: this.rateText(this.currentDownSpeed),
				up: this.rateText(this.currentUpSpeed)
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
			if (this.showingResults) {
				return this.testResults.download !== null ? this.testResults.download.toFixed(1) : '--';
			}
			return formatRate(this.currentDownSpeed).value;
		},
		displayDownUnit() {
			if (this.isTesting || this.showingResults) {
				return 'Mbps';
			}
			return formatRate(this.currentDownSpeed).unit;
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
			if (this.showingResults) {
				return this.testResults.upload !== null ? this.testResults.upload.toFixed(1) : '--';
			}
			return formatRate(this.currentUpSpeed).value;
		},
		displayUpUnit() {
			if (this.isTesting || this.showingResults) {
				return 'Mbps';
			}
			return formatRate(this.currentUpSpeed).unit;
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
				default: return this.$t('Pick a test');
			}
		}
	},
	created() {
		// name -> { down: [], up: [], recv, sent, time } (non-reactive).
		this.history = Object.create(null);
		const initial = this.$store.state.hardwareInfo.net || [];
		this.setInterfaces(initial);
		this.buildDatas(initial);
		// Addresses for the facts lines / interface list. One-shot: they
		// rarely change, and both endpoints already serve Settings.
		this.$api.sys.getNetworkInterfaces().then(res => {
			if (res.data.success === 200) this.netIfaces = res.data.data || [];
		}).catch(() => {});
		this.$api.tailscale.getStatus().then(res => {
			const ips = (res.data.success === 200 && res.data.data && res.data.data.TailscaleIPs) || [];
			this.tailscaleIp = ips.find(ip => !ip.includes(':')) || ips[0] || '';
		}).catch(() => { /* Tailscale not installed or not running */ });
	},
	beforeDestroy() {
		if (this.abortController) {
			this.abortController.abort();
		}
	},
	methods: {
		stateClass,
		ipOf(name) {
			return ipOf(this.netIfaces, name);
		},
		rateText(kb) {
			const r = formatRate(kb);
			return r.value + ' ' + r.unit;
		},
		copyIp(ip) {
			copy(ip).then(() => {
				this.$buefy.toast.open({ message: this.$t('Copied {ip}', { ip }), type: 'is-info', duration: 1500 });
			}).catch(() => {});
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
				this.networkName = defaultInterface(this.initNetwork).name;
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
			const rates = {};
			for (const name in this.history) {
				const h = this.history[name];
				rates[name] = { down: h.down[h.down.length - 1] || 0, up: h.up[h.up.length - 1] || 0 };
			}
			this.rates = rates;
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
				this.openSpeedtest();
			}
		},
		// Opens the speedtest panel without running anything: pick Internet
		// or LAN first, then press Start.
		openSpeedtest() {
			this.showingResults = true;
			this.testError = null;
			this.testResults = { ping: null, jitter: null, download: null, upload: null };
			this.testServer = '';
			this.testPhase = 'idle';
		},
		exitSpeedtest() {
			this.isTesting = false;
			this.showingResults = false;
			this.testPhase = 'idle';
			this.syncSeries();
		},
		// Stop leaves the panel open and ready, so another test is one click
		// away; the header button (or the chart button) goes back to traffic.
		cancelSpeedtest() {
			if (this.abortController) {
				this.abortController.abort();
			}
			this.isTesting = false;
			this.openSpeedtest();
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
.widget.network {
	--net-down: var(--color-primary-fg, #1d4ed8);
	--net-up: var(--color-success-fg, #047857);

	.arrow-btn {
		display: inline-block;
		transition: transform 0.25s ease;

		&.open {
			transform: rotate(90deg);
		}
	}

	.net-state-dot {
		font-size: 7px;
		vertical-align: middle;

		&.is-up { color: var(--color-success-fg, #047857); }
		&.is-down { color: var(--color-danger-fg, #b91c1c); }
		&.is-unknown { color: var(--theme-desktop-glass-text-sub, #475569); }
	}

	/* ── Hero: down / up ── */
	.net-hero {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 0.75rem;
		margin-bottom: 0.45rem;
	}

	.net-rate {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;

		&.is-down .mdi { color: var(--net-down); }
		&.is-up .mdi { color: var(--net-up); }
		&.is-testing.is-down .net-rate-value { color: var(--net-down); }
		&.is-testing.is-up .net-rate-value { color: var(--net-up); }
	}

	.net-rate-label {
		display: flex;
		align-items: center;
		gap: 3px;
		font-size: 0.62rem;
		font-weight: 500;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--theme-desktop-glass-text-sub, #475569);
		line-height: 1;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;

		.mdi {
			font-size: 0.8rem;
		}
	}

	.net-rate-value {
		font-size: 1.45rem;
		font-weight: 600;
		letter-spacing: -0.02em;
		font-variant-numeric: tabular-nums;
		color: var(--theme-desktop-glass-text, #0f172a);
		line-height: 1.1;

		.unit {
			font-size: 0.7rem;
			font-weight: 500;
			letter-spacing: 0;
			color: var(--theme-desktop-glass-text-sub, #475569);
			margin-left: 3px;
		}
	}

	/* ── Live chart ── */
	.net-sparkline {
		display: block;
		width: 100%;
		height: 40px;
		overflow: visible;

		.net-spark-area {
			stroke: none;
			fill: var(--net-down);
			opacity: 0.1;
		}

		.net-spark-line {
			fill: none;
			stroke-width: 1.5;
			stroke-linejoin: round;
			stroke-linecap: round;
			&.is-down { stroke: var(--net-down); }
			&.is-up { stroke: var(--net-up); }
		}
	}

	.net-empty-state {
		height: 40px;
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

	/* ── Secondary facts: one muted line each ── */
	.net-facts {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr);
		column-gap: 0.75rem;
		row-gap: 1px;
		margin: 0.45rem 0 0;
		font-size: 0.68rem;
		line-height: 1.5;

		dt {
			color: var(--theme-desktop-glass-text-sub, #475569);
		}

		dd {
			margin: 0;
			min-width: 0;
			text-align: right;
			font-variant-numeric: tabular-nums;
			color: var(--theme-desktop-glass-text, #0f172a);
			white-space: nowrap;
			overflow: hidden;
			text-overflow: ellipsis;
		}
	}

	.net-copy {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		max-width: 100%;
		padding: 0 2px;
		border: 0;
		border-radius: var(--radius-xs, 4px);
		background: none;
		font: inherit;
		color: inherit;
		cursor: copy;

		.mdi {
			font-size: 0.7rem;
			color: var(--theme-desktop-glass-text-sub, #475569);
			opacity: 0.6;
		}

		&:hover .mdi,
		&:focus-visible .mdi {
			opacity: 1;
		}

		&:hover {
			background: var(--theme-card-hover, rgba(0, 0, 0, 0.06));
		}

		&:focus-visible {
			outline: 2px solid var(--color-primary-fg, #1d4ed8);
			outline-offset: 1px;
		}
	}

	/* ── Interface list (More) ── */
	.net-iface-row {
		width: 100%;
		gap: 0.5rem;
		border: 0;
		background: none;
		font: inherit;
		font-size: 0.72rem;
		color: inherit;
		text-align: left;
		cursor: pointer;

		&.is-selected {
			background: var(--color-primary-soft, rgba(37, 99, 235, 0.1));
		}

		&:focus-visible {
			outline: 2px solid var(--color-primary-fg, #1d4ed8);
			outline-offset: -2px;
		}

		.net-iface-name {
			display: flex;
			flex-direction: column;
			flex: 1 1 auto;
			min-width: 0;
			line-height: 1.3;
			overflow-wrap: anywhere;
		}

		.net-iface-ip {
			font-size: 0.64rem;
			color: var(--theme-desktop-glass-text-sub, #475569);
		}

		.process-usage {
			display: flex;
			flex-direction: column;
			align-items: flex-end;
			flex-shrink: 0;
			font-size: 0.64rem;
			font-weight: 500;
			line-height: 1.3;
			white-space: nowrap;
		}
	}

	/* ── Speedtest panel ── */
	.speedtest-btn.is-active .mdi {
		color: inherit;
	}

	.net-speedtest {
		display: flex;
		flex-direction: column;
		gap: 0.45rem;
		padding-top: 0.55rem;
		border-top: 1px solid var(--theme-desktop-glass-border, rgba(0, 0, 0, 0.08));
	}

	.net-speedtest-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
		font-size: 0.68rem;
		color: var(--theme-desktop-glass-text-sub, #475569);
	}

	.speedtest-mode-toggle {
		display: flex;
		padding: 2px;
		border-radius: 8px;
		background: var(--theme-desktop-glass-track, rgba(0, 0, 0, 0.07));

		button {
			flex: 1 1 0;
			display: inline-flex;
			align-items: center;
			justify-content: center;
			gap: 3px;
			padding: 3px 8px;
			border: 0;
			border-radius: 6px;
			background: transparent;
			color: var(--theme-desktop-glass-text-sub, #475569);
			font: inherit;
			font-size: 0.68rem;
			font-weight: 600;
			cursor: pointer;

			&.is-on {
				background: var(--theme-desktop-glass-bg, #fff);
				color: var(--theme-desktop-glass-text, #0f172a);
				box-shadow: 0 1px 2px rgba(0, 0, 0, 0.12);
			}

			&:disabled {
				cursor: default;
				opacity: 0.7;
			}

			&:focus-visible {
				outline: 2px solid var(--color-primary-fg, #1d4ed8);
				outline-offset: 1px;
			}
		}
	}

	.speedtest-start-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.25rem;
		height: 26px;
		padding: 0 0.7rem;
		border-radius: var(--radius-pill, 999px);
		border: 1px solid transparent;
		background: var(--color-primary, #2563eb);
		color: #ffffff;
		font: inherit;
		font-size: 0.72rem;
		font-weight: 600;
		cursor: pointer;
		white-space: nowrap;
		flex-shrink: 0;

		.mdi {
			font-size: 0.85rem;
		}

		&:hover {
			background: var(--color-primary-hover, #1d4ed8);
		}

		&.is-stop {
			background: transparent;
			color: var(--color-danger-fg, #b91c1c);
			border-color: currentColor;

			&:hover {
				background: var(--color-danger-soft, rgba(239, 68, 68, 0.1));
			}
		}

		&:focus-visible {
			outline: 2px solid var(--color-primary-fg, #1d4ed8);
			outline-offset: 2px;
		}
	}

	.speedtest-status-line {
		display: flex;
		align-items: center;
		gap: 5px;
		flex: 1 1 auto;
		min-width: 0;
		line-height: 1.3;


		.test-pulse-dot {
			width: 6px;
			height: 6px;
			border-radius: 50%;
			background: var(--color-primary, #2563eb);
			animation: testPulse 1.2s infinite ease-in-out;
			flex-shrink: 0;
		}

		.done-icon,
		.error-icon {
			font-size: 0.8rem;
			flex-shrink: 0;
		}

		.done-icon { color: var(--color-success-fg, #047857); }
		.error-icon { color: var(--color-danger-fg, #b91c1c); }
	}

	.speedtest-meta {
		font-size: 0.66rem;
		line-height: 1.4;
		font-variant-numeric: tabular-nums;
		color: var(--theme-desktop-glass-text-sub, #475569);
		overflow-wrap: anywhere;

		b {
			font-weight: 600;
			color: var(--theme-desktop-glass-text, #0f172a);
		}
	}

	.speedtest-progress-bar {
		height: 3px;
		border-radius: 2px;
		background: var(--theme-desktop-glass-track, rgba(0, 0, 0, 0.07));
		overflow: hidden;

		.speedtest-progress-fill {
			height: 100%;
			background: linear-gradient(90deg, var(--net-down), var(--net-up));
			transition: width 0.3s ease;
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.arrow-btn,
		.speedtest-progress-fill {
			transition: none;
		}

		.test-pulse-dot,
		.mdi-spin {
			animation: none;
		}
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
