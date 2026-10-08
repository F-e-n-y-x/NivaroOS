<!-- src/apps/download-station/rb/DsBrowserViewport.vue -->
<!-- The page area of the Download Station browser: draws the pictures the
     server's Chromium sends onto a canvas, and sends mouse, wheel, touch and
     keyboard input back. A hidden textarea holds keyboard focus so IME and
     phone keyboards work. No page code ever runs here. -->
<template>
	<div ref="root" class="rb-viewport" :class="{ 'is-focused': focused }">
		<canvas ref="canvas" class="rb-canvas" :style="{ cursor }" :aria-label="label" role="img"
			@mousedown="onMouseDown" @mousemove="onMouseMove" @mouseleave="onMouseLeave" @dblclick.prevent
			@contextmenu.prevent="onContextMenu" @auxclick.prevent @dragstart.prevent
			@touchstart="onTouch('start', $event)" @touchmove="onTouch('move', $event)"
			@touchend="onTouch('end', $event)" @touchcancel="onTouch('cancel', $event)"></canvas>
		<textarea ref="input" class="rb-input" :style="inputStyle" aria-hidden="true" tabindex="-1"
			autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false"
			@keydown="onKeyDown" @keyup="onKeyUp" @compositionstart="composing = true" @compositionupdate="onCompositionUpdate"
			@compositionend="onCompositionEnd" @input="onInput" @paste="onPaste" @copy.prevent @cut.prevent
			@focus="focused = true" @blur="focused = false"></textarea>
		<slot></slot>
	</div>
</template>

<script>
import { InputCoalescer, keyMessage, macChordToLinux, modsOf, shortcutOf, wheelDelta } from './rbClient'
import { isMacPlatform } from '../../terminal/termKeys'

const LONG_PRESS_MS = 550

