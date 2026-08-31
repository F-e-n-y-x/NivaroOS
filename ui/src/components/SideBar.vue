<template>
	<div v-if="!isLoading" class="side-bar">
		<draggable v-model="allWidgets" tag="div" class="widgets-column" v-bind="dragOptions"
			@start="isDragging = true" @end="isDragging = false">
			<div v-for="w in allWidgets" :key="`widgets_${w.name}`" class="widget-slot">
				<component :is="w.app"></component>
			</div>
		</draggable>
	</div>
</template>

<script>
import lowerFirst from 'lodash/lowerFirst'
import camelCase from 'lodash/camelCase'
import find from 'lodash/find'
import draggable from 'vuedraggable'
import events from '@/events/events'

const widgetsComponents = require.context(
	'@/widgets',
	false,
	/.vue$/
)

const widgetsConfig = "widgets_config"

// Rough content-height estimates, used only to decide when a widget
// column has run out of vertical room and a second one is needed - not
// for actual layout/sizing (each widget still sizes itself naturally in
// CSS). Deliberately approximate rather than measured live (e.g. via
// ResizeObserver) - this is an overflow decision, not a pixel-perfect
// layout, and a widget's "show more" panel occasionally pushing a column
// a bit past this estimate is a fine trade-off for the simplicity.
const ESTIMATED_HEIGHT = {
	cpu: 210,
	ram: 200,
	gpu: 210,
	disks: 190,
	network: 230,
}
const DEFAULT_ESTIMATED_HEIGHT = 200
const GAP_PX = 16 // matches .widgets-column's flex gap (1rem)
// Just clears .home-container's own top padding (pt-4, 16px) plus a small
// buffer - there's no top bar above the widget column to otherwise account
// for.
const TOP_MARGIN_PX = 24
// Both widget columns sit flush against the right edge (see Home.vue), the
// same edge DateTimePill.vue anchors to - this has to clear the pill's own
// footprint (0.9rem bottom offset + ~2.4rem tall) plus a real gap, or the
// last widget can grow down far enough to sit behind/under it.
const BOTTOM_MARGIN_PX = 80

