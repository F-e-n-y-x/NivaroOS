package main

// Sign-in windows: the real browser opened in a throwaway cookie jar
// (Target.createBrowserContext) on a cloud provider's login page. The user
// signs in however the site allows (password, Google, QR code from the
// phone app); once the provider's login cookie appears and a check against
// the provider's own API confirms it, the UI offers "Connect", which
// exports only that provider's cookies (POST /rb/cookies/export) into the
// existing Online Accounts flow. Nothing else can read cookies out of the
// browser.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type rbProvider struct {
	ID         string
	Name       string
	LoginURL   string
	Domains    []string // registrable domains whose cookies may be exported
	AuthCookie string   // the cookie that means "signed in"
	CheckPath  string   // GET on https://www.<domain>, errno 0 when signed in
	InfoPath   string   // optional: account name
}

var rbProviders = map[string]rbProvider{
	"terabox": {
		ID:       "terabox",
		Name:     "TeraBox",
		LoginURL: "https://www.terabox.com/",
		// TeraBox moves accounts between these region domains.
		Domains: []string{"terabox.com", "1024terabox.com", "terabox.app", "teraboxapp.com", "1024tera.com", "freeterabox.com",
			"4funbox.com", "mirrobox.com", "nephobox.com", "momerybox.com", "tibibox.com", "teraboxlink.com", "terafileshare.com"},
		AuthCookie: "ndus",
		CheckPath:  "/api/check/login",
		InfoPath:   "/passport/get_info",
	},
}

type rbSignin struct {
	context  string
	provider rbProvider
	viewer   *rbViewer

	mu        sync.Mutex
	state     string // waiting | verifying | signed_in
	account   string
	checked   string // auth cookie value last verified
	lastStuck bool
	cookie    string // export form, once verified
	domain    string
	stop      chan struct{}
	stopOnce  sync.Once

	diag rbSigninDiag
}

// rbSigninDiag is what a sign-in window noticed, for "Sign-in didn't
// stick?" and the service log. It holds cookie names and domains only -
// never a cookie's value.
type rbSigninDiag struct {
	started  time.Time
	loads    int                        // main-frame navigations
	seen     map[string]map[string]bool // domain -> cookie names in the jar
	blocked  map[string]bool            // "name@domain: reason" Chrome refused
	fails    int                        // auth cookie present but the provider said no
	lostAuth bool                       // the auth cookie appeared, then went away
	hadAuth  bool
	logged   bool
}

// stuck: the signs of a login loop - the provider rejected the cookie
// twice, it vanished after appearing, Chrome refused one of the provider's
// cookies, or the login page kept reloading for a while without one.
func (d *rbSigninDiag) stuck(now time.Time) bool {
	if d.fails >= 2 || d.lostAuth || len(d.blocked) > 0 {
		return true
	}
	return !d.hadAuth && d.loads >= 4 && now.Sub(d.started) > 45*time.Second
}

func (d *rbSigninDiag) report(now time.Time) map[string]interface{} {
	seen := make([]string, 0, len(d.seen))
	for dom, names := range d.seen {
		ns := make([]string, 0, len(names))
		for n := range names {
			ns = append(ns, n)
		}
		sort.Strings(ns)
		seen = append(seen, dom+": "+strings.Join(ns, ", "))
	}
	sort.Strings(seen)
	blocked := make([]string, 0, len(d.blocked))
	for b := range d.blocked {
		blocked = append(blocked, b)
	}
	sort.Strings(blocked)
	return map[string]interface{}{
		"stuck": d.stuck(now), "seen": seen, "blocked": blocked, "loads": d.loads,
		"rejected": d.fails, "lost": d.lostAuth, "seconds": int(now.Sub(d.started).Seconds()),
	}
}

// noteCookies records the jar's cookie names per domain (values dropped).
func (d *rbSigninDiag) noteCookies(all []cdpCookie) {
	if d.seen == nil {
		d.seen = map[string]map[string]bool{}
	}
	for _, c := range all {
		dom := strings.TrimPrefix(strings.ToLower(c.Domain), ".")
		if dom == "" || c.Name == "" {
			continue
		}
		if d.seen[dom] == nil {
			if len(d.seen) >= 40 {
				continue
			}
			d.seen[dom] = map[string]bool{}
		}
		if len(d.seen[dom]) < 40 {
			d.seen[dom][rbText(c.Name, 60)] = true
		}
	}
}

func cookieInDomains(c cdpCookie, domains []string) (string, bool) {
	host := strings.TrimPrefix(strings.ToLower(c.Domain), ".")
	for _, d := range domains {
		if hostMatchesDomain(host, d) {
			return d, true
		}
	}
	return "", false
}

