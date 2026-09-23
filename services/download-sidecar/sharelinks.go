package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Google Drive never serves a file at the link people actually share
// (drive.google.com/file/d/<id>/view is the Drive viewer web app), and for
// anything too big to virus-scan (~100 MB+) even the download endpoint
// answers first with an HTML "Download anyway" warning page. Without this,
// a Drive link "downloads" as a 2 KB view.html / download.html.
//
// resolveShareURL maps share links to the direct endpoint up front;
// followInterstitial handles the warning page (and turns Drive's other
// HTML answers - quota exceeded, private file - into real errors).

var driveFileRe = regexp.MustCompile(`^/(?:a/[^/]+/)?file(?:/u/\d+)?/d/([A-Za-z0-9_-]{10,})`)

func driveDirectURL(id, resourceKey string) *url.URL {
	q := url.Values{"id": {id}, "export": {"download"}, "confirm": {"t"}}
	if resourceKey != "" {
		q.Set("resourcekey", resourceKey)
	}
	return &url.URL{Scheme: "https", Host: "drive.usercontent.google.com", Path: "/download", RawQuery: q.Encode()}
}

func resolveShareURL(u *url.URL) *url.URL {
	host := strings.ToLower(u.Hostname())
	switch host {
	case "drive.google.com":
		q := u.Query()
		if m := driveFileRe.FindStringSubmatch(u.Path); m != nil {
			return driveDirectURL(m[1], q.Get("resourcekey"))
		}
		if (u.Path == "/open" || u.Path == "/uc") && q.Get("id") != "" {
			return driveDirectURL(q.Get("id"), q.Get("resourcekey"))
		}
	case "drive.usercontent.google.com":
		q := u.Query()
		if u.Path == "/download" && q.Get("id") != "" && q.Get("confirm") == "" {
			return driveDirectURL(q.Get("id"), q.Get("resourcekey"))
		}
	}
	return u
}

func isGoogleDriveHost(host string) bool {
	host = strings.ToLower(host)
	return host == "drive.google.com" || host == "drive.usercontent.google.com" || host == "docs.google.com"
}

var errDrivePrivate = errors.New("Google Drive: this file isn't shared publicly - open it in the Download Station browser, sign in there, then click Download")
var errDriveQuota = errors.New("Google Drive: too many people have downloaded this file recently (download quota exceeded) - try again later")

// followInterstitial inspects an HTML response to what should have been a
// file. For a Drive warning page it returns the URL its "Download anyway"
// form submits to; for Drive's error pages it returns a descriptive error;
// otherwise (nil, nil) - the caller keeps the response as is. The body is
// consumed and closed only when a new URL or error is returned.
func followInterstitial(resp *http.Response) (*url.URL, error) {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(ct, "text/html") || !isGoogleDriveHost(resp.Request.URL.Hostname()) {
		return nil, nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	resp.Body.Close()
	if action := parseDownloadForm(raw, resp.Request.URL); action != nil {
		return action, nil
	}
	lower := bytes.ToLower(raw)
	switch {
	case bytes.Contains(lower, []byte("quota")) || bytes.Contains(lower, []byte("too many users")):
		return nil, errDriveQuota
	case bytes.Contains(lower, []byte("servicelogin")) || bytes.Contains(lower, []byte("request access")) || bytes.Contains(lower, []byte("you need access")):
		return nil, errDrivePrivate
	}
	return nil, errors.New("Google Drive returned a web page instead of the file - open the link in the Download Station browser and use its Download button")
}

// parseDownloadForm finds Drive's <form id="download-form"> and builds
// the GET URL it would submit (action + hidden inputs).
func parseDownloadForm(raw []byte, base *url.URL) *url.URL {
	z := html.NewTokenizer(bytes.NewReader(raw))
	var action string
	inForm := false
	q := url.Values{}
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt == html.EndTagToken {
			if name, _ := z.TagName(); inForm && atom.Lookup(name) == atom.Form {
				break
			}
			continue
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		tok := z.Token()
		attr := func(k string) string {
			for _, a := range tok.Attr {
				if a.Key == k {
					return a.Val
				}
			}
			return ""
		}
		switch tok.DataAtom {
		case atom.Form:
			if attr("id") == "download-form" {
				inForm = true
				action = attr("action")
			}
		case atom.Input:
			if inForm && attr("type") == "hidden" && attr("name") != "" {
				q.Set(attr("name"), attr("value"))
			}
		}
	}
	if !inForm || action == "" {
		return nil
	}
	u, err := base.Parse(action)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil
	}
	u.RawQuery = q.Encode()
	return u
}
