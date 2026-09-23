package main

import (
	"bufio"
	"regexp"
	"sort"
	"strings"
)

// This is a from-scratch matcher for the uBlock Origin / Adblock Plus
// filter syntax, run by the lite browser's proxy - not the uBO extension
// itself (an extension can't run inside a page the proxy serves). It uses
// uBO's own default filter lists, and covers the parts of the syntax those
// lists overwhelmingly rely on:
//
//   - network filters: ||host^, |anchors|, * and ^ wildcards, /regex/,
//     @@exceptions, and the options $script $image $stylesheet $xhr
//     $subdocument $media $font $object $ping $websocket $other $document
//     $third-party/$1p/$3p $domain=/$from= $important $all $badfilter
//     (plus $redirect= treated as a plain block)
//   - cosmetic filters: ##selector (generic and per-site), #@# exceptions,
//     and $generichide/$elemhide exceptions
//
// Anything else - scriptlet injection (##+js), procedural cosmetic
// operators (:has-text, :upward, ...), HTML filters (##^), $removeparam,
// $csp, $redirect-rule and friends - is skipped rather than guessed at:
// a filter applied wrongly breaks pages, a filter skipped just lets one
// ad through.

type resType uint32

const (
	typeOther resType = 1 << iota
	typeScript
	typeImage
	typeStylesheet
	typeObject
	typeXHR
	typeSubdocument
	typePing
	typeMedia
	typeFont
	typeWebsocket
	typeDocument

	typeAllButDocument = typeOther | typeScript | typeImage | typeStylesheet | typeObject | typeXHR | typeSubdocument | typePing | typeMedia | typeFont | typeWebsocket
)

var typeOptions = map[string]resType{
	"script": typeScript, "image": typeImage, "stylesheet": typeStylesheet, "css": typeStylesheet,
	"object": typeObject, "xmlhttprequest": typeXHR, "xhr": typeXHR, "subdocument": typeSubdocument,
	"frame": typeSubdocument, "ping": typePing, "beacon": typePing, "media": typeMedia, "font": typeFont,
	"websocket": typeWebsocket, "other": typeOther, "document": typeDocument, "doc": typeDocument,
}

// resTypeFromFetchDest maps the browser's Sec-Fetch-Dest request header
// (which every current browser sends) to a filter resource type - how the
// proxy learns whether a request is a script, an image, a frame, etc.
func resTypeFromFetchDest(dest string) resType {
	switch dest {
	case "document":
		return typeDocument
	case "iframe", "frame", "fencedframe":
		return typeSubdocument
	case "script", "worker", "sharedworker", "serviceworker", "audioworklet", "paintworklet":
		return typeScript
	case "style":
		return typeStylesheet
	case "image":
		return typeImage
	case "font":
		return typeFont
	case "audio", "video", "track":
		return typeMedia
	case "object", "embed":
		return typeObject
	case "empty":
		return typeXHR
	}
	return typeOther
}

type netFilter struct {
	raw       string
	exception bool
	important bool

	// Pattern matching, one of: hostOnly (pure ||host^), regex, or glob.
	hostOnly   string
	regex      *regexp.Regexp
	pattern    string
	hostAnchor bool // ||
	startAnch  bool // |
	endAnch    bool // |

	types    resType // 0 = default set (everything but document)
	explicit bool    // types were listed explicitly
	party    int     // 0 any, 1 third-party only, -1 first-party only

	domainsInc []string
	domainsExc []string

	// For exception filters: $generichide / $elemhide / $document.
	genericHide bool
	elemHide    bool
}

type cosmeticFilter struct {
	selector  string
	domains   []string // empty = generic
	negated   []string
	exception bool
}

// FilterEngine is immutable once built - a list update builds a new one
// and swaps it in atomically (see Adblocker).
type FilterEngine struct {
	hostFilters  map[string][]*netFilter
	tokenFilters map[string][]*netFilter
	anyFilters   []*netFilter

	// Generic cosmetic selectors keyed by their leading .class/#id (uBO's
	// "lowly generic" split) - only injected when the page actually uses
	// that class/id. Selectors with no such key go in alwaysGeneric.
	genericByKey    map[string][]string
	alwaysGeneric   []string
	specific        []cosmeticFilter
	genericExcepted map[string]bool

	NetworkCount  int
	CosmeticCount int
}

func newFilterEngine() *FilterEngine {
	return &FilterEngine{
		hostFilters:     map[string][]*netFilter{},
		tokenFilters:    map[string][]*netFilter{},
		genericByKey:    map[string][]string{},
		genericExcepted: map[string]bool{},
	}
}

