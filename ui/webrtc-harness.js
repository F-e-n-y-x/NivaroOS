// Test page for services/vm-sidecar/hoststream_webrtc_integration_test.go
// (Chrome and Firefox): the real viewer against the lane; served by a Vite
// dev server of ui/, not part of the build. ?preset= (default low),
// ?switch=<preset> sends that preset on the data channel after 8 s. Posts
// states, sizes, cursors and per-second stats to /__log; Chrome reads window.__*.
import { createApp, h } from 'vue'
import Viewer from '@/shell/desktop/HostDesktopWebRTC.vue'

const q = new URLSearchParams(location.search)
const log = (o) => fetch('/__log', { method: 'POST', body: JSON.stringify(o) }).catch(() => {})
window.__states = []
localStorage.setItem('access_token', 'test')
const pcs = []
const PC = window.RTCPeerConnection
window.RTCPeerConnection = function (...a) { const p = new PC(...a); pcs.push(p); return p }
window.RTCPeerConnection.prototype = PC.prototype
let chan = null
const cdc = PC.prototype.createDataChannel
PC.prototype.createDataChannel = function (...a) { return (chan = cdc.apply(this, a)) }
const send = RTCDataChannel.prototype.send
if (q.get('switch')) setTimeout(() => chan && send.call(chan, JSON.stringify({ t: 'preset', preset: q.get('switch') })), 8000)
setInterval(async () => {
	const p = pcs.at(-1)
	const o = { ev: 'stats', pcs: pcs.length }
	if (p) (await p.getStats()).forEach((s) => {
		if (s.type === 'inbound-rtp' && s.kind === 'video') Object.assign(o, { dec: s.framesDecoded, w: s.frameWidth })
		if (s.type === 'codec') o.codec = s.sdpFmtpLine
	})
	const v = document.querySelector('video')
	if (v) Object.assign(o, { vw: v.videoWidth, vh: v.videoHeight })
	log(o)
}, 1000)
createApp({ render: () => h('div', { style: 'position:fixed;inset:0' }, [h(Viewer, {
	fallback: false, preset: q.get('preset') || 'low',
	onState: (s) => { window.__states.push(s.state + ':' + s.reason); log({ ev: 'state', s: s.state, r: s.reason }) },
	onSize: (s) => { window.__size = s; log({ ev: 'size', w: s.w, h: s.h }) },
	onStats: (s) => (window.__stats = s),
	onCursor: (c) => { window.__cursor = c; setTimeout(() => log({ ev: 'cursor', x: c.x, y: c.y, w: c.w,
		css: document.querySelector('.hd-webrtc').style.cursor.slice(0, 40) }), 300) } })]) }).mount('#app')
