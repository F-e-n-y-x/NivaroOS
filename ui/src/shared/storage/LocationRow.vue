<!-- src/shared/storage/LocationRow.vue -->
<!-- One location in StoragePickerWindow: a radio row with the name, the
     filesystem or provider, free space as a real meter with a text
     alternative, and a status pill that is always icon + text (spec §12.4,
     §13). Keyboard handling (roving tabindex, arrows) is the parent's. -->
<template>
	<div class="loc-row" role="radio" :aria-checked="checked ? 'true' : 'false'" :aria-disabled="disabled ? 'true' : 'false'"
		:aria-describedby="descId" :tabindex="tabindex" :class="{ 'is-checked': checked, 'is-disabled': disabled }" v-on="$listeners">
		<span class="loc-radio" aria-hidden="true"><span v-if="checked" class="loc-radio-dot"></span></span>
		<b-icon :icon="icon" custom-size="mdi-20px" class="loc-icon" aria-hidden="true"></b-icon>
		<span class="loc-main">
			<span class="loc-name one-line">{{ location.label || location.ref_id }}</span>
			<span :id="descId" class="loc-meta">
				<span v-if="kindText">{{ kindText }}</span>
				<span v-if="spaceText">{{ spaceText }}</span>
			</span>
		</span>
		<span v-if="space && space.usedRatio !== null" class="loc-meter" role="meter" :aria-valuenow="Math.round(space.usedRatio * 100)"
			aria-valuemin="0" aria-valuemax="100" :aria-valuetext="spaceText" :aria-label="$t('backup.loc.used_space')">
			<span class="loc-meter-fill" :class="{ 'is-high': space.usedRatio > 0.9 }" :style="{ width: Math.round(space.usedRatio * 100) + '%' }"></span>
		</span>
		<span v-if="status" class="loc-status" :class="'tone-' + status.tone">
			<b-icon :icon="status.icon" custom-size="mdi-16px" aria-hidden="true"></b-icon>
			<span>{{ statusText }}</span>
		</span>
	</div>
</template>

<script>
import { locationIcon, locationStatus, spaceInfo } from './locations'

let uid = 0

export default {
	name: 'LocationRow',
	props: {
		location: { type: Object, required: true },
		role: { type: String, default: 'dest' },
		checked: { type: Boolean, default: false },
		disabled: { type: Boolean, default: false },
		tabindex: { type: Number, default: -1 },
		// createFormatter() from apps/backup/format.js
		fmt: { type: Object, required: true }
	},
	data() {
		return { descId: `loc-row-${++uid}` }
	},
	computed: {
		icon() {
			return locationIcon(this.location)
		},
		status() {
			return locationStatus(this.location, this.role)
		},
		statusText() {
			const s = this.status
			if (!s) return ''
			const args = { ...s.args }
			if (args.at) args.at = this.fmt.relative(args.at)
			return this.$t(s.key, args)
		},
		space() {
			return spaceInfo(this.location)
		},
		kindText() {
			const l = this.location
			if (l.system_disk) return this.$t('backup.loc.system_disk')
			if (l.kind === 'cloud') return l.provider ? this.$t('backup.loc.cloud_provider', { provider: l.provider }) : this.$t('backup.ep.cloud')
			if (l.kind === 'smb') return this.$t('backup.ep.smb')
			return l.fstype ? String(l.fstype).replace(/^fuse\./, '').toUpperCase() : this.$t(`backup.ep.${l.kind}`)
		},
		spaceText() {
			const s = this.space
			if (!s) return this.location.kind === 'cloud' && this.location.online ? this.$t('backup.loc.space_unknown') : ''
			if (s.total) return this.$t('backup.loc.free_of', { free: this.fmt.bytes(s.free), total: this.fmt.bytes(s.total) })
			return this.$t('backup.loc.free', { free: this.fmt.bytes(s.free) })
		}
	}
}
</script>

<style lang="scss" scoped>
@import './picker-common.scss';

.loc-row {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	min-height: 3rem;
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-sm);
	border: 1px solid transparent;
	cursor: pointer;
	user-select: none;

	&:hover {
		background: var(--theme-card-hover);
	}
	&.is-checked {
		background: var(--theme-card-selected);
		border-color: var(--color-primary-fg);
	}
	&.is-disabled {
		cursor: not-allowed;
		.loc-name {
			color: var(--theme-text-secondary);
		}
	}
	&:focus-visible {
		@include picker-focus-ring;
		outline-offset: -2px;
	}

	@media (pointer: coarse) {
		min-height: 44px;
	}
}

.loc-radio {
	flex-shrink: 0;
	width: 18px;
	height: 18px;
	border-radius: 50%;
	border: 2px solid var(--theme-input-border);
	display: inline-flex;
	align-items: center;
	justify-content: center;

	.is-checked & {
		border-color: var(--color-primary-fg);
	}
}

.loc-radio-dot {
	width: 8px;
	height: 8px;
	border-radius: 50%;
	background: var(--color-primary-fg);
}

.loc-icon {
	flex-shrink: 0;
	color: var(--theme-text-secondary);
}

.loc-main {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
}

.loc-name {
	font-size: var(--font-sm);
	font-weight: 600;
	color: var(--theme-text-primary);
}

.loc-meta {
	display: flex;
	flex-wrap: wrap;
	gap: 0 var(--space-2);
	font-size: var(--font-xs);
	color: var(--theme-text-muted);

	> span + span::before {
		content: '·';
		margin-right: var(--space-2);
	}
}

.loc-meter {
	flex-shrink: 0;
	width: 72px;
	height: 6px;
	border-radius: var(--radius-pill);
	background: var(--theme-pill-bg);
	border: 1px solid var(--theme-card-border);
	overflow: hidden;

	@media (max-width: 480px) {
		display: none;
	}
}

.loc-meter-fill {
	display: block;
	height: 100%;
	background: var(--color-primary-fg);

	&.is-high {
		background: var(--color-warning-fg);
	}
}

.loc-status {
	@include picker-pill;
}
</style>
