
<template>
	<div class="terminal-window">
		<!-- This bar IS the window's titlebar now (draggable, own minimize/
		     close, no maximize) - same treatment as Files' TabBar.vue. It
		     stays visible regardless of the Terminal/Logs toggle below, for
		     the same reason Files' tab bar stays visible in Shared/Drop: as
		     the only titlebar, hiding it would remove drag/minimize/close
		     entirely whenever Logs is showing. -->
		<div class="terminal-tabs" @mousedown="$emit('drag-start', $event)">
			<button v-for="tab in terminalTabs" :key="tab.id" class="terminal-tab"
				:class="{ active: tab.id === activeTerminalTabId }" @click="activateTerminalTab(tab.id)">
				<span class="one-line">{{ tab.title }}</span>
				<span v-if="canCloseTab(tab)" class="terminal-tab-close" @click.stop="closeTerminalTab(tab.id)">
					<b-icon icon="close" size="is-small"></b-icon>
				</span>
			</button>
			<button class="terminal-tab-add" :title="$t('New tab')" @click="addTerminalTab">
				<b-icon icon="plus" size="is-small"></b-icon>
			</button>
			<button class="terminal-tab-add" :title="$t('New Window')" @click="openNewWindow">
				<b-icon icon="open-in-new" size="is-small"></b-icon>
			</button>
			<div class="terminal-tabs-spacer"></div>
			<button class="logs-button" :class="{ active: hasLogsTab }" @click="openLogsTab">
				<b-icon icon="history-records-outline" pack="casa" custom-size="casa-14px" />
				<span>{{ $t('Logs') }}</span>
			</button>
			<div class="window-controls">
				<button class="window-btn window-btn-minimize" :title="$t('Minimize')" @click.stop="$emit('minimize')"></button>
				<button class="window-btn window-btn-close" :title="$t('Close')" @click.stop="$emit('close')"></button>
			</div>
		</div>

		<div class="terminal-body">
			<div v-for="tab in shellTabs" :key="tab.id" v-show="tab.id === activeTerminalTabId" class="terminal-body-layer">
				<terminal-card :ref="'terminal-' + tab.id"></terminal-card>
			</div>
			<!-- Deliberately outside the v-for above, not a type-branch inside
			     it: a static ref (needed since there's only ever one Logs
			     pane, unlike the per-id dynamic refs shell tabs use) INSIDE a
			     v-for is always collected as an array in Vue 2, regardless of
			     how many elements actually end up using it - that silently
			     broke every this.$refs.logs.active(...) call below. -->
			<div v-if="hasLogsTab" v-show="activeTerminalTabId === 'logs'" class="terminal-body-layer">
				<logs-card ref="logs" :data="logData"></logs-card>
			</div>
		</div>

		<b-loading v-model="isLoading" :is-full-page="false"></b-loading>
	</div>
</template>

<script>
import TerminalCard from './TerminalCard.vue';
import LogsCard from './LogsCard.vue';

export default {
	name: 'terminal-panel',
	components: {
		TerminalCard,
		LogsCard
	},
	data() {
		return {
			isLoading: false,
			wsUrl: ``,
			logData: "",
			timer: '',
			// Each tab is either a `type: 'shell'` (its own independent pty
			// session - own TerminalCard, own WebSocket) or the single
			// `type: 'logs'` tab (fixed id 'logs', toggled via the logs
			// button rather than freely duplicated like shell tabs). All
			// currently-open tabs stay mounted (v-show, not v-if) so
			// switching never disconnects a shell or loses log scroll
			// position. Closing a tab removes it from this array, which lets
			// Vue actually unmount that one TerminalCard so its socket closes.
			terminalTabs: [{ id: 1, title: `${this.$t('Shell')} 1`, type: 'shell' }],
			activeTerminalTabId: 1,
			nextTerminalTabId: 2
		}
	},
	computed: {
		hasLogsTab() {
			return this.terminalTabs.some(t => t.id === 'logs')
		},
		shellTabs() {
			return this.terminalTabs.filter(t => t.type === 'shell')
		}
	},
	mounted() {
		this.getLogs();
		this.timer = setInterval(() => {
			this.getLogs();
		}, 1000 * 5);
	},
	methods: {
		getLogs() {
			this.$api.sys.getLogs().then(res => {
				let data = res.data.data
				let replaceData = data.replace(/\n(.{8})/gu, '\n');
				this.logData = replaceData.substring(8, replaceData.length - 1);
			})
		},
		// Any ref used lexically inside a v-for is collected into an array by
		// Vue 2, regardless of whether the ref key is unique per iteration -
		// this is decided at compile time (is the ref inside a v-for at all),
		// not at runtime by how many elements actually end up sharing a key.
		// The shell refs below (:ref="'terminal-' + tab.id") looked safe
		// because each key really is unique, but they're still wrapped in a
		// single-element array - unwrapped here so callers always get the
		// component instance directly.
		getTabRef(tab) {
			const ref = tab.type === 'logs' ? this.$refs.logs : this.$refs['terminal-' + tab.id]
			return Array.isArray(ref) ? ref[0] : ref
		},
		// A shell tab can always be closed as long as at least one other
		// shell remains open (Logs doesn't count - closing/reopening it via
		// the logs button is always allowed regardless of shell count).
		canCloseTab(tab) {
			if (tab.type === 'logs') return true
			return this.terminalTabs.filter(t => t.type === 'shell').length > 1
		},
		activateTerminalTab(id) {
			if (id === this.activeTerminalTabId) return
			const previous = this.terminalTabs.find(t => t.id === this.activeTerminalTabId)
			const previousRef = previous && this.getTabRef(previous)
			if (previousRef) previousRef.active(false)
			this.activeTerminalTabId = id
			// The newly active tab may have been sized/scrolled while hidden
			// (v-show gives it zero width), so it needs a fresh fit/scroll -
			// same resize path used when switching between shells.
			this.$nextTick(() => {
				const tab = this.terminalTabs.find(t => t.id === id)
				const ref = tab && this.getTabRef(tab)
				if (ref) ref.active(true)
			})
		},
		openNewWindow() {
			this.$store.commit('OPEN_WINDOW', {
				id: 'terminal-' + Date.now(),
				title: this.$t('Terminal'),
				component: 'TerminalPanel',
				width: 720,
				height: 480
			})
		},
		addTerminalTab() {
			const id = this.nextTerminalTabId++
			this.terminalTabs.push({ id, title: `${this.$t('Shell')} ${id}`, type: 'shell' })
			this.activateTerminalTab(id)
		},
		// The logs button opens (or just re-focuses, if already open) a
		// single Logs tab in the strip - it's a real tab like any other,
		// closed via its own tab's close (x), not toggled off by this
		// button.
		openLogsTab() {
			if (!this.hasLogsTab) {
				this.terminalTabs.push({ id: 'logs', title: this.$t('Logs'), type: 'logs' })
				this.$messageBus('terminallogs_logs')
				// Fresh content right away instead of waiting for the next
				// 5s poll tick - the Logs pane didn't even exist until now,
				// so whatever it first renders shouldn't be however stale
				// logData happened to be since the last poll.
				this.getLogs()
			}
			this.activateTerminalTab('logs')
		},
		closeTerminalTab(id) {
			const tab = this.terminalTabs.find(t => t.id === id)
			if (!tab || !this.canCloseTab(tab)) return
			const idx = this.terminalTabs.findIndex(t => t.id === id)
			this.terminalTabs.splice(idx, 1)
			if (this.activeTerminalTabId === id) {
				const fallback = this.terminalTabs[idx] || this.terminalTabs[idx - 1]
				this.activateTerminalTab(fallback.id)
			}
		},
	},
	destroyed() {
		clearInterval(this.timer);
	}
}
</script>

