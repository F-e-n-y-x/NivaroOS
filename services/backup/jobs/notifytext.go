package jobs

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The service has no i18n, but notifications need an English sentence
// (spec §12.12: "Notification text is rendered from en_US"). notify_en.json
// is the part of ui/src/assets/lang/en_US.json that notifications use,
// written by cmd/gen-notifytext (go generate ./jobs) and embedded, so the
// installed binary needs no UI files. TestNotifyTextInSync fails when it
// drifts from en_US.json.

//go:generate go run ../cmd/gen-notifytext

//go:embed notify_en.json
var notifyEnJSON []byte

// NotifyTextPrefixes are the en_US.json key prefixes notify_en.json keeps.
var NotifyTextPrefixes = []string{"backup.notify.", "backup.guard.", "backup.status.", "backup.ep.", "backup.app.title"}

// KeepNotifyText reports whether an en_US.json key belongs in
// notify_en.json: the prefixes above plus every error title (reason_key
// args point at those).
func KeepNotifyText(key string) bool {
	if strings.HasPrefix(key, "backup.err.") && strings.HasSuffix(key, ".title") {
		return true
	}
	for _, p := range NotifyTextPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// RenderNotifyText renders notify_en.json from a parsed en_US.json.
func RenderNotifyText(enUS map[string]string) []byte {
	keys := make([]string, 0, len(enUS))
	for k := range enUS {
		if KeepNotifyText(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{\n")
	enc := func(v string) string {
		var buf bytes.Buffer
		e := json.NewEncoder(&buf)
		e.SetEscapeHTML(false) // keep "&" readable, like en_US.json
		_ = e.Encode(v)
		return strings.TrimSuffix(buf.String(), "\n")
	}
	for i, k := range keys {
		fmt.Fprintf(&b, "  %s: %s", enc(k), enc(enUS[k]))
		if i < len(keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

var notifyText = func() map[string]string {
	m := map[string]string{}
	if err := json.Unmarshal(notifyEnJSON, &m); err != nil {
		panic("notify_en.json: " + err.Error())
	}
	return m
}()

var placeholderRe = regexp.MustCompile(`\{([a-z_]+)\}`)

// RenderEnglish renders a translatable message in English the way the UI
// would (Message's arg conventions): *_key args are looked up first,
// bytes / *_bytes are formatted as sizes and at / *_at as server-local
// times. An unknown key renders as the key itself.
func RenderEnglish(m Message) string {
	text, ok := notifyText[m.Key]
	if !ok {
		return m.Key
	}
	return placeholderRe.ReplaceAllStringFunc(text, func(ph string) string {
		name := ph[1 : len(ph)-1]
		v, ok := m.Args[name]
		if !ok {
			return ph
		}
		return renderArg(name, v)
	})
}

func renderArg(name string, v interface{}) string {
	switch {
	case strings.HasSuffix(name, "_key"):
		if s, ok := v.(string); ok {
			if t, ok := notifyText[s]; ok {
				return t
			}
			return s
		}
	case name == "bytes" || strings.HasSuffix(name, "_bytes"):
		if n, ok := toInt64(v); ok {
			return humanBytes(n)
		}
	case name == "at" || strings.HasSuffix(name, "_at"):
		if s, ok := v.(string); ok {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t.In(time.Local).Format("2006-01-02 15:04")
			}
		}
	}
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	}
	return fmt.Sprint(v)
}

func toInt64(v interface{}) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		return int64(x), true
	case json.Number:
		n, err := x.Int64()
		return n, err == nil
	}
	return 0, false
}

// humanBytes formats a size the way the UI does (decimal units).
func humanBytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 5; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}
