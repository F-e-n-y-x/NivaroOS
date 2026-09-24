/* Download Station lite-browser shim - injected at the top of every proxied
   HTML page. Keeps URLs a page's own scripts build at runtime inside the
   proxy (the server already rewrote the static markup), reports navigation
   to the Download Station window, and applies element hiding to content
   added after load. */
(function () {
	'use strict'
	var C = __NVDS_CONFIG__
	if (window.__nvds) return
	window.__nvds = C
	var PREFIX = C.prefix
	var ORIGIN = location.origin

	// The page's real URL, derived from the proxied location - kept live so
	// pushState navigation is reflected too.
	function realURL(loc) {
		loc = loc || location
		var p = loc.pathname
		if (p.indexOf(PREFIX) !== 0) return C.url
		var rest = p.slice(PREFIX.length)
		var i = rest.indexOf('/')
		var j = rest.indexOf('/', i + 1)
		if (i < 0) return C.url
		var scheme = rest.slice(0, i)
		var host = j < 0 ? rest.slice(i + 1) : rest.slice(i + 1, j)
		var path = j < 0 ? '/' : rest.slice(j)
		return scheme + '://' + host + path + loc.search + loc.hash
	}

	function toProxy(u) {
		if (u == null) return u
		if (typeof u === 'object' && u.href) u = u.href
		u = String(u)
		var t = u.trim()
		if (!t || t[0] === '#') return u
		var lower = t.slice(0, 12).toLowerCase()
		if (/^(javascript|data|blob|mailto|tel|about|sms|magnet|intent):/.test(lower)) return u
		if (t.indexOf(PREFIX) === 0 || t.indexOf(ORIGIN + PREFIX) === 0) return u
		var abs
		try {
			abs = new URL(t, realURL())
		} catch (e) {
			return u
		}
		if (abs.protocol !== 'http:' && abs.protocol !== 'https:') return u
		// A URL on this proxy's own origin but outside the prefix came from
		// resolving a relative path against the proxied location - map it
		// back onto the real site.
		if (abs.origin === ORIGIN) return u
		return PREFIX + abs.protocol.slice(0, -1) + '/' + abs.host + abs.pathname + abs.search + abs.hash
	}
	window.__nvdsToProxy = toProxy

	function wrap(obj, name, fn) {
		try {
			var orig = obj[name]
			if (typeof orig !== 'function') return
			obj[name] = fn(orig)
		} catch (e) {}
	}

	// fetch / XHR / beacons
	wrap(window, 'fetch', function (orig) {
		return function (input, init) {
			try {
				if (typeof input === 'string' || input instanceof URL) input = toProxy(input)
				else if (input && input.url) {
					var p = toProxy(input.url)
					if (p !== input.url) input = new Request(p, input)
				}
			} catch (e) {}
			return orig.call(this, input, init)
		}
	})
	if (window.XMLHttpRequest) {
		wrap(XMLHttpRequest.prototype, 'open', function (orig) {
			return function (method, u) {
				var args = Array.prototype.slice.call(arguments)
				args[1] = toProxy(u)
				return orig.apply(this, args)
			}
		})
	}
	if (navigator.sendBeacon) {
		wrap(navigator, 'sendBeacon', function (orig) {
			return function (u, d) {
				return orig.call(navigator, toProxy(u), d)
			}
		})
	}
	if (window.EventSource) {
		var ES = window.EventSource
		window.EventSource = function (u, o) {
			return new ES(toProxy(u), o)
		}
		window.EventSource.prototype = ES.prototype
	}

	// window.open: new windows can't be tracked from the Download Station
	// window, so open them in this same frame instead (a popup ad simply
	// never gets a window of its own).
	wrap(window, 'open', function () {
		return function (u) {
			if (u) {
				try {
					location.href = toProxy(u)
				} catch (e) {}
			}
			return window
		}
	})

	// History entries must stay same-origin (the proxy's), or pushState
	// throws a SecurityError.
	;['pushState', 'replaceState'].forEach(function (name) {
		wrap(history, name, function (orig) {
			return function (state, title, u) {
				var r = orig.call(history, state, title, u == null ? u : toProxy(u))
				report()
				return r
			}
		})
	})

	// URL-valued DOM properties and setAttribute
	var props = [
		[window.HTMLScriptElement, 'src'],
		[window.HTMLImageElement, 'src'],
		[window.HTMLIFrameElement, 'src'],
		[window.HTMLSourceElement, 'src'],
		[window.HTMLMediaElement, 'src'],
		[window.HTMLEmbedElement, 'src'],
		[window.HTMLTrackElement, 'src'],
		[window.HTMLLinkElement, 'href'],
		[window.HTMLAnchorElement, 'href'],
		[window.HTMLAreaElement, 'href'],
		[window.HTMLFormElement, 'action'],
		[window.HTMLObjectElement, 'data'],
		[window.HTMLVideoElement, 'poster'],
	]
	props.forEach(function (pair) {
		var ctor = pair[0]
		var prop = pair[1]
		if (!ctor) return
		var d = Object.getOwnPropertyDescriptor(ctor.prototype, prop)
		if (!d || !d.set) return
		try {
			Object.defineProperty(ctor.prototype, prop, {
				configurable: true,
				enumerable: d.enumerable,
				get: d.get,
				set: function (v) {
					d.set.call(this, toProxy(v))
				},
			})
		} catch (e) {}
	})
	var URL_ATTRS = { src: 1, href: 1, action: 1, formaction: 1, poster: 1, data: 1 }
	wrap(Element.prototype, 'setAttribute', function (orig) {
		return function (name, value) {
			var n = String(name).toLowerCase()
			if (URL_ATTRS[n]) value = toProxy(value)
			else if (n === 'target' && /^(_blank|_top|_parent|_new)$/i.test(value)) value = '_self'
			return orig.call(this, name, value)
		}
	})

	// Links and forms added or modified after the server rewrite ran.
	function fixTarget(el) {
		var t = el.getAttribute && el.getAttribute('target')
		if (t && /^(_blank|_top|_parent|_new)$/i.test(t)) el.setAttribute('target', '_self')
	}
	document.addEventListener(
		'click',
		function (e) {
			var a = e.target && e.target.closest && e.target.closest('a[href],area[href]')
			if (!a) return
			fixTarget(a)
			var h = a.getAttribute('href')
			var p = toProxy(h)
			if (p !== h) a.setAttribute('href', p)
		},
		true
	)
	document.addEventListener(
		'submit',
		function (e) {
			var f = e.target
			if (!f || !f.getAttribute) return
			fixTarget(f)
			var act = f.getAttribute('action')
			if (act) {
				var p = toProxy(act)
				if (p !== act) f.setAttribute('action', p)
			}
		},
		true
	)

	// Navigation reporting to the Download Station window (address bar,
	// title, back/forward state). Only the top browsing frame reports. The
	// app doesn't take the URL on trust (page scripts can post anything):
	// it looks the nav ID up on the sidecar and only accepts same-origin
	// history changes of the document that was really served.
	var isMain = false
	try {
		isMain = window.parent === window.top && window.parent !== window
	} catch (e) {}
	function report() {
		if (!isMain) return
		try {
			window.parent.postMessage({ nvds: 1, type: 'nav', sid: C.sid, nav: C.nav, url: realURL(), title: document.title || '' }, '*')
		} catch (e) {}
	}
	if (isMain) {
		window.addEventListener('popstate', report)
		window.addEventListener('hashchange', report)
		document.addEventListener('DOMContentLoaded', function () {
			report()
			var t = document.querySelector('title')
			if (t && window.MutationObserver) new MutationObserver(report).observe(t, { childList: true, characterData: true, subtree: true })
		})
		window.addEventListener('load', report)
		report()
		// Reload from the Download Station toolbar - the app can't reach into
		// this frame itself (different origin). Back/forward are not done
		// here: history.back() in a frame walks the whole browser tab's
		// history, so on a tab's first page it took NivaroOS itself back.
		// The app keeps each tab's history and loads the page instead.
		window.addEventListener('message', function (e) {
			if (e.source !== window.parent || !e.data || e.data.nvds !== 1) return
			if (e.data.cmd === 'reload') location.reload()
		})
		// The same goes for the mouse's back/forward buttons and Alt+Left /
		// Alt+Right pressed inside the page: hand them to the app.
		function histKey(dir, ev) {
			ev.preventDefault()
			ev.stopPropagation()
			try {
				window.parent.postMessage({ nvds: 1, type: 'history', dir: dir }, '*')
			} catch (e) {}
		}
		window.addEventListener(
			'keydown',
			function (e) {
				if (!e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return
				if (e.key === 'ArrowLeft') histKey('back', e)
				else if (e.key === 'ArrowRight') histKey('forward', e)
			},
			true
		)
		;['mousedown', 'mouseup', 'auxclick'].forEach(function (type) {
			window.addEventListener(
				type,
				function (e) {
					if (e.button !== 3 && e.button !== 4) return
					if (type !== 'mouseup') {
						e.preventDefault()
						return
					}
					histKey(e.button === 3 ? 'back' : 'forward', e)
				},
				true
			)
		})
	}

	// Element hiding for classes/ids that only show up after load.
	if (C.adblock && window.MutationObserver) {
		var seen = Object.create(null)
		var pending = []
		var timer = null
		function note(k) {
			if (!seen[k]) {
				seen[k] = 1
				pending.push(k)
			}
		}
		function scan(el) {
			if (!el || el.nodeType !== 1) return
			if (el.id) note('#' + el.id)
			var cl = el.classList
			if (cl) for (var i = 0; i < cl.length; i++) note('.' + cl[i])
		}
		function flush() {
			timer = null
			if (!pending.length) return
			var keys = pending.splice(0, 400)
			var host = new URL(realURL()).hostname
			var u = PREFIX + '__nvds/cosmetic?host=' + encodeURIComponent(host) + '&page=' + encodeURIComponent(realURL()) + '&keys=' + encodeURIComponent(keys.join(' '))
			var f = window.__nvdsFetch || fetch
			f(u)
				.then(function (r) {
					return r.text()
				})
				.then(function (css) {
					if (!css) return
					var s = document.createElement('style')
					s.setAttribute('data-nvds', '')
					s.textContent = css
					;(document.head || document.documentElement).appendChild(s)
				})
				.catch(function () {})
			if (pending.length) timer = setTimeout(flush, 300)
		}
		function schedule() {
			if (!timer && pending.length) timer = setTimeout(flush, 300)
		}
		document.addEventListener('DOMContentLoaded', function () {
			// Everything present in the initial markup was already covered
			// server-side; just mark it seen.
			var all = document.getElementsByTagName('*')
			for (var i = 0; i < all.length; i++) scan(all[i])
			pending = []
			new MutationObserver(function (muts) {
				for (var i = 0; i < muts.length; i++) {
					var m = muts[i]
					if (m.type === 'attributes') scan(m.target)
					else
						for (var j = 0; j < m.addedNodes.length; j++) {
							var n = m.addedNodes[j]
							scan(n)
							if (n.querySelectorAll) {
								var inner = n.querySelectorAll('[id],[class]')
								for (var k = 0; k < inner.length && k < 500; k++) scan(inner[k])
							}
						}
				}
				schedule()
			}).observe(document.documentElement, { childList: true, subtree: true, attributes: true, attributeFilter: ['class', 'id'] })
		})
	}
})()