// BuildFilterEngine parses any number of filter-list texts into one engine.
func BuildFilterEngine(lists ...string) *FilterEngine {
	e := newFilterEngine()
	var nets []*netFilter
	bad := map[string]bool{}
	var cosmetics []cosmeticFilter
	for _, text := range lists {
		sc := bufio.NewScanner(strings.NewReader(text))
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || line[0] == '!' || (line[0] == '[' && strings.HasSuffix(line, "]")) {
				continue
			}
			// Hosts-file format (Peter Lowe's list, etc.): "0.0.0.0 host" / "127.0.0.1 host".
			if strings.HasPrefix(line, "0.0.0.0 ") || strings.HasPrefix(line, "127.0.0.1 ") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[1] != "localhost" && fields[1] != "0.0.0.0" {
					line = "||" + fields[1] + "^"
				} else {
					continue
				}
			}
			if cf, ok, isCosmetic := parseCosmetic(line); isCosmetic {
				if ok {
					cosmetics = append(cosmetics, cf)
				}
				continue
			}
			f, isBad := parseNetFilter(line)
			if f == nil {
				continue
			}
			if isBad {
				bad[f.raw] = true
				continue
			}
			nets = append(nets, f)
		}
	}
	for _, f := range nets {
		if bad[f.raw] {
			continue
		}
		e.addNet(f)
	}
	e.addCosmetics(cosmetics)
	return e
}

var optionSplit = regexp.MustCompile(`\$(~?[a-z0-9_\-]+(=[^,]*)?(,~?[a-z0-9_\-]+(=[^,]*)?)*)$`)

// parseNetFilter returns (filter, isBadfilter). A nil filter means the line
// was unsupported or unparseable and is ignored.
func parseNetFilter(line string) (*netFilter, bool) {
	f := &netFilter{}
	if strings.HasPrefix(line, "@@") {
		f.exception = true
		line = line[2:]
	}
	pattern := line
	opts := ""
	// Regex filters can contain "$" themselves - only treat a trailing
	// $options block as options when it isn't inside the /regex/.
	if !(strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/")) {
		if loc := optionSplit.FindStringSubmatchIndex(pattern); loc != nil {
			opts = pattern[loc[2]:loc[3]]
			pattern = pattern[:loc[0]]
		}
	}
	isBad := false
	if opts != "" {
		for _, opt := range strings.Split(opts, ",") {
			neg := strings.HasPrefix(opt, "~")
			name := strings.TrimPrefix(opt, "~")
			val := ""
			if i := strings.IndexByte(name, '='); i >= 0 {
				name, val = name[:i], name[i+1:]
			}
			if t, ok := typeOptions[name]; ok {
				if neg {
					if !f.explicit {
						f.types = typeAllButDocument
					}
					f.types &^= t
				} else {
					if !f.explicit {
						f.types = 0
					}
					f.types |= t
				}
				f.explicit = true
				continue
			}
			switch name {
			case "third-party", "3p":
				f.party = 1
				if neg {
					f.party = -1
				}
			case "first-party", "1p":
				f.party = -1
				if neg {
					f.party = 1
				}
			case "domain", "from":
				for _, dm := range strings.Split(val, "|") {
					dm = strings.ToLower(strings.TrimSpace(dm))
					if dm == "" {
						continue
					}
					if strings.HasPrefix(dm, "~") {
						f.domainsExc = append(f.domainsExc, dm[1:])
					} else {
						f.domainsInc = append(f.domainsInc, dm)
					}
				}
			case "important":
				f.important = true
			case "match-case":
			case "all":
				f.types = typeAllButDocument | typeDocument
				f.explicit = true
			case "badfilter":
				isBad = true
			case "redirect", "rewrite":
				// uBO swaps in a harmless stub resource; we can't, so a plain
				// block is the closest honest equivalent.
			case "generichide", "ghide":
				f.genericHide = true
			case "elemhide", "ehide":
				f.elemHide = true
			case "popup", "popunder":
				// Popup-only filters act on window.open, which the proxy
				// never sees as a request - skip rather than over-block.
				if !f.explicit {
					return nil, false
				}
			case "specifichide", "shide", "strict1p", "strict3p":
			default:
				// $removeparam, $csp, $redirect-rule, $replace, $header,
				// $denyallow, $to, $method, $permissions, ...: unsupported.
				return nil, false
			}
		}
	}
	// Cosmetic-only exception options ($generichide etc.) never block.
	if (f.genericHide || f.elemHide) && !f.exception {
		return nil, false
	}

	if strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") && len(pattern) > 2 {
		re, err := regexp.Compile("(?i)" + pattern[1:len(pattern)-1])
		if err != nil {
			return nil, false
		}
		f.regex = re
	} else {
		p := strings.ToLower(pattern)
		if strings.HasPrefix(p, "||") {
			f.hostAnchor = true
			p = p[2:]
		} else if strings.HasPrefix(p, "|") {
			f.startAnch = true
			p = p[1:]
		}
		if strings.HasSuffix(p, "|") {
			f.endAnch = true
			p = p[:len(p)-1]
		}
		for strings.HasPrefix(p, "*") && !f.hostAnchor && !f.startAnch {
			p = p[1:]
		}
		for strings.HasSuffix(p, "*") && !f.endAnch {
			p = p[:len(p)-1]
		}
		f.pattern = p
		if f.hostAnchor && !f.endAnch && isPlainHostPattern(p) {
			f.hostOnly = strings.TrimSuffix(p, "^")
		}
		if p == "" && len(f.domainsInc) == 0 && !f.genericHide && !f.elemHide && !(f.exception && f.explicit) {
			// A bare "*" / "$script" style filter with no pattern at all
			// would match every request on every site.
			return nil, false
		}
	}
	f.raw = line
	if isBad {
		// The badfilter's target is the same line minus the badfilter option.
		f.raw = removeOption(line, "badfilter")
		if f.exception {
			f.raw = "@@" + f.raw
		}
	} else if f.exception {
		f.raw = "@@" + line
	}
	return f, isBad
}

