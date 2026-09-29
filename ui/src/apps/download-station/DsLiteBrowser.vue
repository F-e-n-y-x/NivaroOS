<!-- src/apps/download-station/DsBrowser.vue -->
<!-- The lite browser: tabs + address bar over sandboxed iframes, each page
     served through the download sidecar's rewriting proxy (see
     services/download-sidecar/browser.go for why a proxy is needed at all).
     Clicking a real file link in a page never downloads in the browser -
     the proxy recognises it and hands it to Download Station instead. -->
<template>
	<div class="ds-browser">
		<div class="tab-strip">
			<div v-for="t in tabs" :key="t.id" class="tab" :class="{ active: t.id === activeId }" :title="t.title || t.url" @click="activeId = t.id" @mousedown.middle.prevent="closeTab(t.id)">
				<b-icon v-if="t.loading" icon="loading" custom-class="mdi-spin" custom-size="mdi-14px"></b-icon>
				<b-icon v-else :icon="t.kind === 'history' ? 'history' : 'web'" custom-size="mdi-14px"></b-icon>
				<span class="tab-title">{{ t.kind === 'history' ? $t('History') : t.title || hostOf(t.url) || $t('New Tab') }}</span>
				<button class="tab-close" :title="$t('Close tab')" :aria-label="$t('Close tab')" @click.stop="closeTab(t.id)">
					<b-icon icon="close" custom-size="mdi-14px"></b-icon>
				</button>
			</div>
			<button class="tab-new" :title="$t('New tab')" :aria-label="$t('New tab')" @click="newTab()">
				<b-icon icon="plus" custom-size="mdi-16px"></b-icon>
			</button>
		</div>

		<div class="address-bar">
			<button class="ds-icon-btn is-flat" :title="$t('Back')" :aria-label="$t('Back')" :disabled="!canBack" @click="historyGo(active, 'back')">
				<b-icon icon="arrow-left" custom-size="mdi-18px"></b-icon>
			</button>
			<button class="ds-icon-btn is-flat" :title="$t('Forward')" :aria-label="$t('Forward')" :disabled="!canForward" @click="historyGo(active, 'forward')">
				<b-icon icon="arrow-right" custom-size="mdi-18px"></b-icon>
			</button>
			<button class="ds-icon-btn is-flat" :title="$t('Reload')" :aria-label="$t('Reload')" :disabled="!active || !active.url" @click="reload">
				<b-icon icon="refresh" custom-size="mdi-18px"></b-icon>
			</button>
			<button class="ds-icon-btn is-flat" :title="$t('Home')" :aria-label="$t('Home')" @click="goHome">
				<b-icon icon="home-outline" custom-size="mdi-18px"></b-icon>
			</button>
			<button class="ds-icon-btn is-flat" :class="{ 'is-active': active && active.kind === 'history' }" :title="$t('History')" :aria-label="$t('History')" @click="openHistory">
				<b-icon icon="history" custom-size="mdi-18px"></b-icon>
			</button>
			<form class="url-form" @submit.prevent="submitAddress">
				<b-icon :icon="isHttps ? 'lock-outline' : 'web'" custom-size="mdi-16px" class="url-icon" :class="{ secure: isHttps }"></b-icon>
				<input ref="address" v-model="addressText" class="url-input" spellcheck="false" autocomplete="off"
					:aria-label="$t('Address bar')" :placeholder="$t('Search or enter address')" @focus="$event.target.select()" />
			</form>
			<button class="ds-icon-btn shield-btn" :class="{ 'is-active': adblockActive }" :aria-pressed="adblockActive ? 'true' : 'false'"
				:title="adblockActive ? $t('Ad blocker is on - click to turn off') : $t('Ad blocker is off - click to turn on')"
				:aria-label="adblockActive ? $t('Ad blocker is on - click to turn off') : $t('Ad blocker is off - click to turn on')" @click="$emit('toggle-adblock')">
				<b-icon :icon="adblockActive ? 'shield-check-outline' : 'shield-off-outline'" custom-size="mdi-18px"></b-icon>
				<span v-if="adblockActive && blockedHere" class="shield-count">{{ blockedHere > 99 ? '99+' : blockedHere }}</span>
			</button>
			<button class="ds-icon-btn" :title="$t('Download this address with Download Station')" :aria-label="$t('Download this address with Download Station')" :disabled="!active || !active.url" @click="downloadCurrent">
				<b-icon icon="download" custom-size="mdi-18px"></b-icon>
			</button>
			<b-dropdown position="is-bottom-left" append-to-body aria-role="menu" class="browser-menu">
				<template #trigger>
					<button class="ds-icon-btn" :title="$t('More')" :aria-label="$t('More')">
						<b-icon icon="dots-vertical" custom-size="mdi-18px"></b-icon>
					</button>
				</template>
				<b-dropdown-item aria-role="menuitem" @click="openHistory">
					<b-icon icon="history" custom-size="mdi-16px"></b-icon> {{ $t('History') }}
				</b-dropdown-item>
				<b-dropdown-item aria-role="menuitem" :disabled="!active || !active.url" @click="openInRealTab">
					<b-icon icon="open-in-new" custom-size="mdi-16px"></b-icon> {{ $t('Open page in a real browser tab') }}
				</b-dropdown-item>
				<b-dropdown-item aria-role="menuitem" :disabled="!active || !active.url" @click="copyAddress">
					<b-icon icon="content-copy" custom-size="mdi-16px"></b-icon> {{ $t('Copy address') }}
				</b-dropdown-item>
				<b-dropdown-item aria-role="menuitem" @click="clearCookies">
					<b-icon icon="cookie-remove-outline" custom-size="mdi-16px"></b-icon> {{ $t('Clear cookies & sign out of sites') }}
				</b-dropdown-item>
			</b-dropdown>
		</div>

		<div class="frame-area">
			<div v-if="!available" class="frame-error" role="status">
				<b-icon icon="lock-alert-outline" custom-size="mdi-36px"></b-icon>
				<p class="start-title">{{ $t('The built-in browser isn\'t available over HTTPS') }}</p>
				<p>{{ $t('Its pages are served from Download Station\'s own port, which browsers block inside an HTTPS page. Open NivaroOS over http:// on your local network to use the browser - downloads still work here.') }}</p>
			</div>
			<div v-else-if="error" class="frame-error" role="alert">
				<b-icon icon="alert-circle-outline" custom-size="mdi-36px"></b-icon>
				<p>{{ error }}</p>
				<button class="ds-primary-btn" @click="startSession">{{ $t('Retry') }}</button>
			</div>
			<template v-else>
				<template v-for="t in tabs">
					<ds-browser-history v-if="t.kind === 'history'" v-show="t.id === activeId" :key="t.id + '-history'"
						@open="(url, newTab) => openFromHistory(t, url, newTab)"></ds-browser-history>
					<iframe v-else-if="t.src" v-show="t.id === activeId" :key="t.id + '-' + t.gen" :ref="'frame-' + t.id" class="page-frame" :src="t.src"
						:title="t.title || hostOf(t.url) || $t('Web page')"
						sandbox="allow-scripts allow-forms allow-same-origin allow-modals allow-pointer-lock allow-presentation"
						referrerpolicy="same-origin" allow="fullscreen; clipboard-write; autoplay" @load="onFrameLoad(t)"></iframe>
					<div v-else-if="t.id === activeId" :key="t.id + '-start'" class="start-page">
						<b-icon icon="web" custom-size="mdi-48px"></b-icon>
						<p class="start-title">{{ $t('Lite Browser') }}</p>
						<p class="start-hint">{{ $t('Open a download page and click its download link - the file goes straight to Download Station, with the page\'s cookies, using multiple connections.') }}</p>
						<form class="start-form" @submit.prevent="submitStart">
							<input v-model="startText" class="ds-input" :aria-label="$t('Search or enter address')" :placeholder="$t('Search or enter address')" />
							<button class="ds-primary-btn" type="submit">{{ $t('Go') }}</button>
						</form>
					</div>
				</template>
			</template>
		</div>
	</div>
