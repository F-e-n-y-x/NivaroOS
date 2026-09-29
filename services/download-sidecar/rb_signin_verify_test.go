package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeProvider func(*http.Request) *http.Response

func (f fakeProvider) RoundTrip(r *http.Request) (*http.Response, error) { return f(r), nil }

func jsonResp(body string, hdr map[string]string) *http.Response {
	h := http.Header{"Content-Type": {"application/json"}}
	for k, v := range hdr {
		h.Set(k, v)
	}
	return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

func withFakeProvider(t *testing.T, f fakeProvider) {
	old := providerClient
	providerClient = &http.Client{Transport: f}
	t.Cleanup(func() { providerClient = old })
}

// An account on another host of the domain: www answers errno -6 with the
// host to use (what the owner's TeraBox account does), and the check
// follows it instead of calling the sign-in rejected.
func TestVerifyLoginFollowsUrlDomainPrefix(t *testing.T) {
	var hosts []string
	withFakeProvider(t, func(r *http.Request) *http.Response {
		hosts = append(hosts, r.URL.Host)
		if r.URL.Query().Get("app_id") != "250528" || r.Header.Get("X-Requested-With") == "" {
			t.Errorf("call to %s lacks the web client's query or headers", r.URL)
		}
		switch {
		case r.URL.Host == "www.1024terabox.com":
			return jsonResp(`{"errno":-6}`, map[string]string{"Url-Domain-Prefix": "dm"})
		case r.URL.Path == "/api/check/login":
			return jsonResp(`{"errno":0}`, nil)
		default:
			return jsonResp(`{"errno":0,"data":{"display_name":"Alex"}}`, nil)
		}
	})
	ok, account, why := verifyProviderLogin(context.Background(), rbProviders["terabox"], "1024terabox.com", "ndus=x")
	if !ok || account != "Alex" || why != "" {
		t.Fatalf("got ok=%v account=%q why=%q", ok, account, why)
	}
	if hosts[0] != "www.1024terabox.com" || hosts[1] != "dm.1024terabox.com" {
		t.Fatalf("hosts = %v", hosts)
	}
}

// errno 4000023: the call needs the home page's jsToken.
func TestVerifyLoginFetchesJSToken(t *testing.T) {
	withFakeProvider(t, func(r *http.Request) *http.Response {
		switch {
		case r.URL.Path == "/":
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(
				"x `function%20fn%28a%29%7Bwindow.jsToken%20%3D%20a%7D%3Bfn%28%22TOKEN123%22%29` y"))}
		case r.URL.Query().Get("jsToken") == "TOKEN123":
			return jsonResp(`{"errno":0}`, nil)
		default:
			return jsonResp(`{"errno":4000023}`, nil)
		}
	})
	p := rbProviders["terabox"]
	p.InfoPath = ""
	if ok, _, why := verifyProviderLogin(context.Background(), p, "terabox.com", "ndus=x"); !ok {
		t.Fatalf("not ok: %s", why)
	}
}

// A real refusal is reported with its errno and host - never the cookie.
func TestVerifyLoginReportsWhy(t *testing.T) {
	withFakeProvider(t, func(r *http.Request) *http.Response { return jsonResp(`{"errno":-6}`, nil) })
	ok, _, why := verifyProviderLogin(context.Background(), rbProviders["terabox"], "terabox.com", "ndus=secretvalue")
	if ok || why != "errno -6 on www.terabox.com" {
		t.Fatalf("ok=%v why=%q", ok, why)
	}
}

// A Url-Domain-Prefix that isn't one DNS label is ignored: the cookie never
// goes to a host outside the provider's domain.
func TestVerifyLoginIgnoresAHostilePrefix(t *testing.T) {
	for _, prefix := range []string{"evil.example/x?", "evil.example#", "a@evil.example", "evil.example:443", "a.b", "-x", ""} {
		var hosts []string
		withFakeProvider(t, func(r *http.Request) *http.Response {
			hosts = append(hosts, r.URL.Hostname())
			return jsonResp(`{"errno":-6}`, map[string]string{"Url-Domain-Prefix": prefix})
		})
		ok, _, why := verifyProviderLogin(context.Background(), rbProviders["terabox"], "1024terabox.com", "ndus=x")
		if ok || why != "errno -6 on www.1024terabox.com" {
			t.Fatalf("prefix %q: ok=%v why=%q", prefix, ok, why)
		}
		for _, h := range hosts {
			if h != "www.1024terabox.com" {
				t.Fatalf("prefix %q: called %s", prefix, h)
			}
		}
	}
}