func removeOption(line, opt string) string {
	i := strings.LastIndexByte(line, '$')
	if i < 0 {
		return line
	}
	var kept []string
	for _, o := range strings.Split(line[i+1:], ",") {
		if o != opt {
			kept = append(kept, o)
		}
	}
	if len(kept) == 0 {
		return line[:i]
	}
	return line[:i] + "$" + strings.Join(kept, ",")
}

func isPlainHostPattern(p string) bool {
	host := strings.TrimSuffix(p, "^")
	if host == "" || strings.ContainsAny(host, "*^/|?=&:") {
		return false
	}
	return strings.Contains(host, ".")
}

var tokenRe = regexp.MustCompile(`[a-z0-9%]{3,}`)

func (e *FilterEngine) addNet(f *netFilter) {
	e.NetworkCount++
	if f.hostOnly != "" {
		e.hostFilters[f.hostOnly] = append(e.hostFilters[f.hostOnly], f)
		return
	}
	if f.regex == nil {
		// Index under the longest literal token that can't be cut by a
		// wildcard - any URL this filter matches must contain it.
		best := ""
		for _, loc := range tokenRe.FindAllStringIndex(f.pattern, -1) {
			tok := f.pattern[loc[0]:loc[1]]
			if loc[0] > 0 && f.pattern[loc[0]-1] == '*' {
				continue
			}
			if loc[1] < len(f.pattern) && f.pattern[loc[1]] == '*' {
				continue
			}
			if loc[0] == 0 && !f.hostAnchor && !f.startAnch {
				continue
			}
			if loc[1] == len(f.pattern) && !f.endAnch {
				continue
			}
			if len(tok) > len(best) {
				best = tok
			}
		}
		if best != "" {
			e.tokenFilters[best] = append(e.tokenFilters[best], f)
			return
		}
	}
	e.anyFilters = append(e.anyFilters, f)
}

// RequestInfo describes one request being checked.
type RequestInfo struct {
	URL      string // absolute, lower-cased by Match
	Host     string
	PageHost string // the top-level page's host ("" if unknown)
	Type     resType
}

// MatchResult says whether to block and which filter decided it.
type MatchResult struct {
	Blocked bool
	Filter  string
}

