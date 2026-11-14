package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// rename is one file move in a plan.
type rename struct {
	Source string
	Target string
}

// conflict explains why a plan cannot be applied. detail is only for humans;
// source is the planned source that cannot move, empty when a conflict belongs
// to no single file.
type conflict struct {
	Reason string
	detail string
	source string
}

// dirPlan is the complete plan for one directory.
type dirPlan struct {
	dir       string
	renames   []rename
	conflicts []conflict
}

var (
	// Group 1 is the text before the marker, group 2 the season, group 3 the
	// run of episode markers. The marker must be bounded by non-alphanumerics.
	seasonEpRe = regexp.MustCompile(`(?i)^(.*?)(?:^|[^\p{L}\p{N}])s([0-9]{1,2})((?:e[0-9]{1,3})+)(?:[^\p{L}\p{N}]|$)`)
	episodeRe  = regexp.MustCompile(`(?i)e([0-9]{1,3})`)
	crossRe    = regexp.MustCompile(`(?i)^(.*?)(?:^|[^\p{L}\p{N}])([0-9]{1,2})x([0-9]{1,3})(?:[^\p{L}\p{N}]|$)`)
	spaceRe    = regexp.MustCompile(`\s+`)
	seasonDir  = regexp.MustCompile(`(?i)^(?:season[\s._-]*|s)([0-9]{1,2})$`)
	tokenRe    = regexp.MustCompile(`\{([^{}]*)\}`)
)

const defaultTemplate = "{show} - S{season}E{episode}"

type episodeInfo struct {
	season   int
	episodes []int
	show     string // raw text before the marker
}

// detect finds an episode marker in a file stem.
func detect(stem string) (episodeInfo, bool) {
	if m := seasonEpRe.FindStringSubmatch(stem); m != nil {
		info := episodeInfo{show: m[1]}
		info.season, _ = strconv.Atoi(m[2])
		for _, e := range episodeRe.FindAllStringSubmatch(m[3], -1) {
			n, _ := strconv.Atoi(e[1])
			info.episodes = append(info.episodes, n)
		}
		if len(info.episodes) > 0 {
			return info, true
		}
	}
	if m := crossRe.FindStringSubmatch(stem); m != nil {
		info := episodeInfo{show: m[1]}
		info.season, _ = strconv.Atoi(m[2])
		n, _ := strconv.Atoi(m[3])
		info.episodes = []int{n}
		return info, true
	}
	return episodeInfo{}, false
}

// normalizeShow turns release-name punctuation into a readable show name.
// Trailing and leading separators are trimmed so rendering is idempotent:
// "Show Name - S04E01.mkv" must not become "Show Name - - S04E01.mkv".
func normalizeShow(s string) string {
	s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	s = spaceRe.ReplaceAllString(s, " ")
	return strings.Trim(s, " -")
}

// showFromDir is the last-resort show name: the directory, or its parent when
// the directory is a season folder.
func showFromDir(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Base(dir)
	}
	base := filepath.Base(abs)
	if seasonDir.MatchString(base) {
		parent := filepath.Base(filepath.Dir(abs))
		if parent != "/" && parent != "." && parent != base {
			return parent
		}
	}
	return base
}