export default {
	emits: ['contextmenu', 'frame', 'resize', 'shortcut'],
	name: 'ds-browser-viewport',
	props: {
		// send(msg) - delivers a message to the browser (false when offline).
		send: { type: Function, required: true },
		tab: { type: Number, default: 0 },
		label: { type: String, default: '' },
		// Page input is ignored (a dialog or menu is open over the page).
		inert: { type: Boolean, default: false }
	},
	data() {
		return {
			cursor: 'default',
			focused: false,
			composing: false,
			inputStyle: { left: '0px', top: '0px' }
		}
	},
	watch: {
		tab() {
			// A new tab starts from its own first picture.
			this.lastFrame = 0
			this.clear()
		}
	},
	created() {
		this.lastFrame = 0
		this.bitmap = null
		this.bitmapInfo = null
		this.buttons = 0
		this.lastKeyWas229 = false
		this.mac = isMacPlatform()
		this.coalescer = new InputCoalescer(m => this.send({ t: 'mouse', ...m }))
		this.raf = 0
		this.resizeTimer = null
	},
	mounted() {
		this.ctx = this.$refs.canvas.getContext('2d', { alpha: false })
		this.ro = new ResizeObserver(() => this.onResize())
		this.ro.observe(this.$refs.root)
		this.$refs.canvas.addEventListener('wheel', this.onWheel, { passive: false })
		this.onResize(true)
	},
	beforeUnmount() {
		if (this.ro) this.ro.disconnect()
		this.$refs.canvas.removeEventListener('wheel', this.onWheel)
		window.removeEventListener('mousemove', this.onWindowMove, true)
		window.removeEventListener('mouseup', this.onWindowUp, true)
		cancelAnimationFrame(this.raf)
		clearTimeout(this.resizeTimer)
		clearTimeout(this.longPress)
		if (this.bitmap && this.bitmap.close) this.bitmap.close()
	},
	methods: {
		// ---- public ----
		focus() {
			const el = this.$refs.input
			if (el && document.activeElement !== el) el.focus({ preventScroll: true })
		},
		blur() {
			if (this.$refs.input) this.$refs.input.blur()
		},
		setCursor(css) {
			this.cursor = css || 'default'
		},
		size() {
			const r = this.$refs.root.getBoundingClientRect()
			return { w: Math.max(1, Math.round(r.width)), h: Math.max(1, Math.round(r.height)), dpr: this.dpr() }
		},
		// Draws a picture (see parseFrameHeader). Every picture is
		// acknowledged once decoded, whatever tab it is for: that is what
		// lets the server send the next one.
		async drawFrame(f, ack) {
			if (f.tab !== this.tab) {
				ack()
				return
			}
			let bmp
			try {
				bmp = await createImageBitmap(new Blob([f.data], { type: 'image/jpeg' }))
			} catch (e) {
				ack()
				return
			}
			ack()
			if (f.tab !== this.tab || f.frame < this.lastFrame) {
				if (bmp.close) bmp.close()
				return
			}
			this.lastFrame = f.frame
			if (this.bitmap && this.bitmap.close) this.bitmap.close()
			this.bitmap = bmp
			this.bitmapInfo = f
			this.paint()
			this.$emit('frame', f)
		},
		clear() {
			if (this.bitmap && this.bitmap.close) this.bitmap.close()
			this.bitmap = null
			this.bitmapInfo = null
			this.paint()
		},

		// ---- drawing ----
		dpr() {
			return Math.min(Math.max(window.devicePixelRatio || 1, 1), 2)
		},
		paint() {
			const c = this.$refs.canvas
			if (!c || !this.ctx) return
			const ctx = this.ctx
			ctx.fillStyle = getComputedStyle(this.$refs.root).getPropertyValue('--rb-bg').trim() || '#ffffff'
			ctx.fillRect(0, 0, c.width, c.height)
			if (!this.bitmap) return
			const scale = c.width / (this.cssW || 1)
			const f = this.bitmapInfo
			// One uniform scale from the picture's width, so a picture that is
			// already at device pixels is drawn 1:1 (sharp), never squashed.
			const dw = Math.round(f.cssW * scale)
			const dh = f.w ? Math.round((f.h * dw) / f.w) : Math.round(f.cssH * scale)
			ctx.imageSmoothingQuality = 'high'
			ctx.drawImage(this.bitmap, 0, 0, dw, dh)
		},
		onResize(initial) {
			const r = this.$refs.root.getBoundingClientRect()
			const dpr = this.dpr()
			const w = Math.max(1, Math.round(r.width))
			const h = Math.max(1, Math.round(r.height))
			this.cssW = w
			const c = this.$refs.canvas
			const pw = Math.round(w * dpr)
			const ph = Math.round(h * dpr)
			if (c.width !== pw || c.height !== ph) {
				c.width = pw
				c.height = ph
				this.paint()
			}
			clearTimeout(this.resizeTimer)
			this.resizeTimer = setTimeout(() => this.$emit('resize', { w, h, dpr }), initial ? 0 : 120)
		},

		// ---- pointer ----
		point(e) {
			const r = this.$refs.canvas.getBoundingClientRect()
			return { x: Math.round((e.clientX - r.left) * 10) / 10, y: Math.round((e.clientY - r.top) * 10) / 10 }
		},
		schedule() {
			if (this.raf) return
			this.raf = requestAnimationFrame(() => {
				this.raf = 0
				this.coalescer.flush()
			})
		},
		onMouseDown(e) {
			this.focus()
			if (this.inert || e.button === 2) return
			e.preventDefault()
			const p = this.point(e)
			this.inputStyle = { left: p.x + 'px', top: p.y + 'px' }
			this.coalescer.flushMove()
			this.buttons = e.buttons
			this.send({ t: 'mouse', type: 'down', ...p, button: e.button, buttons: e.buttons, clicks: e.detail || 1, mods: modsOf(e) })
			// Keep the drag going (text selection, sliders) when the pointer
			// leaves the canvas.
			window.addEventListener('mousemove', this.onWindowMove, true)
			window.addEventListener('mouseup', this.onWindowUp, true)
		},
		onWindowMove(e) {
			if (e.target !== this.$refs.canvas) this.onMouseMove(e)
		},
		onWindowUp(e) {
			window.removeEventListener('mousemove', this.onWindowMove, true)
			window.removeEventListener('mouseup', this.onWindowUp, true)
			if (e.button === 2) return
			this.coalescer.flushMove()
			this.buttons = e.buttons
			this.send({ t: 'mouse', type: 'up', ...this.point(e), button: e.button, buttons: e.buttons, clicks: e.detail || 1, mods: modsOf(e) })
			// Mouse back/forward buttons.
			if (e.button === 3) this.$emit('shortcut', 'back')
			if (e.button === 4) this.$emit('shortcut', 'forward')
		},
		onMouseMove(e) {
			if (this.inert) return
			this.coalescer.queueMove({ type: 'move', ...this.point(e), buttons: e.buttons, mods: modsOf(e) })
			this.schedule()
		},
		onMouseLeave(e) {
			if (!this.buttons) this.cursor = 'default'
		},
		onWheel(e) {
			e.preventDefault()
			if (this.inert) return
			const d = wheelDelta(e, this.$refs.root.clientHeight)
			// Ctrl+wheel (and trackpad pinch) is page zoom, as in any browser.
			if (e.ctrlKey && !e.shiftKey) {
				this.wheelZoom = (this.wheelZoom || 0) + d.dy
				if (Math.abs(this.wheelZoom) >= 60) {
					this.$emit('shortcut', this.wheelZoom < 0 ? 'zoomIn' : 'zoomOut')
					this.wheelZoom = 0
				}
				return
			}
			this.coalescer.queueWheel({ type: 'wheel', ...this.point(e), dx: d.dx, dy: d.dy, mods: modsOf(e) })
			this.schedule()
		},
		onContextMenu(e) {
			this.focus()
			if (this.inert) return
			this.$emit('contextmenu', { ...this.point(e), clientX: e.clientX, clientY: e.clientY })
		},

		// ---- touch ----
		touchPoints(list) {
			const r = this.$refs.canvas.getBoundingClientRect()
			return Array.from(list).map(t => ({ id: t.identifier % 1000, x: t.clientX - r.left, y: t.clientY - r.top }))
		},
		onTouch(action, e) {
			if (this.inert) return
			e.preventDefault()
			if (action === 'start') {
				this.focus()
				clearTimeout(this.longPress)
				if (e.touches.length === 1) {
					const t = e.touches[0]
					const start = { x: t.clientX, y: t.clientY }
					this.touchStart = start
					// Long press = right-click menu, as on a phone.
					this.longPress = setTimeout(() => {
						this.send({ t: 'touch', action: 'cancel', points: [] })
						this.touchCancelled = true
						const r = this.$refs.canvas.getBoundingClientRect()
						this.$emit('contextmenu', { x: start.x - r.left, y: start.y - r.top, clientX: start.x, clientY: start.y })
					}, LONG_PRESS_MS)
				}
				this.touchCancelled = false
			} else if (action === 'move' && this.touchStart && e.touches[0]) {
				const t = e.touches[0]
				if (Math.hypot(t.clientX - this.touchStart.x, t.clientY - this.touchStart.y) > 8) clearTimeout(this.longPress)
			} else {
				clearTimeout(this.longPress)
			}
			if (this.touchCancelled) return
			this.send({ t: 'touch', action, points: this.touchPoints(e.touches), mods: modsOf(e) })
		},

		// ---- keyboard ----
		onKeyDown(e) {
			if (this.inert) return
			const sc = shortcutOf(e)
			if (sc) {
				e.preventDefault()
				e.stopPropagation()
				if (sc !== 'swallow') this.$emit('shortcut', sc, e)
				return
			}
			this.lastKeyWas229 = e.keyCode === 229 || e.key === 'Unidentified' || e.key === 'Dead'
			// Ctrl+V: let the paste event deliver the text (no permission
			// prompt), but still tell the page the keys went down.
			const isPaste = (e.ctrlKey || e.metaKey) && (e.key === 'v' || e.key === 'V')
			const msg = this.pageKey(e, 'down')
			if (!msg) return
			if (isPaste) return
			e.preventDefault()
			e.stopPropagation()
			this.send(msg)
		},
		onKeyUp(e) {
			if (this.inert) return
			if (shortcutOf(e)) return
			const msg = this.pageKey(e, 'up')
			if (!msg) return
			e.preventDefault()
			this.send(msg)
		},
		pageKey(e, type) {
			const msg = keyMessage(e, type)
			return this.mac ? macChordToLinux(msg) : msg
		},
		onCompositionUpdate(e) {
			const text = e.data || ''
			this.send({ t: 'ime', text, composing: true, selStart: text.length, selEnd: text.length })
		},
		onCompositionEnd(e) {
			this.composing = false
			this.send({ t: 'ime', text: e.data || '', composing: false })
			this.$refs.input.value = ''
		},
		// Phone keyboards and dead keys deliver text as input events.
		onInput(e) {
			const el = this.$refs.input
			if (this.composing || e.isComposing) return
			if (e.inputType === 'insertText' && e.data) {
				this.send({ t: 'ime', text: e.data, composing: false })
			} else if (e.inputType === 'deleteContentBackward' && this.lastKeyWas229) {
				this.send({ t: 'key', type: 'down', key: 'Backspace', code: 'Backspace', keyCode: 8, mods: 0 })
				this.send({ t: 'key', type: 'up', key: 'Backspace', code: 'Backspace', keyCode: 8, mods: 0 })
			} else if (e.inputType === 'insertLineBreak') {
				this.send({ t: 'key', type: 'down', key: 'Enter', code: 'Enter', keyCode: 13, mods: 0, text: '\r' })
				this.send({ t: 'key', type: 'up', key: 'Enter', code: 'Enter', keyCode: 13, mods: 0 })
			}
			el.value = ''
		},
		onPaste(e) {
			e.preventDefault()
			const text = e.clipboardData ? e.clipboardData.getData('text/plain') : ''
			if (text && !this.inert) this.send({ t: 'paste', text })
		}
	}
}
</script>

<style lang="scss" scoped>
.rb-viewport {
	--rb-bg: #ffffff;
	position: absolute;
	inset: 0;
	overflow: hidden;
	background: var(--rb-bg);
	outline: none;
}

.rb-canvas {
	display: block;
	width: 100%;
	height: 100%;
	touch-action: none;
	user-select: none;
	-webkit-user-select: none;
	-webkit-touch-callout: none;
}

// Holds focus for the keyboard/IME; placed at the last click so an IME's
// candidate window opens near where the user is typing.
.rb-input {
	position: absolute;
	width: 1px;
	height: 1.2em;
	padding: 0;
	border: 0;
	margin: 0;
	opacity: 0;
	resize: none;
	overflow: hidden;
	pointer-events: none;
	font-size: 16px; // stops iOS zooming into the field
	color: transparent;
	background: transparent;
	caret-color: transparent;
}
</style>