func (e *FilterEngine) Match(r RequestInfo) MatchResult {
	if e == nil {
		return MatchResult{}
	}
	urlL := strings.ToLower(r.URL)
	host := strings.ToLower(r.Host)
	page := strings.ToLower(r.PageHost)
	third := page != "" && registrableDomain(host) != registrableDomain(page)

	var block, important *netFilter
	var exception *netFilter
	consider := func(f *netFilter) {
		// $generichide/$elemhide exceptions only switch off element hiding;
		// they must never un-block network requests.
		if f.genericHide || f.elemHide {
			return
		}
		if !f.appliesTo(r.Type, third, page) || !f.matchURL(urlL, host) {
			return
		}
		if f.exception {
			if exception == nil {
				exception = f
			}
			return
		}
		if f.important && important == nil {
			important = f
		}
		if block == nil {
			block = f
		}
	}

	for h := host; h != ""; {
		for _, f := range e.hostFilters[h] {
			consider(f)
		}
		i := strings.IndexByte(h, '.')
		if i < 0 {
			break
		}
		h = h[i+1:]
	}
	seen := map[string]bool{}
	for _, tok := range tokenRe.FindAllString(urlL, -1) {
		if seen[tok] {
			continue
		}
		seen[tok] = true
		for _, f := range e.tokenFilters[tok] {
			consider(f)
		}
	}
	for _, f := range e.anyFilters {
		consider(f)
	}

	if important != nil {
		return MatchResult{Blocked: true, Filter: important.raw}
	}
	if block != nil && exception == nil {
		return MatchResult{Blocked: true, Filter: block.raw}
	}
	return MatchResult{}
}

func (f *netFilter) appliesTo(t resType, third bool, page string) bool {
	types := f.types
	if !f.explicit {
		types = typeAllButDocument
		// A plain "||ads.example^" also blocks navigating to that host
		// outright - uBO's "strict blocking", with its warning page.
		if f.hostOnly != "" && !f.exception {
			types |= typeDocument
		}
	}
	if types&t == 0 {
		return false
	}
	if f.party == 1 && !third {
		return false
	}
	if f.party == -1 && third {
		return false
	}
	if len(f.domainsInc) > 0 || len(f.domainsExc) > 0 {
		if page == "" {
			return len(f.domainsInc) == 0
		}
		for _, d := range f.domainsExc {
			if hostMatchesDomain(page, d) {
				return false
			}
		}
		if len(f.domainsInc) > 0 {
			ok := false
			for _, d := range f.domainsInc {
				if hostMatchesDomain(page, d) {
					ok = true
					break
				}
			}
			return ok
		}
	}
	return true
}

func (f *netFilter) matchURL(urlL, host string) bool {
	if f.hostOnly != "" {
		return host == f.hostOnly || strings.HasSuffix(host, "."+f.hostOnly)
	}
	if f.regex != nil {
		return f.regex.MatchString(urlL)
	}
	p := f.pattern
	if f.hostAnchor {
		// Try every position that starts a hostname label.
		start := strings.Index(urlL, "://")
		if start < 0 {
			return false
		}
		start += 3
		hostEnd := start + len(host)
		if hostEnd > len(urlL) {
			hostEnd = len(urlL)
		}
		for i := start; i < hostEnd; i++ {
			if i == start || urlL[i-1] == '.' {
				if globMatch(p, urlL[i:], f.endAnch) {
					return true
				}
			}
		}
		return false
	}
	if f.startAnch {
		return globMatch(p, urlL, f.endAnch)
	}
	for i := 0; i <= len(urlL); i++ {
		if globMatch(p, urlL[i:], f.endAnch) {
			return true
		}
		if p != "" && p[0] != '*' && p[0] != '^' {
			// Fast-skip to the next occurrence of the first literal byte.
			j := strings.IndexByte(urlL[i+1:], p[0])
			if j < 0 {
				return false
			}
			i += j
		}
	}
	return false
}

func isSeparator(c byte) bool {
	return !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == '%')
}

// globMatch reports whether pattern matches a prefix of s (or all of s when
// anchoredEnd). '*' is any run of characters; '^' is one separator
// character or the end of the URL.
func globMatch(p, s string, anchoredEnd bool) bool {
	for len(p) > 0 {
		switch p[0] {
		case '*':
			for len(p) > 0 && p[0] == '*' {
				p = p[1:]
			}
			if len(p) == 0 {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if globMatch(p, s[i:], anchoredEnd) {
					return true
				}
			}
			return false
		case '^':
			if len(s) == 0 {
				p = p[1:]
				continue
			}
			if !isSeparator(s[0]) {
				return false
			}
			p, s = p[1:], s[1:]
		default:
			if len(s) == 0 || s[0] != p[0] {
				return false
			}
			p, s = p[1:], s[1:]
		}
	}
	return !anchoredEnd || len(s) == 0
}

