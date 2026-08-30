<template>
	<div id="logs" class="logs scrollbars">
		<div contenteditable v-dompurify-html="data" class="content"></div>
	</div>
</template>

<script>
export default {
	name: "logs-card",
	data() {
		return {
			state: true
		}
	},
	props: {
		data: String,
	},
	methods: {
		getTop(e) {
			let offset = e.offsetTop;
			if (e.offsetParent != null) offset += this.getTop(e.offsetParent);
			return offset;
		},
		active(state) {
			this.state = state;
			if (state) {
				this.srcollToBottom();
			}
		},
		srcollToBottom() {
			// Scoped to this component's own root (this.$el), not a global
			// document.getElementById("logs") lookup - Terminal now supports
			// multiple windows (each with its own Logs tab), so a global id
			// lookup would grab whichever Logs pane happened to exist first
			// in the whole document, regardless of which window this
			// instance belongs to.
			this.$nextTick(() => {
				const content = this.$el.querySelector('.content')
				if (content) this.$el.scrollTo(0, content.clientHeight)
			})
		}
	},
}
</script>

<style lang="scss" scoped>
.logs {
	width: 100%;
	height: 100%;
	white-space: pre-wrap;
	color: #fff;
	font-size: 13px;
	font-family: 'Monaco', 'Consolas', monospace !important;
	padding: 0.5rem 0.75rem;
	line-height: 1.5em;
	overflow-y: auto;
	overflow-x: hidden;
	box-sizing: border-box;

	>div {
		outline: none;
	}
}
</style>