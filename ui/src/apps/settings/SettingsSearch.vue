<template>
	<div class="settings-search">
		<div class="search-box" :class="{ 'is-focused': focused }">
			<b-icon class="search-icon" icon="search-outline" pack="casa" size="is-20"></b-icon>
			<input
				v-model="query"
				type="text"
				class="search-input"
				:placeholder="$t('Search settings')"
				@focus="focused = true"
				@blur="focused = false"
			/>
			<button v-if="query" class="search-clear" type="button" @mousedown.prevent="query = ''">
				<b-icon icon="close-outline" pack="casa" size="is-16"></b-icon>
			</button>
		</div>
		<div v-if="results.length" class="search-results">
			<button v-for="r in results" :key="r.sectionId + r.label" class="search-result" @click="jump(r)">
				<b-icon class="result-icon" :icon="r.sectionIcon" :pack="r.sectionPack || 'casa'" size="is-20"></b-icon>
				<span class="result-label">{{ $t(r.label) }}</span>
				<span class="result-section">{{ $t(r.sectionLabel) }}</span>
			</button>
		</div>
	</div>
</template>

<script>
import { filterRows } from '@/utils/settingsSearch'

export default {
	name: 'settings-search',
	props: {
		rows: { type: Array, required: true }
	},
	data() {
		return { query: '', focused: false }
	},
	computed: {
		results() {
			return filterRows(this.rows, this.query).slice(0, 8)
		}
	},
	methods: {
		jump(result) {
			this.$emit('jump', result.sectionId)
			this.query = ''
		}
	}
}
</script>

<style lang="scss" scoped>
.settings-search {
	position: relative;
	padding: var(--space-3) var(--space-4) 0;
}

.search-box {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	background: var(--theme-input-bg, #f8fafc);
	border-radius: var(--radius-card);
	border: 1px solid var(--theme-card-border, #e2e8f0);
	padding: var(--space-3) var(--space-4);
	transition: border-color 0.15s ease, box-shadow 0.15s ease, background 0.15s ease;

	&.is-focused {
		border-color: var(--color-primary, #2563eb);
		background: var(--theme-card-bg, #ffffff);
		box-shadow: var(--shadow-sm);
	}
}

.search-icon {
	flex-shrink: 0;
	color: var(--theme-text-muted, #94a3b8);
}

.search-input {
	flex: 1;
	min-width: 0;
	border: none;
	outline: none;
	background: transparent;
	font-family: inherit;
	font-size: var(--font-base);
	font-weight: 400;
	color: var(--theme-text-primary, #1e293b);

	&::placeholder {
		color: var(--theme-text-muted, #94a3b8);
	}
}

.search-clear {
	flex-shrink: 0;
	width: 1.35rem;
	height: 1.35rem;
	border-radius: 50%;
	border: none;
	background: var(--color-border, rgba(0, 0, 0, 0.05));
	color: var(--theme-text-muted, #64748b);
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;

	&:hover {
		background: var(--color-border-strong, rgba(0, 0, 0, 0.1));
	}
}

.search-results {
	position: absolute;
	left: 1rem;
	right: 1rem;
	top: 100%;
	margin-top: var(--space-2);
	background: var(--theme-card-bg, #fff);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.06));
	border-radius: var(--radius-modal);
	box-shadow: var(--shadow-md);
	z-index: 5;
	overflow: hidden;
}

.search-result {
	display: flex;
	align-items: center;
	width: 100%;
	border: none;
	border-bottom: 1px solid var(--theme-table-divider, rgba(0, 0, 0, 0.06));
	background: transparent;
	padding: var(--space-3) var(--space-4);
	font-size: var(--font-base);
	cursor: pointer;
	text-align: left;
	transition: background-color 0.1s ease;

	&:last-child {
		border-bottom: none;
	}

	&:hover,
	&:focus-visible {
		background: var(--theme-table-row-hover, rgba(0, 0, 0, 0.035));
	}
}

.result-icon {
	flex-shrink: 0;
	margin-right: var(--space-3);
	color: var(--color-text-muted, hsla(208, 16%, 42%, 1));
}

.result-label {
	flex: 1;
	font-weight: 400;
}

.result-section {
	flex-shrink: 0;
	margin-left: var(--space-3);
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-pill);
	background: var(--theme-pill-bg, rgba(0, 0, 0, 0.045));
	color: var(--theme-pill-color, rgba(44, 62, 80, 0.6));
	font-size: var(--font-2xs);
	font-weight: 400;
}
</style>