// providerCookie picks, from all of a jar's cookies, the provider's cookies
// for the domain its auth cookie is on, joined as a Cookie header.
func providerCookie(p rbProvider, all []cdpCookie) (cookie, domain, auth string) {
	for _, c := range all {
		if c.Name != p.AuthCookie || c.Value == "" {
			continue
		}
		if d, ok := cookieInDomains(c, p.Domains); ok {
			domain, auth = d, c.Value
			break
		}
	}
	if domain == "" {
		return "", "", ""
	}
	seen := map[string]bool{}
	var parts []string
	// The auth cookie first: valuedCookie() and people reading the config
	// both expect ndus=... up front.
	parts = append(parts, p.AuthCookie+"="+auth)
	seen[p.AuthCookie] = true
	for _, c := range all {
		if seen[c.Name] || c.Value == "" {
			continue
		}
		if d, ok := cookieInDomains(c, p.Domains); !ok || d != domain {
			continue
		}
		if strings.ContainsAny(c.Value, ";\r\n") || strings.ContainsAny(c.Name, "=;\r\n ") {
			continue
		}
		seen[c.Name] = true
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; "), domain, auth
}

func (i *rbInstance) startSignin(v *rbViewer, provider string) error {
	p, ok := rbProviders[provider]
	if !ok {
		return errors.New("unknown provider")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	i.mu.Lock()
	conn := i.cdp
	i.mu.Unlock()
	var bc struct {
		BrowserContextID string `json:"browserContextId"`
	}
	if err := conn.Call(ctx, "", "Target.createBrowserContext", map[string]interface{}{"disposeOnDetach": false}, &bc); err != nil {
		return err
	}
	s := &rbSignin{context: bc.BrowserContextID, provider: p, viewer: v, state: "waiting", stop: make(chan struct{})}
	s.diag.started = time.Now()
	i.mu.Lock()
	i.signins[s.context] = s
	i.mu.Unlock()
	v.context = s.context
	// Nothing downloads from a sign-in window.
	conn.Send("", "Browser.setDownloadBehavior", map[string]interface{}{"behavior": "deny", "browserContextId": s.context})
	t, err := i.newTab(ctx, p.LoginURL, s.context, 0)
	if err != nil {
		i.endSignin(s.context)
		return err
	}
	v.activate(t.ID)
	v.sendJSON(map[string]interface{}{"t": "signin", "provider": p.ID, "state": "waiting", "context": s.context})
	go func() {
		tk := time.NewTicker(1500 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-tk.C:
				i.checkSignin(s.context, false)
			}
		}
	}()
	return nil
}

func (i *rbInstance) endSignin(ctxID string) {
	i.mu.Lock()
	s := i.signins[ctxID]
	delete(i.signins, ctxID)
	var gone []*rbTab
	for _, t := range i.tabs {
		if t.Context == ctxID {
			gone = append(gone, t)
		}
	}
	for _, t := range gone {
		i.removeTabLocked(t)
	}
	conn := i.cdp
	i.mu.Unlock()
	if s != nil {
		s.stopOnce.Do(func() { close(s.stop) })
		s.mu.Lock()
		if s.state != "signed_in" && (s.diag.loads > 0 || len(s.diag.seen) > 0) {
			log.Printf("ds-browser: %s sign-in window closed without a sign-in: %v", s.provider.Name, s.diag.report(time.Now()))
		}
		s.mu.Unlock()
	}
	if conn != nil && ctxID != "" {
		conn.Send("", "Target.disposeBrowserContext", map[string]interface{}{"browserContextId": ctxID})
	}
	i.updateCasts()
}

func (i *rbInstance) jarCookies(ctx context.Context, ctxID string) ([]cdpCookie, error) {
	i.mu.Lock()
	conn := i.cdp
	i.mu.Unlock()
	params := map[string]interface{}{}
	if ctxID != "" {
		params["browserContextId"] = ctxID
	}
	var res struct {
		Cookies []cdpCookie `json:"cookies"`
	}
	if err := conn.Call(ctx, "", "Storage.getCookies", params, &res); err != nil {
		return nil, err
	}
	return res.Cookies, nil
}

// checkSignin looks for the provider's login cookie and, when a new one
// appears, confirms it against the provider's API.
func (i *rbInstance) checkSignin(ctxID string, force bool) {
	i.mu.Lock()
	s := i.signins[ctxID]
	i.mu.Unlock()
	if s == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	all, err := i.jarCookies(ctx, ctxID)
	if err != nil {
		return
	}
	cookie, domain, auth := providerCookie(s.provider, all)
	now := time.Now()
	s.mu.Lock()
	s.diag.noteCookies(all)
	if auth == "" {
		was := s.state
		if s.diag.hadAuth {
			s.diag.lostAuth = true
		}
		s.state, s.cookie, s.checked = "waiting", "", ""
		wasStuck := s.lastStuck
		s.lastStuck = s.diag.stuck(now)
		nowStuck := s.lastStuck
		diag := s.diag.report(now)
		logIt := s.lastStuck && !s.diag.logged
		if logIt {
			s.diag.logged = true
		}
		s.mu.Unlock()
		if logIt {
			log.Printf("ds-browser: %s sign-in looks stuck: %v", s.provider.Name, diag)
		}
		if was != "waiting" || nowStuck != wasStuck {
			s.viewer.sendJSON(map[string]interface{}{"t": "signin", "provider": s.provider.ID, "state": "waiting", "context": s.context, "diag": diag})
		}
		return
	}
	s.diag.hadAuth = true
	if auth == s.checked && !force {
		s.mu.Unlock()
		return
	}
	s.checked = auth
	s.state = "verifying"
	s.mu.Unlock()
	s.viewer.sendJSON(map[string]interface{}{"t": "signin", "provider": s.provider.ID, "state": "verifying", "context": s.context})
	ok, account := verifyProviderLogin(ctx, s.provider, domain, cookie)
	s.mu.Lock()
	if ok {
		s.state, s.cookie, s.domain, s.account = "signed_in", cookie, domain, account
	} else {
		s.state = "waiting"
		s.checked = "" // try again on the next poll: the site may still be setting cookies
		s.diag.fails++
	}
	state := s.state
	s.lastStuck = s.diag.stuck(time.Now())
	diag := s.diag.report(time.Now())
	s.mu.Unlock()
	s.viewer.sendJSON(map[string]interface{}{"t": "signin", "provider": s.provider.ID, "state": state, "account": account, "domain": domain, "context": s.context, "diag": diag})
}

var providerClient = &http.Client{Transport: newTransport(false), Timeout: 12 * time.Second}

// verifyProviderLogin asks the provider (through the netguarded transport)
// whether cookie is a signed-in session, and for the account's name.
func verifyProviderLogin(ctx context.Context, p rbProvider, domain, cookie string) (bool, string) {
	get := func(path string) map[string]interface{} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www."+domain+path, nil)
		if err != nil {
			return nil
		}
		req.Header.Set("Cookie", cookie)
		req.Header.Set("User-Agent", defaultUserAgent)
		req.Header.Set("Accept", "application/json")
		resp, err := providerClient.Do(req)
		if err != nil {
			return nil
		}
		defer resp.Body.Close()
		var out map[string]interface{}
		if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out) != nil {
			return nil
		}
		return out
	}
	res := get(p.CheckPath)
	if res == nil || !errnoZero(res["errno"]) {
		return false, ""
	}
	account := ""
	if p.InfoPath != "" {
		if info := get(p.InfoPath); info != nil {
			account = findAccountName(info)
		}
	}
	return true, account
}

