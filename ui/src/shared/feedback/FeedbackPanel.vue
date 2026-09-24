<template>
	<div class="feedback-panel">
		<section class="feedback-body">
			<b-field :label="$t('Title')" label-for="feedback-title">
				<b-input id="feedback-title" v-model="postTitle" :placeholder="$t('Start with [Issue], [App Request], or [Feature Request]...')"
					maxlength="100"></b-input>
			</b-field>
			<b-field :label="$t('Description')" label-for="feedback-description">
				<b-input id="feedback-description" v-model="postBody"
					:placeholder="$t('The more details provided, the easier this feedback or issue gets addressed.')" maxlength="2000"
					type="textarea"></b-input>
			</b-field>

			<div class="feedback-sysinfo">
				<b-checkbox v-model="includeSystemInfo" :disabled="!systemInfo">
					{{ $t('Include system information') }}
				</b-checkbox>
				<p class="feedback-hint">
					{{ $t('This opens a new issue on GitHub. Everything below becomes public once you submit it there - check it first.') }}
				</p>
				<div v-if="loadingInfo" class="feedback-hint">{{ $t('Loading system information...') }}</div>
				<div v-else-if="infoError" class="feedback-error" role="alert">
					<span>{{ infoError }}</span>
					<b-button rounded size="is-small" @click="loadDebugInfo">{{ $t('Retry') }}</b-button>
				</div>
				<pre v-else-if="includeSystemInfo" class="feedback-preview" :aria-label="$t('System information that will be included')">{{ systemInfo }}</pre>
			</div>
		</section>

		<footer class="feedback-foot">
			<a class="feedback-link" rel="noopener noreferrer" :href="repoUrl + '/issues/new/choose'" target="_blank">
				{{ $t('For more feedback options, visit NivaroOS project on GitHub...') }}
			</a>
			<b-button :label="$t('Open on GitHub')" rounded type="is-primary" :disabled="!postTitle.trim()" @click="submitIssue" />
		</footer>
	</div>
</template>

<script>
import browserInfo from 'browser-info'

const REPO_URL = 'https://github.com/F-e-n-y-x/NivaroOS'
// GitHub (and browsers) reject very long URLs; keep the prefilled body well under.
const MAX_BODY_CHARS = 6000

export default {
	name: 'feedback-panel',
	data() {
		return {
			repoUrl: REPO_URL,
			postTitle: '',
			postBody: '',
			systemInfo: '',
			includeSystemInfo: true,
			loadingInfo: false,
			infoError: ''
		}
	},
	mounted() {
		this.loadDebugInfo()
	},
	methods: {
		loadDebugInfo() {
			this.loadingInfo = true
			this.infoError = ''
			this.$api.sys.getDebugInfo().then(res => {
				const raw = res && res.data && typeof res.data.data === 'string' ? res.data.data : ''
				if (!raw) throw new Error('empty')
				let browser = { name: '', fullVersion: '' }
				try { browser = browserInfo() || browser } catch (e) { /* unknown browser */ }
				this.systemInfo = raw
					.replace('$Browser$', browser.name || navigator.userAgent)
					.replace('$Version$', browser.fullVersion || '')
					.split('\n')
					.map(l => l.trim())
					.filter(Boolean)
					.join('\n')
			}).catch(() => {
				this.systemInfo = ''
				this.includeSystemInfo = false
				this.infoError = this.$t('Could not load system information. You can still send the report without it.')
			}).finally(() => {
				this.loadingInfo = false
			})
		},
		buildBody() {
			const parts = ['### Description', '', this.postBody.trim() || '_No description given._']
			if (this.includeSystemInfo && this.systemInfo) {
				parts.push('', '### System information', '', this.systemInfo)
			}
			let body = parts.join('\n')
			if (body.length > MAX_BODY_CHARS) body = body.slice(0, MAX_BODY_CHARS) + '\n\n_(truncated)_'
			return body
		},
		submitIssue() {
			const title = this.postTitle.trim()
			if (!title) return
			const url = new URL(`${REPO_URL}/issues/new`)
			url.searchParams.set('title', title.startsWith('[') ? title : `[Feedback] ${title}`)
			url.searchParams.set('body', this.buildBody())
			const w = window.open(url.toString(), '_blank', 'noopener,noreferrer')
			if (w) w.opener = null
			this.$emit('close')
		}
	}
}
</script>

<style lang="scss" scoped>
.feedback-panel {
	display: flex;
	flex-direction: column;
	height: 100%;
	background: var(--theme-bg-window-opaque, var(--theme-card-bg));
	color: var(--theme-text-primary);
}

.feedback-body {
	flex: 1 1 auto;
	overflow-y: auto;
	padding: var(--space-5);
}

.feedback-sysinfo {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.feedback-hint {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary);
}

.feedback-error {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-2);
	font-size: var(--font-xs);
	color: var(--color-danger-fg);
}

.feedback-preview {
	margin: 0;
	padding: var(--space-3);
	max-height: 12rem;
	overflow: auto;
	white-space: pre-wrap;
	word-break: break-word;
	font-size: var(--font-xs);
	background: var(--theme-card-subtle);
	color: var(--theme-text-primary);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-sm);
}

.feedback-foot {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-3);
	padding: var(--space-3) var(--space-5);
	border-top: 1px solid var(--theme-card-border);
}

.feedback-link {
	font-size: var(--font-xs);
	color: var(--color-link, var(--color-primary-fg));

	&:focus-visible {
		outline: 2px solid var(--theme-focus-ring, var(--color-primary));
		outline-offset: 2px;
	}
}
</style>
