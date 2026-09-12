<template>
	<div :class="[{ active: state }]" class="ficon is-flex is-align-items-center" @click="activeSelf" @dblclick="expandDir">
		<div class="cover">
			<div :class="item | coverType">
				<img :class="item | iconType" :src="getIconFile(item)" alt="folder" />
			</div>
		</div>
		<div class="one-line">
			{{ name }}
		</div>

	</div>
</template>

<script>
import { mixin } from '@/mixins/mixin';

export default {
	name: "list-item",
	mixins: [mixin],
	data() {
		return {
			isActive: this.state
		}
	},
	props: {
		item: {},
		name: String,
		path: String,
		state: Boolean,
		IsDir: {
			type: Boolean,
			default: true
		}
	},
	computed: {
		icon() {
			if (this.IsDir) {
				return "folder"
			} else {
				return "file"
			}
		}
	},
	methods: {
		activeSelf() {
			this.isActive = true;
			this.$emit("active", this.path)
		},
		expandDir() {
			if (this.IsDir) {
				this.$emit("expand", this.path)
			}
		}
	},

}
</script>

<style lang="scss" scoped>
.ficon {
	padding: var(--space-2) var(--space-2) var(--space-2) var(--space-3);
	-webkit-touch-callout: none;
	user-select: none;
	font-size: var(--font-base);
	line-height: 1.5em;
	border-bottom: 1px solid var(--theme-card-border, #e4e4e4);
	border-radius: var(--radius-xs);
	transition: background-color 0.2s;
	cursor: pointer;

	.cover {
		width: 1.5rem;
		height: 1.5rem;
		margin-right: var(--space-2);
	}

	&:hover {
		background-color: var(--theme-card-border, #e0e0e0);
	}

	&.active {
		background-color: rgba(59, 130, 246, 0.15);
	}
}
</style>