export default {
	name: 'side-bar',
	components: { draggable },
	data() {
		return {
			isLoading: true,
			apps: [], // [{ app, componentName }]
			positions: [], // [{ name, hidden }] - array order is display order
			availableHeight: window.innerHeight - TOP_MARGIN_PX - BOTTOM_MARGIN_PX,
			isDragging: false,
			// A drag that crosses columns fires BOTH column setters below
			// (one list loses the item, the other gains it) - staged here
			// and committed together on nextTick (see queueReorder) so the
			// merge always sees both sides' final state, never one side's
			// stale snapshot of the other mid-drag.
			pendingCol1: null,
			pendingCol2: null,
		}
	},
	computed: {
		orderedVisible() {
			return this.positions
				.filter(p => !p.hidden)
				.map(p => {
					const app = find(this.apps, o => o.app.name === p.name)
					return app ? { name: p.name, app: app.app } : null
				})
				.filter(Boolean)
		},
		// Greedily fills column 1 (the primary, right-most column) in order
		// until the next widget's estimated height would overflow
		// availableHeight, then spills the rest into column 2. `col1.length
		// === 0` guard means a single widget taller than availableHeight
		// still gets placed rather than leaving column 1 permanently empty.
		columns() {
			const available = Math.max(200, this.availableHeight)
			const col1 = []
			const col2 = []
			let used = 0
			this.orderedVisible.forEach(w => {
				const h = (ESTIMATED_HEIGHT[w.name] || DEFAULT_ESTIMATED_HEIGHT) + GAP_PX
				if (col1.length === 0 || used + h <= available) {
					col1.push(w)
					used += h
				} else {
					col2.push(w)
				}
			})
			return [col1, col2]
		},
		// v-model pair for the two draggable columns. Reordering within
		// either column reconstructs the single canonical order (column 1
		// then column 2) and persists it - `columns` then re-derives the
		// actual split from that on next render, so a reorder can
		// legitimately move a widget across the column boundary on its own
		// (e.g. dragging something to the top of column 2 can pull it back
		// into column 1 once it's early enough in the order to fit).
		column1Widgets: {
			get() { return this.columns[0] },
			set(newOrder) { this.queueReorder('col1', newOrder) }
		},
		column2Widgets: {
			get() { return this.columns[1] },
			set(newOrder) { this.queueReorder('col2', newOrder) }
		},
		allWidgets: {
			get() { return this.orderedVisible },
			set(newOrder) {
				const names = newOrder.map(w => w.name)
				this.positions = names.map(n => {
					const found = this.positions.find(p => p.name === n)
					return found || { name: n }
				})
				this.saveData()
			}
		},
		dragOptions() {
			return {
				// Same group name on both columns' draggable instances is
				// what lets Sortable.js drag an item OUT of one list and
				// drop it INTO the other (not just reorder within its own
				// column) - without this they're two independent lists.
				group: 'widgets',
				animation: 200,
				ghostClass: 'widget-ghost',
				chosenClass: 'widget-chosen',
				// Anything actually interactive inside a widget (buttons,
				// the CPU per-core toggle, GPU/RAM "more info" arrow, the
				// network interface dropdown, disk settings gear) shouldn't
				// start a drag when clicked - preventOnFilter: false lets
				// its own click still fire normally instead of being
				// swallowed by Sortable.
				filter: 'button, a, input, .widget-icon-button, .cores-toggle',
				preventOnFilter: false,
			}
		},
	},
	created() {
		widgetsComponents.keys().forEach(fileName => {
			const componentName = lowerFirst(
				camelCase(
					fileName
						.split('/')
						.pop()
						.replace(/\.\w+$/, '')
				)
			)
			const app = require(`@/widgets/${fileName.replace("./", "")}`).default
			this.apps.push({ app, componentName })
		});
	},
	mounted() {
		this.getConfig();
		this.$EventBus.$on(events.SET_WIDGET_HIDDEN, this.setWidgetHidden);
		this.onResize = () => {
			this.availableHeight = window.innerHeight - TOP_MARGIN_PX - BOTTOM_MARGIN_PX
		}
		window.addEventListener('resize', this.onResize);
	},
	beforeDestroy() {
		this.$EventBus.$off(events.SET_WIDGET_HIDDEN, this.setWidgetHidden);
		window.removeEventListener('resize', this.onResize);
	},
	methods: {
		getConfig() {
			this.$api.users.getCustomStorage(widgetsConfig).then(res => {
				if (res.status === 200) {
					const saved = res.data.data && res.data.data.length ? res.data.data : []
					this.positions = this.reconcilePositions(saved)
					this.isLoading = false;
					if (JSON.stringify(saved) !== JSON.stringify(this.positions)) {
						this.saveData();
					}
				}
			})
		},

		// Keeps saved order for widgets that still exist, normalized down to
		// {name, hidden} (dropping any leftover x/y/cols/rows from the old
		// free-roam layout's storage format), and appends any widget never
		// seen before at the end, visible by default.
		reconcilePositions(saved) {
			const knownNames = this.apps.map(a => a.app.name)
			const kept = saved
				.filter(p => knownNames.includes(p.name))
				.map(p => ({ name: p.name, hidden: !!p.hidden }))
			const keptNames = kept.map(p => p.name)
			const fresh = this.apps
				.filter(a => !keptNames.includes(a.app.name))
				.map(a => ({ name: a.app.name, hidden: false }))
			return kept.concat(fresh)
		},

		saveData() {
			this.$api.users.setCustomStorage(widgetsConfig, this.positions).then(res => {
				if (res.data.success === 200 && res.data.data) {
					this.positions = res.data.data
				}
			})
		},

		// Stages one column's new order and commits both columns together
		// on nextTick. A same-column reorder only ever calls this once, so
		// it flushes alone using the other (untouched) column straight
		// from `columns`. A cross-column drag calls this twice (the source
		// list losing the item, the destination list gaining it) - both
		// calls land in the same tick, so the first nextTick callback to
		// run sees both pending values and commits the real final state;
		// the second is a no-op since the pending slots are already clear.
		// Committing from `this.columns[...]` independently per call (the
		// previous approach) raced: each call could see the OTHER column's
		// pre-drag snapshot rather than its just-updated value, which
		// either duplicated the moved widget into both columns (breaking
		// its :key) or dropped it from both entirely.
		queueReorder(which, newOrder) {
			if (which === 'col1') this.pendingCol1 = newOrder
			else this.pendingCol2 = newOrder
			this.$nextTick(() => {
				if (this.pendingCol1 === null && this.pendingCol2 === null) return
				const col1 = this.pendingCol1 !== null ? this.pendingCol1 : this.columns[0]
				const col2 = this.pendingCol2 !== null ? this.pendingCol2 : this.columns[1]
				this.pendingCol1 = null
				this.pendingCol2 = null
				this.applyReorder(col1, col2)
			})
		},

		// Column-1-then-column-2 is the canonical visible order; hidden
		// widgets are appended after, keeping their existing relative order.
		applyReorder(col1, col2) {
			const hidden = this.positions.filter(p => p.hidden)
			const visible = col1.concat(col2).map(item => ({ name: item.name, hidden: false }))
			this.positions = visible.concat(hidden)
			this.saveData()
		},

		// Settings' widget-visibility list dispatches this over the
		// EventBus (SideBar owns `positions`/persistence, Settings is just
		// a remote control). Toggling an existing widget keeps its place in
		// the order; a widget with no record yet (never toggled or
		// reordered before) is appended at the end.
		setWidgetHidden(name, hidden) {
			const found = this.positions.some(p => p.name === name)
			this.positions = found
				? this.positions.map(p => p.name === name ? { ...p, hidden } : p)
				: this.positions.concat([{ name, hidden }])
			this.saveData()
		},
	},
}
</script>

<style lang="scss">
.side-bar {
	display: flex;
	flex-direction: row;
	align-items: flex-start;
	flex: 0 0 auto;

	@include until(480px) {
		display: none;
	}
}

.widgets-column {
	flex: 0 0 18.5rem;
	width: 18.5rem;
	display: flex;
	flex-direction: column;
	gap: 0.75rem;
	min-height: 4rem;
	height: calc(100vh - 7rem);
	overflow-y: auto;
	overflow-x: hidden;
	scrollbar-width: none;
	&::-webkit-scrollbar {
		display: none;
	}
}

.widget-slot {
	cursor: grab;
	user-select: none;
	flex-shrink: 0;

	> .widget {
		margin-bottom: 0 !important;
	}
}

.widget-chosen {
	cursor: grabbing;
}

.widget-ghost {
	opacity: 0.4;
}
</style>
