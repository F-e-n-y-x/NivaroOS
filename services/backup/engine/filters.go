package engine

import (
	"context"
	"strings"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/filter"
)

// excludePresetRules are the named exclude sets of Filters.ExcludePresets.
// Patterns without a leading "/" match at any depth.
var excludePresetRules = map[string][]string{
	ExcludePresetCaches:      {".cache/**", "Cache/**", "cache/**", "Caches/**", "__pycache__/**", ".gradle/caches/**"},
	ExcludePresetTrash:       {".Trash/**", ".Trash-*/**", ".Trashes/**", "$RECYCLE.BIN/**", ".recycle/**", "@Recycle/**", "#recycle/**"},
	ExcludePresetTemp:        {"*.tmp", "*.temp", "~$*", "*.part", "*.partial", "*.crdownload", ".~lock.*#", "*.swp"},
	ExcludePresetThumbs:      {"Thumbs.db", "ehthumbs.db", "desktop.ini", ".DS_Store", "._*", ".thumbnails/**", ".AppleDouble/**", "@eaDir/**"},
	ExcludePresetNodeModules: {"node_modules/**"},
}

// filterSpec is everything that goes into one rclone filter.
type filterSpec struct {
	f Filters
	// protectDest adds the rules that keep a mirror from touching its own
	// recycle folder and marker (spec §6.4), before any user rule.
	protectDest bool
	// excludeDirs are extra folders (relative to the source root) left
	// out, e.g. a destination that sits inside the source.
	excludeDirs []string
}

// rules renders the spec as rclone filter rules ("- glob" / "+ glob"),
// in the order they apply.
func (s filterSpec) rules() ([]string, error) {
	var r []string
	if s.protectDest {
		r = append(r, "- /"+escapeGlob(VersionsDir)+"/**", "- /"+escapeGlob(MarkerFile))
	}
	for _, d := range s.excludeDirs {
		r = append(r, "- /"+escapeGlob(d)+"/**")
	}
	for _, p := range s.f.ExcludePresets {
		globs, ok := excludePresetRules[p]
		if !ok {
			return nil, Errorf(CodeInvalidFilter, "unknown exclude preset %q", p)
		}
		for _, g := range globs {
			r = append(r, "- "+g)
		}
	}
	for _, g := range s.f.Exclude {
		if err := checkGlob(g); err != nil {
			return nil, err
		}
		r = append(r, "- "+g)
	}
	if len(s.f.Include) > 0 {
		for _, g := range s.f.Include {
			if err := checkGlob(g); err != nil {
				return nil, err
			}
			r = append(r, "+ "+g)
		}
		r = append(r, "- **")
	}
	return r, nil
}

func checkGlob(g string) error {
	if strings.TrimSpace(g) == "" {
		return Errorf(CodeInvalidFilter, "empty filter pattern")
	}
	if strings.ContainsAny(g, "\r\n\x00") {
		return Errorf(CodeInvalidFilter, "filter pattern %q contains a line break", g)
	}
	return nil
}

// newFilter builds the rclone filter for a spec. Invalid patterns are
// invalid_filter.
func newFilter(s filterSpec) (*filter.Filter, error) {
	rules, err := s.rules()
	if err != nil {
		return nil, err
	}
	// Every limit starts "off"; a zero Duration would mean "0s old".
	opt := filter.Options{MinAge: fs.DurationOff, MaxAge: fs.DurationOff, MinSize: fs.SizeSuffix(-1), MaxSize: fs.SizeSuffix(-1)}
	opt.FilterRule = rules
	if s.f.MaxSizeBytes > 0 {
		opt.MaxSize = fs.SizeSuffix(s.f.MaxSizeBytes)
	}
	fi, err := filter.NewFilter(&opt)
	if err != nil {
		return nil, Errorf(CodeInvalidFilter, "%v", err)
	}
	return fi, nil
}

// withFilter returns ctx carrying the spec's filter.
func withFilter(ctx context.Context, s filterSpec) (context.Context, *filter.Filter, error) {
	fi, err := newFilter(s)
	if err != nil {
		return nil, nil, err
	}
	return filter.ReplaceConfig(ctx, fi), fi, nil
}

// ValidateFilters checks exclude/include patterns the way a run would use
// them; the job side calls it when a job is saved.
func ValidateFilters(f Filters) error {
	_, err := newFilter(filterSpec{f: f})
	return err
}

// includedFile applies a filter to one relative file path.
func includedFile(fi *filter.Filter, rel string, size int64, modTime time.Time) bool {
	return fi.Include(rel, size, modTime, nil)
}

// includedDir applies a filter's directory rules to one relative folder.
func includedDir(ctx context.Context, fi *filter.Filter, rel string) bool {
	ok, err := fi.IncludeDirectory(ctx, nil)(rel)
	if err != nil {
		return true
	}
	return ok
}