<style lang="scss" scoped>
.terminal-window {
	height: 100%;
	display: flex;
	flex-direction: column;
	background: #1e1e1e;
}

.terminal-body {
	flex: 1 1 auto;
	min-height: 0;
	position: relative;
}

.terminal-body-layer {
	position: absolute;
	top: 0;
	left: 0;
	right: 0;
	bottom: 0;
	overflow: hidden;

	::v-deep #logs {
		height: 100%;
		min-height: 0;
	}

	::v-deep .terminal-instance {
		height: 100%;
		min-height: 0;
	}
}

.terminal-tabs {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: 2px;
	padding: 0.35rem 0.5rem;
	background: #1e1e1e;
	overflow-x: auto;
	cursor: grab;
}

.terminal-tabs-spacer {
	flex: 1 1 auto;
	min-width: 0.5rem;
}

.window-controls {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: 0.5rem;
	margin-left: 0.75rem;
}

.window-btn {
	width: 0.85rem;
	height: 0.85rem;
	border-radius: 50%;
	border: none;
	cursor: pointer;
	padding: 0;
}

.window-btn-minimize {
	background: #f6bd3b;
}

.window-btn-close {
	background: #f2534a;
}

.terminal-tab {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	max-width: 10rem;
	border: none;
	background: rgba(255, 255, 255, 0.05);
	color: rgba(255, 255, 255, 0.55);
	font-size: 0.75rem;
	padding: 0.35rem 0.5rem;
	// Fully rounded by default (inactive tabs) - the active tab overrides
	// this to a flat bottom below, since it's meant to read as flush with
	// the content area directly beneath it, not as a separate chip.
	border-radius: 6px;
	cursor: pointer;
	flex-shrink: 0;

	&.active {
		border-radius: 6px 6px 0 0;
		// Flat #1e1e1e, matching the content area it sits flush above -
		// a gradient starting at #292929 was tried and reverted, since
		// that's actually the inactive tab's own color (rgba(255,255,255,0.05)
		// over #1e1e1e), making the selected tab's top edge indistinguishable
		// from an unselected one.
		background: #1e1e1e;
		color: #fff;
	}
}

.terminal-tab-close {
	display: flex;
	align-items: center;
	border-radius: 4px;
	padding: 1px;

	&:hover {
		background: rgba(255, 255, 255, 0.15);
	}
}

.terminal-tab-add {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: center;
	width: 1.6rem;
	height: 1.6rem;
	border: none;
	background: transparent;
	color: rgba(255, 255, 255, 0.5);
	border-radius: 6px;
	cursor: pointer;

	&:hover {
		background: rgba(255, 255, 255, 0.08);
		color: #fff;
	}

	&.active {
		background: rgba(255, 255, 255, 0.14);
		color: #fff;
	}
}

.logs-button {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: center;
	gap: 0.35rem;
	height: 1.6rem;
	padding: 0 0.55rem;
	border: none;
	background: transparent;
	color: rgba(255, 255, 255, 0.5);
	font-size: 0.72rem;
	border-radius: 6px;
	cursor: pointer;

	// Buefy's b-icon ships its own default margin, which stacked
	// asymmetrically with this button's own `gap`/`padding` - zeroed out
	// so the icon+label group is actually centered in the equal
	// left/right padding above.
	::v-deep .icon {
		margin: 0 !important;
	}

	&:hover {
		background: rgba(255, 255, 255, 0.08);
		color: #fff;
	}

	&.active {
		background: rgba(255, 255, 255, 0.14);
		color: #fff;
	}
}
</style>