</template>

<script>
import { downloadSidecar } from '@/api/downloadSidecar'
import { escapeHtml } from '@/utils/escapeHtml'
import DsBrowserHistory from './DsBrowserHistory.vue'
import { createHistory, recordVisit, canGo, go } from './tabHistory'

let tabSeq = 0
const STATS_POLL_MS = 2500

export default {
	name: 'ds-browser',
	components: { DsBrowserHistory },
	inject: { ds: 'downloadStation' },
	props: {
		visible: { type: Boolean, default: false },
		adblockEnabled: { type: Boolean, default: false }
	},
	data() {
		return {
			session: null,
			tabs: [],
			activeId: null,
			addressText: '',
			startText: '',
			error: '',
			stats: null,
			homePage: 'https://duckduckgo.com/',
			statsTimer: null,
			available: downloadSidecar.browserAvailable(),
			destroyed: false
		}
	},
	computed: {
		canBack() {
			return !!(this.active && this.active.kind !== 'history' && canGo(this.active.hist, 'back'))
		},
		canForward() {
			return !!(this.active && this.active.kind !== 'history' && canGo(this.active.hist, 'forward'))
		},
		active() {
			return this.tabs.find(t => t.id === this.activeId) || null
		},
		isHttps() {
			return !!(this.active && this.active.url && this.active.url.startsWith('https:'))
		},
		adblockActive() {
			return this.adblockEnabled
		},
		blockedHere() {
			if (!this.stats || !this.active || !this.active.url) return 0
			const host = this.hostOf(this.active.url)
			return (this.stats.blocked_by_host || {})[host] || 0
		}
	},
	watch: {
		activeId() {
			this.addressText = this.active && this.active.kind !== 'history' ? this.active.url : ''
		}
	},
	async created() {
		this.newTab('', false)
		if (!this.available) return
		window.addEventListener('message', this.onMessage)
		await this.startSession()
		if (this.destroyed) return
		try {
			const s = await downloadSidecar.getSettings()
			if (s && s.home_page) this.homePage = s.home_page
		} catch (e) {}
		if (this.destroyed) return
		this.statsTimer = setInterval(this.pollStats, STATS_POLL_MS)
	},
	beforeDestroy() {
		// created() is async: anything it's still awaiting checks this flag,
		// so a window closed mid-start leaks neither the stats timer nor a
		// browser session on the sidecar.
		this.destroyed = true
		window.removeEventListener('message', this.onMessage)
		clearInterval(this.statsTimer)
		if (this.session) downloadSidecar.deleteBrowserSession(this.session.id).catch(() => {})
	},
	methods: {
		hostOf(u) {
			try {
				return new URL(u).hostname
			} catch (e) {
				return ''
			}
		},
		async startSession() {
			if (!this.available) return
			this.error = ''
			try {
				const session = await downloadSidecar.createBrowserSession()
				if (this.destroyed) {
					downloadSidecar.deleteBrowserSession(session.id).catch(() => {})
					return
				}
				this.session = session
				// Re-point any open tabs at the new session (their addresses
				// were ones the user already chose).
				for (const t of this.tabs) if (t.url) this.load(t, t.url, true)
			} catch (e) {
				if (!this.destroyed) this.error = this.$t('Could not start the browser: {error}', { error: e.message })
			}
		},
		async pollStats() {
			if (!this.visible || !this.session || this.destroyed) return
			try {
				this.stats = await downloadSidecar.getBrowserSession(this.session.id)
			} catch (e) {
				if (!this.destroyed && e.status === 404) this.startSession()
			}
		},
		newTab(url = '', focus = true) {
			const t = { id: ++tabSeq, kind: '', url: '', title: '', src: '', loading: false, gen: 0, recorded: '', navSeq: 0, navCache: {}, hist: createHistory() }
			this.tabs.push(t)
			this.activeId = t.id
			if (url) this.load(t, url, true)
			else if (focus) this.$nextTick(() => this.$refs.address && this.$refs.address.focus())
			return t
		},
		closeTab(id) {
			const i = this.tabs.findIndex(t => t.id === id)
			if (i < 0) return
			this.tabs.splice(i, 1)
			if (!this.tabs.length) this.newTab('', false)
			if (this.activeId === id) this.activeId = this.tabs[Math.min(i, this.tabs.length - 1)].id
		},
		// Public: used by the app shell to open a URL (e.g. "Open Browser").
		openUrl(url, inNewTab) {
			if (!url) {
				if (!this.active || this.active.url) this.newTab()
				return
			}
			const t = inNewTab && this.active && this.active.url ? this.newTab() : this.active || this.newTab()
			this.load(t, url, true)
		},
		normalize(text) {
			const s = (text || '').trim()
			if (!s) return ''
			if (/^https?:\/\//i.test(s)) return s
			// "example.com/path" or "192.168.1.1:8080" - an address, not a search.
			if (!/\s/.test(s) && (/^[\w-]+(\.[\w-]+)+(:\d+)?(\/.*)?$/.test(s) || /^localhost(:\d+)?/.test(s))) return 'https://' + s
			return 'https://html.duckduckgo.com/html/?q=' + encodeURIComponent(s)
		},
		// typed: the user chose this address (address bar, start page,
		// history, home, "Open Browser") - the sidecar then lets the proxy
		// reach its host even if it's on the local network. Addresses a page
		// navigates to by itself never get that.
		async load(tab, rawUrl, typed = false) {
			const url = this.normalize(rawUrl)
			if (!url || !this.session) {
				tab.url = url
				return
			}
			tab.kind = ''
			tab.url = url
			tab.title = ''
			tab.loading = true
			tab.navSeq++
			if (tab.id === this.activeId) this.addressText = url
			const session = this.session
			if (typed) await downloadSidecar.markTyped(session.id, url).catch(() => {})
			if (this.destroyed || session !== this.session || tab.url !== url) return
			try {
				tab.src = downloadSidecar.proxyUrl(session.prefix, url)
			} catch (e) {
				tab.loading = false
				return
			}
			// A fresh key forces a new iframe even when src is unchanged.
			tab.gen++
		},
		submitAddress() {
			if (!this.active) this.newTab()
			this.load(this.active, this.addressText, true)
			if (this.$refs.address) this.$refs.address.blur()
		},
		submitStart() {
			this.load(this.active, this.startText, true)
			this.startText = ''
		},
		// Opens History in its own tab (reusing one already open).
		openHistory() {
			const existing = this.tabs.find(t => t.kind === 'history')
			if (existing) {
				this.activeId = existing.id
				return
			}
			const blank = this.active && !this.active.url && !this.active.kind ? this.active : null
			const t = blank || this.newTab('', false)
			t.kind = 'history'
			t.src = ''
			this.activeId = t.id
			this.addressText = ''
		},
		openFromHistory(tab, url, inNewTab) {
			if (inNewTab) {
				const t = this.newTab()
				this.load(t, url, true)
			} else {
				this.load(tab, url, true)
			}
		},
		// One history entry per page visit; a later title update for the
		// same URL refreshes that entry (the sidecar dedupes it).
		recordHistory(tab) {
			const key = tab.url + '\n' + tab.title
			if (!tab.url || tab.recorded === key) return
			tab.recorded = key
			downloadSidecar.addHistory(tab.url, tab.title).catch(() => {})
		},
		goHome() {
			this.load(this.active || this.newTab(), this.homePage, true)
		},
		frameOf(tab) {
			const ref = this.$refs['frame-' + tab.id]
			return Array.isArray(ref) ? ref[0] : ref
		},
		// Back/Forward use the tab's own history (see tabHistory.js) and load
		// the page, never the frame's history.back() - that walked the whole
		// browser tab's history and took NivaroOS itself back.
		historyGo(tab, dir) {
			if (!tab || tab.kind === 'history') return
			const url = go(tab.hist, dir)
			// Every page in the list was really served to this tab, so it may
			// be reached again the way it was the first time.
			if (url) this.load(tab, url, true)
		},
		reload() {
			// Re-load through the address we know, so a page that failed to
			// load (no shim to receive a 'reload' message) still recovers.
			if (this.active && this.active.url) this.load(this.active, this.active.url, true)
		},
		onFrameLoad(tab) {
			tab.loading = false
		},
		onMessage(e) {
			if (e.origin !== downloadSidecar.origin || !e.data || e.data.nvds !== 1) return
			if (e.data.type === 'nav') {
				// Only the tab's own top proxied frame may report, and even
				// then its URL isn't taken on trust (the page's scripts can
				// post anything) - see verifiedNavUrl.
				const tab = this.tabs.find(t => {
					const f = this.frameOf(t)
					return f && f.contentWindow === e.source
				})
				if (tab) this.onNav(tab, e.data)
			} else if (e.data.type === 'history' && (e.data.dir === 'back' || e.data.dir === 'forward')) {
				// Mouse back/forward buttons or Alt+Left/Right inside the page.
				const tab = this.tabs.find(t => {
					const f = this.frameOf(t)
					return f && f.contentWindow === e.source
				})
				if (tab) this.historyGo(tab, e.data.dir)
			} else if (e.data.type === 'download' && this.session && e.data.session === this.session.id) {
				this.ds.openAddDownload({ captureSession: e.data.session, captureId: e.data.capture })
			}
		},
		async onNav(tab, data) {
			if (!this.session || typeof data.nav !== 'string') return
			const seq = ++tab.navSeq
			const shown = await this.verifiedNavUrl(tab, data.nav, data.url)
			if (!shown || this.destroyed || seq !== tab.navSeq) return
			tab.url = shown
			recordVisit(tab.hist, shown)
			tab.title = typeof data.title === 'string' ? data.title.slice(0, 300) : ''
			this.recordHistory(tab)
			if (tab.id === this.activeId && document.activeElement !== this.$refs.address) this.addressText = tab.url
		},
		// The address bar shows the document the proxy really served for
		// this nav ID. A same-origin URL from the page is accepted on top of
		// that (pushState/hash changes - which a real browser also lets a
		// page make); anything pointing at another site is ignored.
		async verifiedNavUrl(tab, nav, claimed) {
			let served = tab.navCache[nav]
			if (!served) {
				try {
					const res = await downloadSidecar.getNav(this.session.id, nav)
					served = res && res.url
				} catch (e) {
					return ''
				}
				if (!served) return ''
				tab.navCache = { [nav]: served }
			}
			try {
				const a = new URL(claimed)
				const b = new URL(served)
				if (a.origin === b.origin) return a.href
			} catch (e) {}
			return served
		},
		// Sends the page's own address to Download Station, with this
		// session's cookies for it (e.g. a direct media URL you navigated to).
		async downloadCurrent() {
			if (!this.active || !this.active.url || !this.session) return
			try {
				const c = await downloadSidecar.captureUrl(this.session.id, this.active.url, '')
				this.ds.openAddDownload({ captureSession: this.session.id, captureId: c.id })
			} catch (e) {
				this.ds.openAddDownload({ url: this.active.url })
			}
		},
		openInRealTab() {
			if (this.active && this.active.url) window.open(this.active.url, '_blank', 'noopener')
		},
		async copyAddress() {
			if (!this.active || !this.active.url) return
			try {
				await navigator.clipboard.writeText(this.active.url)
				this.$buefy.toast.open({ message: this.$t('Address copied'), type: 'is-success' })
			} catch (e) {
				this.$buefy.toast.open({ message: this.$t('Could not copy to the clipboard'), type: 'is-danger' })
			}
		},
		async clearCookies() {
			if (!this.session) return
			try {
				await downloadSidecar.clearBrowserCookies(this.session.id)
				this.$buefy.toast.open({ message: this.$t('Cookies cleared'), type: 'is-success' })
				this.reload()
			} catch (e) {
				this.$buefy.toast.open({ message: escapeHtml(this.$t('Could not clear cookies: {error}', { error: e.message })), type: 'is-danger' })
			}
		}
	}
}
</script>

<style lang="scss" scoped>
@import './ds-common.scss';

.ds-browser {
	display: flex;
	flex-direction: column;
	height: 100%;
	min-height: 0;
	background: var(--theme-card-bg, #fff);
}

.tab-strip {
	flex-shrink: 0;
	display: flex;
	align-items: flex-end;
	gap: 2px;
	padding: var(--space-2) var(--space-2) 0;
	background: var(--theme-card-subtle, #f1f5f9);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	overflow-x: auto;
	scrollbar-width: none;
	user-select: none;
}

.tab {
	flex: 0 1 12rem;
	min-width: 5.5rem;
	display: flex;
	align-items: center;
	gap: var(--space-1);
	height: 2rem;
	padding: 0 var(--space-1) 0 var(--space-3);
	border-radius: var(--radius-sm) var(--radius-sm) 0 0;
	font-size: var(--font-xs);
	color: var(--theme-text-secondary, #475569);
	cursor: pointer;

	&:hover {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.04));
	}

	&.active {
		background: var(--theme-card-bg, #fff);
		color: var(--theme-text-primary, #1e293b);
		box-shadow: 0 -1px 0 var(--theme-card-border, rgba(0, 0, 0, 0.08)), 1px 0 0 var(--theme-card-border, rgba(0, 0, 0, 0.08)), -1px 0 0 var(--theme-card-border, rgba(0, 0, 0, 0.08));
	}

	::v-deep .icon {
		flex-shrink: 0;
		width: 1rem;
		height: 1rem;
		color: var(--theme-text-muted, #94a3b8);
	}
}

.tab-title {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.tab-close {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 1.25rem;
	height: 1.25rem;
	border: none;
	background: transparent;
	border-radius: var(--radius-xs);
	cursor: pointer;
	padding: 0;
	opacity: 0.6;

	&:hover {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.08));
		opacity: 1;
	}
}

.tab-new {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 1.75rem;
	height: 1.75rem;
	margin: 0 0 0.15rem var(--space-1);
	border: none;
	background: transparent;
	border-radius: var(--radius-sm);
	color: var(--theme-text-secondary, #475569);
	cursor: pointer;

	&:hover {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.06));
	}
}

.address-bar {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: 2px;
	padding: var(--space-2);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	background: var(--theme-card-bg, #fff);
}

.url-form {
	flex: 1 1 auto;
	min-width: 6rem;
	position: relative;
	margin: 0 var(--space-2);
}

.url-icon {
	position: absolute;
	left: 0.65rem;
	top: 50%;
	transform: translateY(-50%);
	color: var(--theme-text-muted, #94a3b8);
	pointer-events: none;

	&.secure {
		color: var(--color-success-fg, #047857);
	}
}

.url-input {
	width: 100%;
	height: 2rem;
	padding: 0 var(--space-3) 0 2rem;
	border: 1px solid transparent;
	border-radius: var(--radius-pill);
	background: var(--theme-card-subtle, #f1f5f9);
	color: var(--theme-text-primary, #1e293b);
	font-family: inherit;
	font-size: var(--font-sm);
	outline: none;

	&:focus {
		background: var(--theme-input-bg, #fff);
		border-color: var(--theme-input-focus, #2563eb);
		box-shadow: 0 0 0 3px var(--theme-focus-ring, rgba(37, 99, 235, 0.4));
	}
}

.shield-btn {
	position: relative;
	width: auto;
	min-width: 1.85rem;
	padding: 0 0.35rem;
	gap: 0.15rem;
}

.shield-count {
	font-size: var(--font-2xs);
	font-weight: 600;
	font-variant-numeric: tabular-nums;
}

.frame-area {
	position: relative;
	flex: 1 1 auto;
	min-height: 0;
	background: var(--theme-card-bg, #fff);
}

// No fixed white: a page that doesn't paint its own background shows the
// theme surface, and the proxy's own pages (captured, blocked, errors) pick
// up the NivaroOS theme through color-scheme (see the unscoped block below).
.page-frame {
	position: absolute;
	inset: 0;
	width: 100%;
	height: 100%;
	border: none;
	background: transparent;
	color-scheme: light;
}

.start-page,
.frame-error {
	position: absolute;
	inset: 0;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	padding: var(--space-6);
	text-align: center;
	background: var(--theme-bg-window, #f8fafc);
	color: var(--theme-text-muted, #94a3b8);

	> ::v-deep .icon {
		width: 3rem;
		height: 3rem;
	}

	p {
		max-width: 30rem;
		font-size: var(--font-sm);
		color: var(--theme-text-secondary, #64748b);
		margin: var(--space-2) 0 var(--space-4);
		line-height: 1.5;
	}
}

.start-title {
	font-size: var(--font-md) !important;
	font-weight: 600;
	color: var(--theme-text-primary, #1e293b) !important;
	margin-bottom: 0 !important;
}

.start-form {
	display: flex;
	gap: var(--space-2);
	width: 100%;
	max-width: 30rem;

	.ds-primary-btn {
		height: 2.1rem;
	}
}

.browser-menu ::v-deep .dropdown-item {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	font-size: var(--font-sm);
}
</style>

<style lang="scss">
// Unscoped: the iframe's used color-scheme is what prefers-color-scheme
// reports inside the proxied document, so the sidecar's own pages follow the
// NivaroOS theme rather than the OS setting.
html[data-theme='dark'] .ds-browser .page-frame,
html.is-dark .ds-browser .page-frame {
	color-scheme: dark;
}
</style>
