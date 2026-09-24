package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// cronPreviewCount is how many next activations POST /cron/preview lists.
const cronPreviewCount = 5

// PreviewCron answers POST /cron/preview: whether spec parses, a
// translatable description (one of CronHumanKeys) and the next five
// activations in the server's time zone. An invalid spec is not an API
// error: Valid=false and Error holds the parser's message, shown inline.
func PreviewCron(spec string, now time.Time) CronPreview {
	tz, off := ServerTimezone(now)
	p := CronPreview{Args: map[string]interface{}{}, Next: []time.Time{}, Timezone: tz, UTCOffset: off}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		p.Error = "empty cron expression"
		return p
	}
	sched, err := ParseSchedule(spec)
	if err != nil {
		p.Error = err.Error()
		return p
	}
	p.Valid = true
	t := now
	for i := 0; i < cronPreviewCount; i++ {
		t = sched.Next(t)
		if t.IsZero() {
			break // a spec that never fires again (e.g. Feb 30)
		}
		p.Next = append(p.Next, t)
	}
	p.HumanKey, p.Args = HumanizeCron(spec)
	return p
}

// HumanizeCron maps a valid spec onto one of CronHumanKeys with its args;
// anything the builder can't describe is "backup.cron.custom" with the
// expression itself, so the UI never guesses.
func HumanizeCron(spec string) (string, map[string]interface{}) {
	custom := func() (string, map[string]interface{}) {
		return "backup.cron.custom", map[string]interface{}{"expr": spec}
	}
	s := strings.TrimSpace(spec)
	switch strings.ToLower(s) {
	case "@hourly":
		return "backup.cron.hourly_at", map[string]interface{}{"minute": 0}
	case "@daily", "@midnight":
		return "backup.cron.daily_at", map[string]interface{}{"time": "00:00"}
	case "@weekly":
		return "backup.cron.weekly_at", map[string]interface{}{"day": 0, "time": "00:00"}
	case "@monthly":
		return "backup.cron.monthly_at", map[string]interface{}{"dom": 1, "time": "00:00"}
	}
	if strings.HasPrefix(s, "@") {
		return custom()
	}
	f := strings.Fields(s)
	if len(f) != 5 {
		return custom()
	}
	min, hour, dom, mon, dow := f[0], f[1], f[2], f[3], f[4]
	if mon != "*" {
		return custom()
	}
	m, mOK := cronNumber(min, 0, 59)
	h, hOK := cronNumber(hour, 0, 23)
	clock := func() string { return fmt.Sprintf("%02d:%02d", h, m) }

	switch {
	case min == "*" && hour == "*" && dom == "*" && dow == "*":
		return "backup.cron.every_minute", nil
	case hour == "*" && dom == "*" && dow == "*":
		if n, ok := cronStep(min, 59); ok {
			return "backup.cron.every_n_minutes", map[string]interface{}{"n": n}
		}
		if mOK {
			return "backup.cron.hourly_at", map[string]interface{}{"minute": m}
		}
	case mOK && dom == "*" && dow == "*":
		if n, ok := cronStep(hour, 23); ok {
			return "backup.cron.every_n_hours", map[string]interface{}{"n": n, "minute": m}
		}
		if hOK {
			return "backup.cron.daily_at", map[string]interface{}{"time": clock()}
		}
	case mOK && hOK && dom == "*":
		days, ok := cronWeekdays(dow)
		if !ok {
			return custom()
		}
		if len(days) == 1 {
			return "backup.cron.weekly_at", map[string]interface{}{"day": days[0], "time": clock()}
		}
		strs := make([]string, len(days))
		for i, d := range days {
			strs[i] = strconv.Itoa(d)
		}
		return "backup.cron.weekdays_at", map[string]interface{}{"days": strings.Join(strs, ","), "time": clock()}
	case mOK && hOK && dow == "*":
		if d, ok := cronNumber(dom, 1, 31); ok {
			return "backup.cron.monthly_at", map[string]interface{}{"dom": d, "time": clock()}
		}
	}
	return custom()
}

// cronNumber parses a plain number field within [lo, hi].
func cronNumber(field string, lo, hi int) (int, bool) {
	n, err := strconv.Atoi(field)
	if err != nil || n < lo || n > hi {
		return 0, false
	}
	return n, true
}

// cronStep parses "*/n" (or "0/n") with 2 <= n <= max.
func cronStep(field string, max int) (int, bool) {
	base, step, ok := strings.Cut(field, "/")
	if !ok || (base != "*" && base != "0") {
		return 0, false
	}
	n, err := strconv.Atoi(step)
	if err != nil || n < 2 || n > max {
		return 0, false
	}
	return n, true
}

var weekdayNames = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

// cronWeekdays expands a day-of-week field ("1-5", "MON,WED", "0,6", "7")
// into sorted distinct weekday numbers, 0 = Sunday.
func cronWeekdays(field string) ([]int, bool) {
	seen := [7]bool{}
	one := func(s string) (int, bool) {
		if n, ok := weekdayNames[strings.ToLower(s)]; ok {
			return n, true
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 || n > 7 {
			return 0, false
		}
		return n % 7, true
	}
	for _, part := range strings.Split(field, ",") {
		if strings.Contains(part, "/") || part == "*" || part == "?" {
			return nil, false
		}
		lo, hi, isRange := strings.Cut(part, "-")
		a, ok := one(lo)
		if !ok {
			return nil, false
		}
		if !isRange {
			seen[a] = true
			continue
		}
		b, ok := one(hi)
		if !ok {
			return nil, false
		}
		if hi == "7" {
			b = 7 // "5-7" is Friday to Sunday
		}
		if b < a {
			return nil, false
		}
		for d := a; d <= b; d++ {
			seen[d%7] = true
		}
	}
	var out []int
	for d, on := range seen {
		if on {
			out = append(out, d)
		}
	}
	return out, len(out) > 0
}

// ServerTimezone returns the server's IANA zone name ("Europe/Berlin")
// and the UTC offset at t ("+02:00"). The name comes from $TZ, the
// /etc/localtime symlink or /etc/timezone, whichever names a zone; the
// Go runtime only knows it as "Local".
func ServerTimezone(t time.Time) (string, string) {
	off := t.In(time.Local).Format("-07:00")
	if name := time.Local.String(); name != "" && name != "Local" {
		return name, off
	}
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" && !strings.HasPrefix(tz, "/") {
		return tz, off
	}
	if target, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if _, zone, ok := strings.Cut(target, "zoneinfo/"); ok && zone != "" {
			return zone, off
		}
	}
	if raw, err := os.ReadFile("/etc/timezone"); err == nil {
		if zone := strings.TrimSpace(string(raw)); zone != "" {
			return zone, off
		}
	}
	return "UTC", off
}
