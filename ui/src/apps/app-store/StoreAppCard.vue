<!--
	One App Store catalog card. It used to be copied inline five times in
	AppStoreApp.vue, so every search keystroke, resize tick, carousel step
	and install-progress event re-rendered all 600+ cards at once (the
	store's lag). As its own component a card only re-renders when its own
	props change.
-->
<template>
	<!-- The title is the card's button, stretched over the whole card (a
	     card with role=button can't also contain the Install buttons). -->
	<article class="app-card" @click="$emit('detail', item.id)">
		<div class="card-banner">
			<img
				v-if="banner && !bannerFailed"
				:src="banner"
				class="card-banner-img"
				alt=""
				loading="lazy"
				decoding="async"
				@error="bannerFailed = true"
			/>
			<div v-else class="card-banner-placeholder" :style="gradient">
				<i :class="'mdi mdi-' + categoryIcon + ' placeholder-icon'" aria-hidden="true"></i>
			</div>
		</div>

		<div class="app-card-body">
			<div class="app-card-top">
				<img :src="iconSrc" class="app-icon" alt="" loading="lazy" decoding="async" @error="iconFailed = true" />
				<div class="app-info">
					<h4 class="app-title" :title="item.title">
						<button type="button" class="card-stretch" @click.stop="$emit('detail', item.id)">{{ item.title }}</button>
					</h4>
					<span class="app-author" :title="item.author">{{ item.author || $t('Community') }}</span>
				</div>
			</div>
			<p class="app-tagline">{{ item.tagline }}</p>
			<div class="app-card-bottom">
				<div class="app-meta-group">
					<span class="app-cat-pill">{{ item.category }}</span>
					<span v-if="!compatible" class="app-arch-text is-incompatible" :title="$t('Not built for this server\'s CPU ({arch})', { arch: arch || '?' })">{{ $t('Not for this CPU') }}</span>
				</div>
				<div class="app-card-action" @click.stop @keydown.stop>
					<button v-if="installed" type="button" class="card-btn is-open" @click="$emit('open', item)">
						{{ $t('Open') }}
					</button>
					<div v-else class="card-btn-split">
						<button
							type="button"
							class="card-btn is-install"
							:disabled="!compatible || installing !== undefined"
							:class="{ 'is-loading': installing !== undefined }"
							:title="!compatible ? $t('Not built for this server\'s CPU ({arch})', { arch: arch || '?' }) : ''"
							@click="$emit('install', item.id, item)"
						>
							<span>{{ installLabel }}</span>
						</button>
						<button
							type="button"
							class="card-btn-cog"
							:disabled="!compatible || installing !== undefined"
							:title="$t('Customize & Install')"
							:aria-label="$t('Customize & install {title}', { title: item.title })"
							@click="$emit('customize', item.id, item)"
						>
							<i class="mdi mdi-tune-variant" aria-hidden="true"></i>
						</button>
					</div>
				</div>
			</div>
		</div>
	</article>
</template>

<script>
import defaultAppIcon from '@/assets/img/app-icons/default.svg'

const GRADIENTS = [
	'linear-gradient(135deg, #1e293b 0%, #0f172a 100%)',
	'linear-gradient(135deg, #1e3a8a 0%, #0f172a 100%)',
	'linear-gradient(135deg, #064e3b 0%, #022c22 100%)',
	'linear-gradient(135deg, #3b0764 0%, #1e1b4b 100%)',
	'linear-gradient(135deg, #4c0519 0%, #1e1b4b 100%)',
	'linear-gradient(135deg, #431407 0%, #1e293b 100%)'
]

export default {
	name: 'store-app-card',
	props: {
		item: { type: Object, required: true },
		installed: { type: Boolean, default: false },
		// undefined = not installing; a number = progress in percent
		installing: { type: Number, default: undefined },
		compatible: { type: Boolean, default: true },
		arch: { type: String, default: '' },
		categoryIcon: { type: String, default: 'cube-outline' }
	},
	data() {
		return { bannerFailed: false, iconFailed: false }
	},
	computed: {
		banner() {
			return this.item.thumbnail || (this.item.screenshots && this.item.screenshots[0]) || ''
		},
		iconSrc() {
			return this.iconFailed || !this.item.icon ? defaultAppIcon : this.item.icon
		},
		gradient() {
			let hash = 0
			const s = this.item.title || 'nivaroos'
			for (let i = 0; i < s.length; i++) hash = s.charCodeAt(i) + ((hash << 5) - hash)
			return { background: GRADIENTS[Math.abs(hash) % GRADIENTS.length] }
		},
		installLabel() {
			if (this.installing === undefined) return this.$t('Install')
			return this.installing > 5 ? `${this.installing}%` : this.$t('Installing...')
		}
	}
}
</script>

<style lang="scss" scoped>
.app-card {
	position: relative;
	background: var(--theme-card-bg, #ffffff);
	border: 1px solid var(--theme-card-border, #e2e8f0);
	border-radius: var(--radius-card);
	overflow: hidden;
	display: flex;
	flex-direction: column;
	cursor: pointer;
	transition: border-color 0.15s ease;
	// Only the part on screen is rendered/painted: the catalog has 600+ cards.
	content-visibility: auto;
	contain-intrinsic-size: auto 290px;

	&:hover {
		border-color: var(--theme-input-border, #cbd5e1);
	}

	&:focus-within {
		border-color: var(--color-primary-fg);
	}
}

.card-stretch {
	all: unset;
	cursor: pointer;

	&::after {
		content: '';
		position: absolute;
		inset: 0;
		z-index: 0;
	}

	&:focus-visible::after {
		outline: 2px solid var(--color-primary-fg);
		outline-offset: -2px;
		border-radius: var(--radius-card);
	}
}

// Above the stretched title so they stay clickable.
.app-card-action {
	position: relative;
	z-index: 1;
}

.card-banner {
	position: relative;
	width: 100%;
	height: 145px;
	overflow: hidden;
	// Fixed dark backdrop: the placeholder icon is translucent white.
	background: #0f172a;
}

.card-banner-img {
	width: 100%;
	height: 100%;
	object-fit: cover;
	object-position: top;
}

.card-banner-placeholder {
	width: 100%;
	height: 100%;
	display: flex;
	align-items: center;
	justify-content: center;
}

.placeholder-icon {
	color: rgba(255, 255, 255, 0.3);
	font-size: var(--font-2xl);
}

.app-card-body {
	padding: var(--space-4);
	display: flex;
	flex-direction: column;
	flex: 1;
}

.app-card-top {
	display: flex;
	gap: var(--space-3);
	margin-bottom: var(--space-2);
}

.app-icon {
	width: 40px;
	height: 40px;
	border-radius: var(--radius-control);
	object-fit: contain;
	flex-shrink: 0;
	background: var(--theme-card-subtle, #f8fafc);
	padding: var(--space-1);
	border: 1px solid var(--theme-card-border, #f1f5f9);
}

.app-info {
	flex: 1;
	min-width: 0;
}

.app-title {
	font-size: var(--font-base);
	font-weight: 600;
	color: var(--theme-text-primary, #0f172a);
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	line-height: 1.3;
}

.app-author {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #5b6779);
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	display: block;
}

.app-tagline {
	font-size: var(--font-sm);
	color: var(--theme-text-secondary, #475569);
	line-height: 1.4;
	display: -webkit-box;
	-webkit-line-clamp: 2;
	-webkit-box-orient: vertical;
	overflow: hidden;
	margin-bottom: var(--space-3);
	flex: 1;
}

.app-card-bottom {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding-top: var(--space-2);
	border-top: 1px solid var(--theme-card-border, #f1f5f9);
	gap: var(--space-2);
}

.app-meta-group {
	display: flex;
	align-items: center;
	gap: var(--space-1);
	min-width: 0;
	flex: 1;
	overflow: hidden;
}

.app-cat-pill {
	font-size: var(--font-2xs);
	font-weight: 500;
	color: var(--theme-pill-color, #475569);
	background: var(--theme-pill-bg, #f1f5f9);
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-pill);
	white-space: nowrap;
}

.app-arch-text {
	font-size: var(--font-2xs);
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;

	&.is-incompatible {
		color: var(--color-warning-fg);
	}
}

.card-btn {
	padding: var(--space-1) var(--space-4);
	border-radius: var(--radius-pill);
	font-size: var(--font-sm);
	font-weight: 600;
	border: none;
	cursor: pointer;
	white-space: nowrap;

	&.is-install {
		background: var(--color-primary);
		color: #ffffff;

		&:hover:not(:disabled) {
			background: var(--color-primary-hover);
		}

		// Readable disabled state (white on #93c5fd was 1.8:1).
		&:disabled {
			background: var(--theme-card-subtle, #e2e8f0);
			color: var(--theme-text-secondary, #475569);
			cursor: not-allowed;
		}

		&.is-loading:disabled {
			background: var(--color-primary-soft, rgba(37, 99, 235, 0.1));
			color: var(--color-primary-fg);
		}
	}

	&.is-open {
		background: var(--color-primary-soft, rgba(37, 99, 235, 0.1));
		color: var(--color-primary-fg);
		border: 1px solid rgba(59, 130, 246, 0.35);

		&:hover {
			background: rgba(59, 130, 246, 0.16);
		}
	}

	&:focus-visible {
		outline: 2px solid var(--color-primary-fg);
		outline-offset: 2px;
	}
}

.card-btn-split {
	display: inline-flex;
	align-items: center;
	border-radius: var(--radius-pill);
	overflow: hidden;

	.card-btn.is-install {
		border-radius: 0;
		padding: var(--space-1) var(--space-3);
	}

	.card-btn-cog {
		background: var(--color-primary-hover);
		color: #ffffff;
		border: none;
		padding: var(--space-1) var(--space-2);
		cursor: pointer;
		display: flex;
		align-items: center;
		justify-content: center;
		align-self: stretch;

		i.mdi {
			font-size: var(--font-sm);
		}

		&:hover:not(:disabled) {
			background: #1e40af;
		}

		&:disabled {
			background: var(--theme-card-subtle, #e2e8f0);
			color: var(--theme-text-secondary, #475569);
			cursor: not-allowed;
		}

		&:focus-visible {
			outline: 2px solid var(--color-primary-fg);
			outline-offset: -2px;
		}
	}
}
</style>