// hostMatchesDomain handles "example.com" (and subdomains) plus uBO's
// entity form "example.*" (any TLD).
func hostMatchesDomain(host, d string) bool {
	if strings.HasSuffix(d, ".*") {
		base := strings.TrimSuffix(d, "*")
		for h := host; h != ""; {
			if strings.HasPrefix(h, base) {
				// "example.*" matches example.com / example.co.uk, not
				// example.com.evil.net.
				rest := h[len(base):]
				if rest != "" && strings.Count(rest, ".") <= 1 {
					return true
				}
			}
			i := strings.IndexByte(h, '.')
			if i < 0 {
				break
			}
			h = h[i+1:]
		}
		return false
	}
	return host == d || strings.HasSuffix(host, "."+d)
}

// A deliberately small public-suffix approximation: enough to treat
// cdn.example.co.uk and www.example.co.uk as the same party without
// shipping the full PSL.
var twoLevelSuffixes = map[string]bool{
	"co.uk": true, "org.uk": true, "ac.uk": true, "gov.uk": true, "com.au": true, "net.au": true, "org.au": true,
	"co.jp": true, "ne.jp": true, "co.in": true, "net.in": true, "org.in": true, "com.br": true, "com.cn": true,
	"com.mx": true, "com.tr": true, "co.kr": true, "co.nz": true, "co.za": true, "com.sg": true, "com.hk": true,
	"com.tw": true, "com.ar": true, "com.pl": true, "github.io": true, "blogspot.com": true, "cloudfront.net": true,
	"herokuapp.com": true, "appspot.com": true, "netlify.app": true, "vercel.app": true, "pages.dev": true,
}

