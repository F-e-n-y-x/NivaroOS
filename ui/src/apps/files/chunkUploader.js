// Chunked uploads to /v2/casaos/file/upload (replaces simple-uploader.js; same wire protocol).
// Per chunk: an optional GET with the chunk's fields as the query (200 = the server already has it,
// skip - that's resume; anything else = send it), then a multipart POST with the same fields and the
// bytes last as `file`. A transient failure (network, 5xx) retries the chunk; a permanent status, or
// running out of retries, fails the whole file with the server's message.
import { uploadIdentifier } from './uploadIdentifier'
import { cancelUpload } from './uploadCancel'

const SUCCESS = [200, 201, 202]

export function createUploader(options) {
	const opts = {
		chunkSize: 8 * 1024 * 1024,
		simultaneous: 3,
		testChunks: true,
		retries: 0,
		retryInterval: 1500,
		permanentErrors: [400, 404, 409, 415, 501],
		headers: () => ({}),
		fields: () => ({}),
		onProgress() {},
		onSuccess() {},
		onError() {},
		onComplete() {},
		...options,
	}
	const files = []
	let busy = 0

	const params = (e, c) => ({
		chunkNumber: c.n,
		chunkSize: opts.chunkSize,
		currentChunkSize: c.end - c.start,
		totalSize: e.size,
		identifier: e.uniqueIdentifier,
		filename: e.name,
		relativePath: e.relativePath,
		totalChunks: e.chunks.length,
		...opts.fields(e),
	})

	function request(method, url, body, c, done, progress) {
		const x = new XMLHttpRequest()
		c.xhr = x
		x.open(method, url)
		for (const [k, v] of Object.entries(opts.headers())) if (v != null) x.setRequestHeader(k, v)
		if (progress) x.upload.onprogress = progress
		// A network error is status 0: transient, like a 5xx.
		x.onload = x.onerror = () => {
			if (c.xhr !== x) return // aborted (cancel / the file failed)
			c.xhr = null
			done(x.status, x.responseText)
		}
		x.send(body)
	}

	function release(c) {
		const x = c.xhr
		c.xhr = null
		clearTimeout(c.timer)
		if (c.active) busy--
		c.active = false
		if (x) x.abort()
	}

	function stop(e) {
		for (const c of e.chunks) release(c)
	}

	function finished() {
		if (!files.some((f) => f.status === 'uploading')) opts.onComplete()
	}

	function tick(e, force) {
		const now = Date.now()
		if (!force && now - e.t < 500) return
		const up = e.chunks.reduce((s, c) => s + (c.done ? c.end - c.start : c.loaded), 0)
		// Never goes backwards (a retried chunk starts again from 0).
		e.progress = Math.max(e.progress, e.size ? up / e.size : 0)
		const dt = now - e.t
		if (dt > 0) e.speed = 0.1 * Math.max(((up - e.lastUp) / dt) * 1000, 0) + 0.9 * e.speed
		e.lastUp = up
		e.t = now
		opts.onProgress(e)
	}

	function fail(e, text) {
		stop(e)
		e.status = 'error'
		e.speed = 0
		e.message = text
		opts.onError(e, text)
		finished()
	}

	function chunkDone(e, c) {
		release(c)
		c.done = true
		if (e.chunks.every((x) => x.done)) {
			e.status = 'success'
			e.progress = 1
			e.speed = 0
			opts.onSuccess(e)
			finished()
		} else tick(e, true)
	}

	function test(e, c) {
		request('GET', opts.target + '?' + new URLSearchParams(params(e, c)), null, c, (status, text) => {
			c.tested = true
			if (SUCCESS.includes(status)) chunkDone(e, c)
			else if (opts.permanentErrors.includes(status)) fail(e, text)
			else return send(e, c)
			pump()
		})
	}

	function send(e, c) {
		const form = new FormData()
		for (const [k, v] of Object.entries(params(e, c))) form.append(k, v)
		form.append('file', e.file.slice(c.start, c.end, e.file.type), e.name)
		c.loaded = 0
		request('POST', opts.target, form, c, (status, text) => {
			if (SUCCESS.includes(status)) chunkDone(e, c)
			else if (opts.permanentErrors.includes(status) || c.retries >= opts.retries) fail(e, text)
			else {
				c.retries++
				c.loaded = 0
				c.timer = setTimeout(() => send(e, c), opts.retryInterval) // keeps its slot meanwhile
				return
			}
			pump()
		}, (ev) => {
			if (ev.lengthComputable && ev.total) c.loaded = (ev.loaded / ev.total) * (c.end - c.start)
			tick(e)
		})
	}

	function pump() {
		while (busy < opts.simultaneous) {
			let e, c
			for (e of files) {
				if (e.status !== 'uploading') continue
				c = e.chunks.find((x) => !x.done && !x.active)
				if (c) break
			}
			if (!c) return
			busy++
			c.active = true
			if (opts.testChunks && !c.tested) test(e, c)
			else send(e, c)
		}
	}

	return {
		files,
		// Queues the files (a FileList or array) and starts sending; `extra` is copied onto
		// each entry (e.g. targetPath). Returns the new entries.
		add(list, extra = {}) {
			const added = []
			for (const f of Array.from(list)) {
				// A dropped folder can show up as a "." pseudo-file.
				if (f.size % 4096 === 0 && f.name === '.') continue
				const count = Math.max(Math.ceil(f.size / opts.chunkSize), 1)
				const chunks = Array.from({ length: count }, (_, i) => ({
					n: i + 1,
					start: i * opts.chunkSize,
					end: Math.min(f.size, (i + 1) * opts.chunkSize),
					done: false, active: false, tested: false, retries: 0, loaded: 0, xhr: null, timer: null,
				}))
				const e = {
					file: f,
					name: f.name,
					size: f.size,
					relativePath: f.relativePath || f.webkitRelativePath || f.name,
					uniqueIdentifier: uploadIdentifier(f),
					status: 'uploading',
					progress: 0,
					speed: 0,
					message: '',
					chunks,
					t: Date.now(),
					lastUp: 0,
					...extra,
				}
				files.push(e)
				added.push(e)
			}
			pump()
			return added
		},
		// Stops a file and, unless it already finished, tells the server to drop its temp file.
		cancel(e) {
			const i = files.indexOf(e)
			if (i < 0) return
			files.splice(i, 1)
			stop(e)
			if (e.status !== 'success') cancelUpload(opts.target, opts.headers(), e)
			e.status = 'cancelled'
			pump()
		},
	}
}
