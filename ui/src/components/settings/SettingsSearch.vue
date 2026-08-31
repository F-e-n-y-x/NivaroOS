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
	padding: 0.75rem 1rem 0;
}

.search-box {
	display: flex;
	align-items: center;
	gap: 0.65rem;
	background: #f8fafc;
	border-radius: 12px;
	border: 1px solid #e2e8f0;
	padding: 0.65rem 1rem;
	transition: border-color 0.15s ease, box-shadow 0.15s ease, background 0.15s ease;

	&.is-focused {
		border-color: #2563eb;
		background: #ffffff;
		box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.12);
	}
}

.search-icon {
	flex-shrink: 0;
	color: #94a3b8;
}

.search-input {
	flex: 1;
	min-width: 0;
	border: none;
	outline: none;
	background: transparent;
	font-family: inherit;
	font-size: 0.875rem;
	font-weight: 400;
	color: #1e293b;

	&::placeholder {
		color: #94a3b8;
	}
}

.search-clear {
	flex-shrink: 0;
	width: 1.35rem;
	height: 1.35rem;
	border-radius: 50%;
	border: none;
	background: rgba(0, 0, 0, 0.05);
	color: #64748b;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;

	&:hover {
		background: rgba(0, 0, 0, 0.1);
	}
}

.search-results {
	position: absolute;
	left: 1rem;
	right: 1rem;
	top: 100%;
	margin-top: 0.5rem;
	background: #fff;
	border: 1px solid rgba(0, 0, 0, 0.06);
	border-radius: 14px;
	box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
	z-index: 5;
	overflow: hidden;
}

.search-result {
	display: flex;
	align-items: center;
	width: 100%;
	border: none;
	border-bottom: 1px solid rgba(0, 0, 0, 0.06);
	background: transparent;
	padding: 0.65rem 1rem;
	font-size: 0.85rem;
	cursor: pointer;
	text-align: left;
	transition: background-color 0.1s ease;

	&:last-child {
		border-bottom: none;
	}

	&:hover,
	&:focus-visible {
		background: rgba(0, 0, 0, 0.035);
	}
}

.result-icon {
	flex-shrink: 0;
	margin-right: 0.75rem;
	color: hsla(208, 16%, 42%, 1);
}

.result-label {
	flex: 1;
	font-weight: 600;
}

.result-section {
	flex-shrink: 0;
	margin-left: 0.75rem;
	padding: 0.15rem 0.55rem;
	border-radius: 999px;
	background: rgba(0, 0, 0, 0.045);
	color: rgba(44, 62, 80, 0.6);
	font-size: 0.7rem;
	font-weight: 600;
}
</style>