func registrableDomain(host string) string {
	parts := strings.Split(host, ".")
	if len(parts) <= 2 {
		return host
	}
	if twoLevelSuffixes[strings.Join(parts[len(parts)-2:], ".")] && len(parts) >= 3 {
		return strings.Join(parts[len(parts)-3:], ".")
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

// ---- cosmetic filters ----

var unsupportedCosmetic = []string{
	":has-text(", ":-abp-contains(", ":contains(", ":upward(", ":xpath(", ":matches-css", ":matches-attr(",
	":matches-path(", ":matches-prop(", ":min-text-length(", ":others(", ":remove(", ":remove-attr(",
	":remove-class(", ":style(", ":watch-attr(", ":-abp-has(", ":if(", ":if-not(", ":nth-ancestor(",
	":spath(", ":shadow(", ":matches-media(",
}

// parseCosmetic returns (filter, supported, isCosmeticLine).
func parseCosmetic(line string) (cosmeticFilter, bool, bool) {
	idx := -1
	sepLen := 0
	exception := false
	for i := 0; i+1 < len(line); i++ {
		if line[i] != '#' {
			continue
		}
		rest := line[i:]
		switch {
		case strings.HasPrefix(rest, "#@#"):
			idx, sepLen, exception = i, 3, true
		case strings.HasPrefix(rest, "##"):
			idx, sepLen = i, 2
		case strings.HasPrefix(rest, "#?#"), strings.HasPrefix(rest, "#$#"), strings.HasPrefix(rest, "#@?#"),
			strings.HasPrefix(rest, "#@$#"), strings.HasPrefix(rest, "#%#"), strings.HasPrefix(rest, "#@%#"):
			return cosmeticFilter{}, false, true
		}
		if idx >= 0 {
			break
		}
	}
	if idx < 0 {
		return cosmeticFilter{}, false, false
	}
	// A "#" inside a URL network filter (e.g. "||x.com/#foo") would be a
	// false positive - domains never contain '/', '|' or '^'.
	domainPart := line[:idx]
	if strings.ContainsAny(domainPart, "/|^$*=") && !strings.HasSuffix(domainPart, ".*") {
		return cosmeticFilter{}, false, false
	}
	sel := strings.TrimSpace(line[idx+sepLen:])
	if sel == "" || sel[0] == '+' || sel[0] == '^' {
		return cosmeticFilter{}, false, true
	}
	for _, u := range unsupportedCosmetic {
		if strings.Contains(sel, u) {
			return cosmeticFilter{}, false, true
		}
	}
	if strings.ContainsAny(sel, "{}") {
		return cosmeticFilter{}, false, true
	}
	cf := cosmeticFilter{selector: sel, exception: exception}
	if domainPart != "" {
		for _, d := range strings.Split(strings.ToLower(domainPart), ",") {
			d = strings.TrimSpace(d)
			if d == "" {
				continue
			}
			if strings.HasPrefix(d, "~") {
				cf.negated = append(cf.negated, d[1:])
			} else {
				cf.domains = append(cf.domains, d)
			}
		}
	}
	return cf, true, true
}

var leadingKeyRe = regexp.MustCompile(`^([.#])([A-Za-z_][\w-]*)`)

func genericKey(sel string) string {
	if m := leadingKeyRe.FindStringSubmatch(sel); m != nil {
		// Only a key if the rest can't widen the match past it (a top-level
		// combinator list like ".ad, div" can't use ".ad" as its key).
		if !strings.Contains(sel, ",") {
			return m[1] + m[2]
		}
	}
	return ""
}

func (e *FilterEngine) addCosmetics(list []cosmeticFilter) {
	for _, cf := range list {
		if cf.exception && len(cf.domains) == 0 {
			e.genericExcepted[cf.selector] = true
		}
	}
	for _, cf := range list {
		e.CosmeticCount++
		if len(cf.domains) == 0 && !cf.exception {
			if e.genericExcepted[cf.selector] {
				continue
			}
			if len(cf.negated) > 0 {
				e.specific = append(e.specific, cf)
				continue
			}
			if k := genericKey(cf.selector); k != "" {
				e.genericByKey[k] = append(e.genericByKey[k], cf.selector)
			} else {
				e.alwaysGeneric = append(e.alwaysGeneric, cf.selector)
			}
			continue
		}
		e.specific = append(e.specific, cf)
	}
}

// CosmeticOptions carries the page-level exception flags a network
// exception filter can set ($generichide/$elemhide).
func (e *FilterEngine) cosmeticExceptions(pageURL, host string) (genericHide, elemHide bool) {
	urlL := strings.ToLower(pageURL)
	check := func(f *netFilter) {
		if !f.exception || (!f.genericHide && !f.elemHide) {
			return
		}
		if f.matchURL(urlL, host) {
			genericHide = genericHide || f.genericHide
			elemHide = elemHide || f.elemHide
		}
	}
	for h := host; h != ""; {
		for _, f := range e.hostFilters[h] {
			check(f)
		}
		i := strings.IndexByte(h, '.')
		if i < 0 {
			break
		}
		h = h[i+1:]
	}
	for _, tok := range tokenRe.FindAllString(urlL, -1) {
		for _, f := range e.tokenFilters[tok] {
			check(f)
		}
	}
	return
}

// SelectorsForPage returns the element-hiding selectors for a page, given
// the classes and ids its HTML uses (keys like ".ad-banner", "#sidebar-ad").
// includeAlways adds the key-less generic selectors - wanted for the first
// stylesheet of a page, not for later incremental lookups.
func (e *FilterEngine) SelectorsForPage(pageURL, host string, keys []string, includeAlways bool) []string {
	if e == nil {
		return nil
	}
	host = strings.ToLower(host)
	genericHide, elemHide := e.cosmeticExceptions(pageURL, host)
	if elemHide {
		return nil
	}
	excepted := map[string]bool{}
	var out []string
	for _, cf := range e.specific {
		if !cf.exception {
			continue
		}
		for _, d := range cf.domains {
			if hostMatchesDomain(host, d) {
				excepted[cf.selector] = true
			}
		}
	}
	for _, cf := range e.specific {
		if cf.exception || excepted[cf.selector] {
			continue
		}
		neg := false
		for _, d := range cf.negated {
			if hostMatchesDomain(host, d) {
				neg = true
				break
			}
		}
		if neg {
			continue
		}
		if len(cf.domains) == 0 {
			if !genericHide {
				out = append(out, cf.selector)
			}
			continue
		}
		for _, d := range cf.domains {
			if hostMatchesDomain(host, d) {
				out = append(out, cf.selector)
				break
			}
		}
	}
	if !genericHide {
		if includeAlways {
			for _, s := range e.alwaysGeneric {
				if !excepted[s] {
					out = append(out, s)
				}
			}
		}
		for _, k := range keys {
			for _, s := range e.genericByKey[k] {
				if !excepted[s] {
					out = append(out, s)
				}
			}
		}
	}
	sort.Strings(out)
	uniq := out[:0]
	for i, s := range out {
		if i == 0 || s != out[i-1] {
			uniq = append(uniq, s)
		}
	}
	return uniq
}

// CosmeticCSS renders selectors as one rule each: a single selector a
// browser doesn't understand only drops its own rule, instead of silently
// invalidating one huge comma-joined rule and everything in it.
func CosmeticCSS(selectors []string) string {
	var b strings.Builder
	for _, s := range selectors {
		b.WriteString(s)
		b.WriteString("{display:none!important}\n")
	}
	return b.String()
}
