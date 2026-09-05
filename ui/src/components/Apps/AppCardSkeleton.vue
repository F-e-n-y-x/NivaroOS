<template>
	<div class="common-card is-flex is-align-items-center is-justify-content-center app-card">
		<div class="cards-content">
			<div class="has-text-centered is-flex is-justify-content-center is-flex-direction-column icon-cell">
				<div class="is-flex is-justify-content-center">
					<div class="skeleton-icon-wrapper">
						<div class="skeleton-icon-box" :style="{ animationDelay: `${delay}s` }"></div>
					</div>
				</div>
				<div class="is-flex is-justify-content-center">
					<div class="skeleton-label" :style="{ animationDelay: `${delay + 0.06}s` }"></div>
				</div>
			</div>
		</div>
	</div>
</template>

<script>
export default {
	name: 'AppCardSkeleton',
	props: {
		index: {
			type: Number,
			default: 0
		},
		delay: {
			type: Number,
			default: 0
		}
	}
}
</script>

<style lang="scss" scoped>
.app-card {
	width: 100%;
	height: 100%;
	background: transparent;
	border-radius: 12px;
	pointer-events: none;
	user-select: none;
}

.cards-content {
	width: 100%;
	height: 100%;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
}

.icon-cell {
	width: 100%;
	height: 100%;
	padding: 6px 4px 4px;
	box-sizing: border-box;
	justify-content: center;
	gap: 0;
}

.skeleton-icon-wrapper {
	width: 52px;
	height: 52px;
	position: relative;
	display: flex;
	align-items: center;
	justify-content: center;
}

.skeleton-icon-box {
	width: 52px;
	height: 52px;
	border-radius: 12px;
	background: linear-gradient(135deg, rgba(255, 255, 255, 0.07) 0%, rgba(255, 255, 255, 0.16) 50%, rgba(255, 255, 255, 0.07) 100%);
	background-size: 250% 250%;
	border: 1px solid rgba(255, 255, 255, 0.12);
	backdrop-filter: blur(8px);
	-webkit-backdrop-filter: blur(8px);
	box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
	animation: skeleton-shimmer 2s ease-in-out infinite;
}

.skeleton-label {
	width: 52px;
	height: 9px;
	margin-top: 6px;
	border-radius: 4px;
	background: linear-gradient(135deg, rgba(255, 255, 255, 0.08) 0%, rgba(255, 255, 255, 0.18) 50%, rgba(255, 255, 255, 0.08) 100%);
	background-size: 250% 250%;
	animation: skeleton-shimmer 2s ease-in-out infinite;
}

@keyframes skeleton-shimmer {
	0% {
		background-position: 100% 100%;
		opacity: 0.65;
	}
	50% {
		background-position: 0% 0%;
		opacity: 1;
	}
	100% {
		background-position: 100% 100%;
		opacity: 0.65;
	}
}
</style>