func errnoZero(v interface{}) bool {
	switch n := v.(type) {
	case float64:
		return n == 0
	case string:
		return n == "0"
	}
	return false
}

// findAccountName digs a display name out of a provider's user-info reply.
func findAccountName(m map[string]interface{}) string {
	var walk func(v interface{}, depth int) string
	walk = func(v interface{}, depth int) string {
		if depth > 3 {
			return ""
		}
		obj, ok := v.(map[string]interface{})
		if !ok {
			return ""
		}
		for _, k := range []string{"display_name", "displayName", "username", "uname", "nick_name", "nickname", "email"} {
			if s, ok := obj[k].(string); ok && strings.TrimSpace(s) != "" {
				return rbText(strings.TrimSpace(s), 120)
			}
		}
		for _, k := range []string{"data", "user", "records", "user_info"} {
			if s := walk(obj[k], depth+1); s != "" {
				return s
			}
		}
		return ""
	}
	return walk(m, 0)
}

type rbCookieExport struct {
	Cookie      string   `json:"cookie"`
	AccountHint string   `json:"account_hint"`
	Domains     []string `json:"domains"`
}

// exportCookies returns a provider's login cookie from a verified sign-in
// window (context), or, with no context, from the user's normal browser
// profile if they are already signed in there.
func (i *rbInstance) exportCookies(ctx context.Context, provider, ctxID string) (*rbCookieExport, error) {
	p, ok := rbProviders[provider]
	if !ok {
		return nil, errors.New("unknown provider")
	}
	if ctxID != "" {
		i.mu.Lock()
		s := i.signins[ctxID]
		i.mu.Unlock()
		if s == nil {
			return nil, errors.New("that sign-in window is closed")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.state != "signed_in" || s.cookie == "" {
			return nil, errors.New("not signed in yet")
		}
		return &rbCookieExport{Cookie: s.cookie, AccountHint: s.account, Domains: []string{s.domain}}, nil
	}
	all, err := i.jarCookies(ctx, "")
	if err != nil {
		return nil, err
	}
	cookie, domain, _ := providerCookie(p, all)
	if cookie == "" {
		return nil, errors.New("you are not signed in to " + p.Name + " in the browser")
	}
	okLogin, account := verifyProviderLogin(ctx, p, domain, cookie)
	if !okLogin {
		return nil, errors.New(p.Name + " says that sign-in has expired - sign in again")
	}
	return &rbCookieExport{Cookie: cookie, AccountHint: account, Domains: []string{domain}}, nil
}

// signinOf returns the sign-in window a tab belongs to, if any.
func (i *rbInstance) signinOf(t *rbTab) *rbSignin {
	if t == nil || t.Context == "" {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.signins[t.Context]
}

// noteSigninNetwork records, for a sign-in window, cookies Chrome refused to
// store (Network.responseReceivedExtraInfo) or send
// (Network.requestWillBeSentExtraInfo) - the usual cause of a login that
// "just reloads". Only names, domains and Chrome's reasons are kept.
func (i *rbInstance) noteSigninNetwork(t *rbTab, method string, raw json.RawMessage) {
	s := i.signinOf(t)
	if s == nil {
		return
	}
	var p struct {
		Blocked []struct {
			Reasons []string `json:"blockedReasons"`
			Line    string   `json:"cookieLine"`
			Cookie  *struct {
				Name   string `json:"name"`
				Domain string `json:"domain"`
			} `json:"cookie"`
		} `json:"blockedCookies"`
		Associated []struct {
			Reasons []struct {
				Reason string `json:"exclusionReason"`
			} `json:"blockedReasons"`
			Cookie struct {
				Name   string `json:"name"`
				Domain string `json:"domain"`
			} `json:"cookie"`
		} `json:"associatedCookies"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	var add []string
	for _, b := range p.Blocked {
		name, dom := "", ""
		if b.Cookie != nil {
			name, dom = b.Cookie.Name, b.Cookie.Domain
		} else if j := strings.IndexByte(b.Line, '='); j > 0 {
			name = b.Line[:j]
		}
		if name != "" && len(b.Reasons) > 0 {
			add = append(add, rbText(name, 60)+"@"+rbText(strings.TrimPrefix(dom, "."), 80)+" (set): "+strings.Join(b.Reasons, ","))
		}
	}
	for _, a := range p.Associated {
		var rs []string
		for _, r := range a.Reasons {
			// A cookie simply not matching this URL is not a problem.
			if r.Reason != "" && r.Reason != "ExcludeDomainMismatch" && r.Reason != "ExcludeNotOnPath" {
				rs = append(rs, r.Reason)
			}
		}
		if a.Cookie.Name != "" && len(rs) > 0 {
			add = append(add, rbText(a.Cookie.Name, 60)+"@"+rbText(strings.TrimPrefix(a.Cookie.Domain, "."), 80)+" (send): "+strings.Join(rs, ","))
		}
	}
	if len(add) == 0 {
		return
	}
	s.mu.Lock()
	if s.diag.blocked == nil {
		s.diag.blocked = map[string]bool{}
	}
	for _, a := range add {
		// Only the provider's own cookies matter for the sign-in.
		dom := a[strings.IndexByte(a, '@')+1:]
		if j := strings.IndexByte(dom, ' '); j >= 0 {
			dom = dom[:j]
		}
		if dom != "" && !anyDomainMatch(dom, s.provider.Domains) {
			continue
		}
		if len(s.diag.blocked) < 40 {
			s.diag.blocked[a] = true
		}
	}
	s.mu.Unlock()
}

func anyDomainMatch(host string, domains []string) bool {
	for _, d := range domains {
		if hostMatchesDomain(strings.ToLower(host), d) {
			return true
		}
	}
	return false
}

// noteSigninLoad counts a sign-in window's main-frame navigations.
func (i *rbInstance) noteSigninLoad(t *rbTab) {
	if s := i.signinOf(t); s != nil {
		s.mu.Lock()
		s.diag.loads++
		s.mu.Unlock()
	}
}
