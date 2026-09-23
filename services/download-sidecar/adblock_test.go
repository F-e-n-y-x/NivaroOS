package main

import (
	"strings"
	"testing"
)

const testList = `! Title: test
||ads.example.com^
||tracker.net^$third-party
/banner/*/ad_$image
@@||ads.example.com/allowed/*
||cdn.site.com/popunder.js$script,domain=site.com|~good.site.com
||important.ads^$important
@@||important.ads^
||evil.com^$doc
/ads-[0-9]+\.js/
||badfilter-target.com^
||badfilter-target.com^$badfilter
||x.com^$removeparam=utm
0.0.0.0 hosts-style.example
example.com##.promo-box
~sub.example.com,example.com##.only-root
##.ad-banner
###sidebar-ad
##a[href^="https://click.example/"]
example.com#@#.ad-banner
site.com##+js(nobab)
site.com##div:has-text(Sponsored)
@@||nohide.org^$generichide
`

func TestNetworkFilters(t *testing.T) {
	e := BuildFilterEngine(testList)
	cases := []struct {
		url, page string
		typ       resType
		want      bool
	}{
		{"https://ads.example.com/x.js", "news.org", typeScript, true},
		{"https://sub.ads.example.com/x.js", "news.org", typeScript, true},
		{"https://notads.example.com/x.js", "news.org", typeScript, false},
		{"https://ads.example.com/allowed/x.js", "news.org", typeScript, false},
		{"https://tracker.net/p.gif", "news.org", typeImage, true},
		{"https://tracker.net/p.gif", "tracker.net", typeImage, false},
		{"https://img.org/banner/300/ad_1.png", "news.org", typeImage, true},
		{"https://img.org/banner/300/ad_1.png", "news.org", typeScript, false},
		{"https://cdn.site.com/popunder.js", "site.com", typeScript, true},
		{"https://cdn.site.com/popunder.js", "good.site.com", typeScript, false},
		{"https://cdn.site.com/popunder.js", "other.com", typeScript, false},
		{"https://important.ads/a", "news.org", typeScript, true},
		{"https://evil.com/", "evil.com", typeDocument, true},
		{"https://ads.example.com/", "ads.example.com", typeDocument, true},
		{"https://news.org/", "news.org", typeDocument, false},
		{"https://x.org/ads-123.js", "news.org", typeScript, true},
		{"https://badfilter-target.com/a", "news.org", typeScript, false},
		{"https://x.com/a", "news.org", typeScript, false},
		{"https://hosts-style.example/a", "news.org", typeImage, true},
	}
	for _, c := range cases {
		host := hostOf(c.url)
		got := e.Match(RequestInfo{URL: c.url, Host: host, PageHost: c.page, Type: c.typ}).Blocked
		if got != c.want {
			t.Errorf("%s on %s (type %d): blocked=%v, want %v", c.url, c.page, c.typ, got, c.want)
		}
	}
}

func hostOf(u string) string {
	u = u[strings.Index(u, "://")+3:]
	if i := strings.IndexAny(u, "/?"); i >= 0 {
		u = u[:i]
	}
	return u
}

func TestCosmeticFilters(t *testing.T) {
	e := BuildFilterEngine(testList)
	sel := func(host string, keys ...string) map[string]bool {
		m := map[string]bool{}
		for _, s := range e.SelectorsForPage("https://"+host+"/", host, keys, true) {
			m[s] = true
		}
		return m
	}
	s := sel("news.org", ".ad-banner", "#sidebar-ad", ".unrelated")
	if !s[".ad-banner"] || !s["#sidebar-ad"] || !s[`a[href^="https://click.example/"]`] {
		t.Errorf("generic selectors missing: %v", s)
	}
	if s[".promo-box"] {
		t.Error("site-specific selector leaked to another site")
	}
	if s := sel("news.org", ".unrelated"); s[".ad-banner"] {
		t.Error("keyed generic selector injected although page never uses the class")
	}
	s = sel("example.com", ".ad-banner")
	if !s[".promo-box"] || !s[".only-root"] {
		t.Errorf("specific selectors missing: %v", s)
	}
	if s[".ad-banner"] {
		t.Error("#@# exception not honoured")
	}
	if s := sel("sub.example.com"); s[".only-root"] {
		t.Error("~domain negation not honoured")
	}
	if s := sel("nohide.org", ".ad-banner"); s[".ad-banner"] {
		t.Error("$generichide not honoured")
	}
	for k := range sel("site.com") {
		if strings.Contains(k, "has-text") || strings.Contains(k, "+js") {
			t.Errorf("unsupported filter leaked: %s", k)
		}
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		p, s string
		end  bool
		want bool
	}{
		{"ads^", "ads/x", false, true},
		{"ads^", "ads", false, true},
		{"ads^", "adsx", false, false},
		{"a*c", "abbbc", true, true},
		{"a*c", "abbbcd", true, false},
		{"/ad_", "/ad_1", false, true},
	}
	for _, c := range cases {
		if got := globMatch(c.p, c.s, c.end); got != c.want {
			t.Errorf("globMatch(%q,%q,%v)=%v", c.p, c.s, c.end, got)
		}
	}
}

func TestEntityDomains(t *testing.T) {
	if !hostMatchesDomain("www.example.co.uk", "example.*") || !hostMatchesDomain("example.com", "example.*") {
		t.Error("entity match failed")
	}
	if hostMatchesDomain("example.com.evil.net", "example.*") {
		t.Error("entity over-matched")
	}
}