// validateTemplate rejects templates that cannot produce a plain file name.
func validateTemplate(t string) error {
	if t == "" {
		return errors.New("template must not be empty")
	}
	if strings.ContainsAny(t, `/\`) {
		return errors.New("template must not contain path separators")
	}
	last := 0
	for _, m := range tokenRe.FindAllStringSubmatchIndex(t, -1) {
		if strings.ContainsAny(t[last:m[0]], "{}") {
			return errors.New("malformed template")
		}
		switch t[m[2]:m[3]] {
		case "show", "season", "episode":
		default:
			return fmt.Errorf("unknown template token {%s}", t[m[2]:m[3]])
		}
		last = m[1]
	}
	if strings.ContainsAny(t[last:], "{}") {
		return errors.New("malformed template")
	}
	return nil
}

func formatEpisodes(eps []int) string {
	var b strings.Builder
	for i, e := range eps {
		if i > 0 {
			b.WriteByte('E')
		}
		fmt.Fprintf(&b, "%02d", e)
	}
	return b.String()
}

// renderTemplate substitutes tokens, then collapses whitespace.
func renderTemplate(t, show string, season int, eps []int) string {
	out := strings.NewReplacer(
		"{show}", show,
		"{season}", fmt.Sprintf("%02d", season),
		"{episode}", formatEpisodes(eps),
	).Replace(t)
	return strings.TrimSpace(spaceRe.ReplaceAllString(out, " "))
}

// invalidTarget returns a conflict reason when name cannot be written.
func invalidTarget(name string) string {
	switch {
	case name == "":
		return "empty output name"
	case strings.ContainsAny(name, "/:"):
		return "invalid output name"
	}
	return ""
}

// planPaths plans every target named by the invocation. Recursive targets go
// first, and a directory is planned at most once: a later target in a
// directory a walk already visited is skipped, and a walk that reaches a
// planned directory skips its subtree (the earlier walk covered it). Hidden
// and symlinked directories, which a walk never enters, keep their own
// targets.
func planPaths(targets []dirTarget, o options) ([]dirPlan, error) {
	var plans []dirPlan
	planned := make(map[string]bool)
	for _, t := range targets {
		if !t.recursive {
			continue
		}
		err := filepath.WalkDir(t.dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				return nil
			}
			if path != t.dir && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			key, err := dirKey(path)
			if err != nil {
				return err
			}
			if planned[key] {
				return fs.SkipDir
			}
			planned[key] = true
			dp, err := planDir(path, nil, o)
			if err != nil {
				return err
			}
			if len(dp.renames) > 0 || len(dp.conflicts) > 0 {
				plans = append(plans, dp)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, t := range targets {
		if t.recursive || planned[t.key] {
			continue
		}
		dp, err := planDir(t.dir, t.names, o)
		if err != nil {
			return nil, err
		}
		plans = append(plans, dp)
	}
	return plans, nil
}

// planDir scans one directory. names is the scope: nil plans every video in
// the directory, a non-nil set plans only the named videos (keys folded by
// scopeKey). It never touches the filesystem.
func planDir(dir string, names map[string]bool, o options) (dirPlan, error) {
	dp := dirPlan{dir: dir}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dp, err
	}

	exts := make(map[string]bool, len(o.exts))
	for _, e := range o.exts {
		exts[e] = true
	}

	type video struct {
		name   string // file name
		stem   string // name without the video extension
		ext    string
		target string // rendered name, without extension
	}

	var videos []video
	var rest []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		ext := strings.TrimPrefix(filepath.Ext(name), ".")
		if exts[strings.ToLower(ext)] {
			videos = append(videos, video{
				name: name,
				stem: strings.TrimSuffix(name, "."+ext),
				ext:  ext,
			})
			continue
		}
		rest = append(rest, name)
	}

	// Render the target of every video in the directory, collecting the ones
	// that can be renamed plus the conflicts of the ones that cannot. Rendering
	// out-of-scope videos as well keeps the scope filter from changing how a
	// shared sidecar or a shared target is decided.
	var detected []video
	var invalid []conflict
	dirShow := ""
	for _, v := range videos {
		info, ok := detect(v.stem)
		if !ok {
			continue
		}
		show := o.show
		if show == "" {
			show = normalizeShow(info.show)
		}
		if show == "" {
			if dirShow == "" {
				dirShow = showFromDir(dir)
			}
			show = dirShow
		}
		season := info.season
		if o.seasonSet {
			season = o.season
		}
		v.target = renderTemplate(o.template, show, season, info.episodes)
		if reason := invalidTarget(v.target); reason != "" {
			invalid = append(invalid, conflict{Reason: reason, detail: v.target, source: filepath.Join(dir, v.name)})
			continue
		}
		detected = append(detected, v)
	}

	// Sidecars are files that share a video's stem, matched by the longest
	// stem. Matched against every detected video, before the scope filter:
	// filtering first would let an in-scope Show.S01E01.mkv steal
	// Show.S01E01.720p.srt from an out-of-scope Show.S01E01.720p.mkv.
	sidecars := map[string][]string{}
	if o.sidecars {
		for _, name := range rest {
			best := ""
			for _, v := range detected {
				if len(name) > len(v.stem) && strings.HasPrefix(name, v.stem) && name[len(v.stem)] == '.' && len(v.stem) > len(best) {
					best = v.stem
				}
			}
			if best != "" {
				sidecars[best] = append(sidecars[best], name)
			}
		}
	}

	// Apply the scope: drop out-of-scope videos, their sidecars, and their
	// conflicts. An out-of-scope sibling must not abort the run.
	inScope := func(v video) bool {
		return names == nil || names[scopeKey(v.name)]
	}
	var kept []video
	for _, v := range detected {
		if !inScope(v) {
			delete(sidecars, v.stem)
			continue
		}
		kept = append(kept, v)
	}
	for _, c := range invalid {
		if names == nil || names[scopeKey(filepath.Base(c.source))] {
			dp.conflicts = append(dp.conflicts, c)
		}
	}

	for _, v := range kept {
		if v.target+"."+v.ext != v.name {
			dp.renames = append(dp.renames, rename{
				Source: filepath.Join(dir, v.name),
				Target: filepath.Join(dir, v.target+"."+v.ext),
			})
		}
		for _, sc := range sidecars[v.stem] {
			target := v.target + sc[len(v.stem):]
			if target == sc {
				continue
			}
			dp.renames = append(dp.renames, rename{
				Source: filepath.Join(dir, sc),
				Target: filepath.Join(dir, target),
			})
		}
	}

	sources := make(map[string]bool, len(dp.renames))
	for _, r := range dp.renames {
		sources[scopeKey(filepath.Base(r.Source))] = true
	}
	seen := make(map[string]bool, len(dp.renames))
	for _, r := range dp.renames {
		base := filepath.Base(r.Target)
		key := scopeKey(base)
		if seen[key] {
			dp.conflicts = append(dp.conflicts, conflict{Reason: "duplicate target", detail: base, source: r.Source})
			continue
		}
		seen[key] = true
		if sources[key] {
			// The occupant is renamed away by this batch.
			continue
		}
		switch _, err := os.Lstat(r.Target); {
		case err == nil:
			dp.conflicts = append(dp.conflicts, conflict{Reason: "target exists", detail: base, source: r.Source})
		case !errors.Is(err, fs.ErrNotExist):
			return dp, err
		}
	}
	return dp, nil
}
