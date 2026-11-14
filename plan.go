package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
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
	// entriesAreDirs marks a synthetic plan whose sources are directories
	// (movie folder renames), so applyDir stages each one as a directory
	// rather than a file.
	entriesAreDirs bool
}

var (
	// Group 1 is the text before the marker, group 2 the season, group 3 the
	// run of episode markers. The marker must be bounded by non-alphanumerics.
	seasonEpRe = regexp.MustCompile(`(?i)^(.*?)(?:^|[^\p{L}\p{N}])s([0-9]{1,2})((?:e[0-9]{1,3})+)(?:[^\p{L}\p{N}]|$)`)
	episodeRe  = regexp.MustCompile(`(?i)e([0-9]{1,3})`)
	crossRe    = regexp.MustCompile(`(?i)^(.*?)(?:^|[^\p{L}\p{N}])([0-9]{1,2})x([0-9]{1,3})(?:[^\p{L}\p{N}]|$)`)
	spaceRe    = regexp.MustCompile(`\s+`)
	seasonDir  = regexp.MustCompile(`(?i)^(?:season[\s._-]*|s)([0-9]{1,2})$`)
	// showSeasonRe finds a season-only marker ("S04", "Season 4") in a release
	// folder name and the show text before it.
	showSeasonRe = regexp.MustCompile(`(?i)^(.*?)(?:^|[^\p{L}\p{N}])(?:season[\s._-]*|s)([0-9]{1,2})(?:[^\p{L}\p{N}]|$)`)
	// seasonRangeRe marks a multi-season pack ("S01-S05", "Season 1-5"), which
	// has no single season folder to move into.
	seasonRangeRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:s|season[\s._-]*)[0-9]{1,2}[\s._-]*(?:-|–|to)[\s._-]*s?[0-9]{1,2}(?:[^\p{L}\p{N}]|$)`)
	tokenRe       = regexp.MustCompile(`\{([^{}]*)\}`)

	// siteTagRe strips one leading release-site tag. The chunk has no spaces
	// and starts with "www." or ends in a listed TLD. Any dotted word is not
	// enough: "Dr.No - 1962" is a title, not a site. A hyphen only separates
	// when a space comes before it, so the glued hyphen of a codec tag
	// ("Movie.1080p.WEB-DL") is not mistaken for a site tag.
	siteTagRe = regexp.MustCompile(`(?i)^\s*(?:www\.\S*|\S*\.(?:com|net|org|info|biz|co|in|io|me|cc|ws|to|tv|xyz|top|site|online|club|live|life|lol|pw|fun|company|world|mx|se|ms|gs|vc))(?:\s*[:|–]\s*|\s+-\s*)\s*`)
	// groupTagRe strips leading [...] and (...) release-group tags.
	groupTagRe = regexp.MustCompile(`^\s*(?:\[[^\]]*\]|\([^)]*\))(?:\s*(?:\[[^\]]*\]|\([^)]*\)))*\s*`)

	yearRe         = regexp.MustCompile(`[0-9]{4}`)
	movieSeasonRe  = regexp.MustCompile(`(?i)\b(?:season\s?[0-9]{1,2}|s[0-9]{1,2})\b`)
	movieEpisodeRe = regexp.MustCompile(`(?i)\b(?:ep|episode|e)\s?[0-9]{1,4}\b`)
	movieAnimeRe   = regexp.MustCompile(`(?i)\s[-–]\s?[0-9]{1,4}$`)
	// movieExtraRe marks a sample or trailer, which would otherwise take the
	// movie's own name and collide with it.
	movieExtraRe = regexp.MustCompile(`(?i)\b(?:sample|trailer)\b`)
	emptyPairRe  = regexp.MustCompile(`\s*(?:\(\s*\)|\[\s*\])`)
)

const (
	defaultTemplate      = "{title} - S{season}E{episode}"
	defaultMovieTemplate = "{title} ({year})"
)

// strongTokens start release metadata in a movie name; the first one after the
// title cuts the title. Bare "web", "ts", "cam", "dd" and "dv" are excluded:
// they are title words, and "hdcam", "ddp", "dd5.1" and "hdr" cover the real
// tags. Alternation is built longest-first so "hdr10" is not read as "hdr".
var strongTokens = []string{
	// resolution
	"2160p", "1080p", "720p", "576p", "480p", "4k", "uhd",
	// source
	"web-dl", "webdl", "webrip", "web-rip", "hdtv", "hdrip", "bluray", "blu-ray",
	"bdrip", "brrip", "dvdrip", "dvdscr", "remux", "hdcam", "hdts",
	"telesync", "predvd",
	// video
	"x264", "x265", "h264", "h265", "h.264", "h.265", "hevc", "avc", "av1",
	"xvid", "divx", "10bit", "hdr", "hdr10",
	// audio
	"aac", "ac3", "eac3", "ddp", "dd5.1", "dts", "atmos", "truehd", "flac",
	// subs
	"esub", "esubs", "msub",
}

// strongTokenRe also matches a size tag such as "1.6GB". Bounds are checked by
// tokenBounded, which rejects a match glued to a letter or digit.
var strongTokenRe = func() *regexp.Regexp {
	toks := append([]string(nil), strongTokens...)
	sort.Slice(toks, func(i, j int) bool { return len(toks[i]) > len(toks[j]) })
	for i, t := range toks {
		toks[i] = regexp.QuoteMeta(t)
	}
	return regexp.MustCompile(`(?i)(?:` + strings.Join(toks, "|") + `|[0-9]+(?:\.[0-9]+)?(?:mb|gb))`)
}()

// weakTokens are ordinary words that also end release names: languages, audio
// words, and edition words. They never cut a title; they are peeled off the end
// of a title found without a year.
var weakTokens = func() map[string]bool {
	words := []string{
		"telugu", "hindi", "tamil", "malayalam", "kannada", "bengali", "marathi",
		"punjabi", "english", "korean", "japanese", "chinese", "dual", "multi",
		"audio", "dubbed",
		"extended", "unrated", "uncut", "remastered", "proper", "repack", "imax",
		"theatrical", "directors", "cut", "hq",
	}
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}()

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

// detectShowSeason finds a season in a folder name. An episode marker counts as
// its season; otherwise a season-only marker ("S04", "Season 4") supplies the
// show text before it. A folder with no marker is not a season folder.
func detectShowSeason(stem string) (show string, season int, ok bool) {
	if info, isEpisode := detect(stem); isEpisode {
		return info.show, info.season, true
	}
	m := showSeasonRe.FindStringSubmatch(stem)
	if m == nil {
		return "", 0, false
	}
	season, _ = strconv.Atoi(m[2])
	return m[1], season, true
}

// movieInfo is a movie name found in a file stem. year is 0 when the name has
// no year in front of the release metadata.
type movieInfo struct {
	title string
	year  int
}

// detectMovie finds a movie title in a file stem. The caller tries detect
// first: a TV marker never falls through to a movie. maxYear is passed in so
// the clock never changes a golden test.
func detectMovie(stem string, maxYear int) (movieInfo, bool) {
	s := strings.TrimSpace(siteTagRe.ReplaceAllString(stem, ""))
	s = strings.TrimSpace(groupTagRe.ReplaceAllString(s, ""))
	if s == "" || movieExtraRe.MatchString(s) {
		return movieInfo{}, false
	}

	strong := strongTokenStarts(s)
	years := movieYearCandidates(s, maxYear)
	if len(strong) == 0 && !(len(years) > 0 && strings.ContainsAny(s, "._")) {
		// No release tag, and a year only counts in a dotted or underscored
		// name: "Jailer 2 (2026)" and "Family Trip 2019" are already named.
		return movieInfo{}, false
	}

	// Cut at the earlier of the first strong token and the last year in front
	// of it. A token or year at position 0 is part of the title itself
	// ("Cam.2018.1080p", "1917.2160p").
	cut := len(s)
	for _, pos := range strong {
		if pos > 0 && hasTitleText(s[:pos]) {
			cut = pos
			break
		}
	}
	year, yearCut := 0, -1
	for _, pos := range years {
		if pos >= cut || !hasTitleText(s[:pos]) {
			continue
		}
		if pos > yearCut {
			yearCut = pos
		}
	}
	if yearCut >= 0 {
		cut = yearCut
		year, _ = strconv.Atoi(s[yearCut : yearCut+4])
	}
	if cut == len(s) {
		return movieInfo{}, false
	}

	title := normalizeMovieTitle(s[:cut])
	if year == 0 {
		title = peelWeakTokens(title)
	}
	if title == "" || !movieTitleOK(title, maxYear) {
		return movieInfo{}, false
	}
	return movieInfo{title: title, year: year}, true
}

// strongTokenStarts returns the start offset of every whole strong token in s.
func strongTokenStarts(s string) []int {
	var out []int
	for _, m := range strongTokenRe.FindAllStringIndex(s, -1) {
		if tokenBounded(s, m[0], m[1]) {
			out = append(out, m[0])
		}
	}
	return out
}

// movieYearCandidates returns the start offset of every whole four-digit
// number that could be a year: bare, or inside parentheses or brackets.
func movieYearCandidates(s string, maxYear int) []int {
	var out []int
	for _, m := range yearRe.FindAllStringIndex(s, -1) {
		if !tokenBounded(s, m[0], m[1]) {
			continue
		}
		year, _ := strconv.Atoi(s[m[0]:m[1]])
		if year >= 1900 && year <= maxYear {
			out = append(out, m[0])
		}
	}
	return out
}

// tokenBounded reports whether s[start:end] stands alone: the runes around it,
// if any, are not letters or digits.
func tokenBounded(s string, start, end int) bool {
	if start > 0 {
		if r, _ := utf8.DecodeLastRuneInString(s[:start]); isWordRune(r) {
			return false
		}
	}
	if end < len(s) {
		if r, _ := utf8.DecodeRuneInString(s[end:]); isWordRune(r) {
			return false
		}
	}
	return true
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

// hasTitleText reports whether the text before a cut point holds a title
// character, not just separators and brackets.
func hasTitleText(s string) bool {
	return strings.Trim(s, " -–_.[](){}\t") != ""
}

// normalizeMovieTitle turns the raw text before the cut into a readable title:
// dots and underscores become spaces, empty and unbalanced trailing brackets
// are dropped, and the edges are trimmed.
func normalizeMovieTitle(s string) string {
	s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	s = spaceRe.ReplaceAllString(s, " ")
	return trimMovieTitle(s)
}

// trimMovieTitle trims separators from both edges and drops brackets left
// empty or unbalanced by the cut. Brackets are matched on the whole title, so
// "Movie (Director's Cut)" keeps its closing parenthesis.
func trimMovieTitle(s string) string {
	s = strings.Trim(s, " -–_.([{")
	for s != "" {
		switch s[len(s)-1] {
		case '(', '[', '{':
		case ')', ']', '}':
			if bracketsBalanced(s) {
				return s
			}
		default:
			return s
		}
		s = strings.TrimRight(s[:len(s)-1], " -–_.([{")
	}
	return s
}

// bracketsBalanced reports whether every closing bracket in s has a matching
// opening bracket before it.
func bracketsBalanced(s string) bool {
	var stack []byte
	for i := range s {
		switch c := s[i]; c {
		case '(', '[', '{':
			stack = append(stack, c)
		case ')', ']', '}':
			if len(stack) == 0 {
				return false
			}
			open := stack[len(stack)-1]
			if (c == ')' && open != '(') || (c == ']' && open != '[') || (c == '}' && open != '{') {
				return false
			}
			stack = stack[:len(stack)-1]
		}
	}
	return len(stack) == 0
}

// peelWeakTokens drops trailing language and edition words from a title that
// has no year: "Jailer 2 Telugu" becomes "Jailer 2". It is a heuristic: a title
// that really ends in such a word loses it, so always review the plan.
func peelWeakTokens(title string) string {
	words := strings.Fields(title)
	for len(words) > 1 && weakTokens[strings.ToLower(words[len(words)-1])] {
		words = words[:len(words)-1]
	}
	return trimMovieTitle(strings.Join(words, " "))
}

// movieTitleOK rejects titles that look like episode or season fragments
// rather than movie names.
func movieTitleOK(title string, maxYear int) bool {
	if movieSeasonRe.MatchString(title) || movieEpisodeRe.MatchString(title) || movieAnimeRe.MatchString(title) {
		return false
	}
	if hasLetter(title) {
		return true
	}
	// All digits: only a year can stand alone as a title ("1917", "2012").
	year, err := strconv.Atoi(title)
	return err == nil && year >= 1900 && year <= maxYear
}

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
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
// allowed lists the tokens the template may use.
func validateTemplate(t string, allowed ...string) error {
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
		tok := t[m[2]:m[3]]
		ok := false
		for _, a := range allowed {
			if tok == a {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("unknown template token {%s}", tok)
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
func renderTemplate(t, title string, season int, eps []int) string {
	out := strings.NewReplacer(
		"{title}", title,
		"{season}", fmt.Sprintf("%02d", season),
		"{episode}", formatEpisodes(eps),
	).Replace(t)
	return strings.TrimSpace(spaceRe.ReplaceAllString(out, " "))
}

// renderMovie substitutes the movie tokens, drops the brackets the year left
// empty, and collapses whitespace, so "{title} ({year})" gives "Jailer 2" when
// the name has no year.
func renderMovie(t, title string, year int) string {
	yearText := ""
	if year > 0 {
		yearText = strconv.Itoa(year)
	}
	out := strings.NewReplacer("{title}", title, "{year}", yearText).Replace(t)
	out = emptyPairRe.ReplaceAllString(out, "")
	out = spaceRe.ReplaceAllString(out, " ")
	return strings.Trim(out, " -")
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

// planMovieFolders plans the movie folders named directly on the command line.
// A whole-directory target is renamed with the movie template when its own
// base name looks like a release name, so "All We Imagine As Light (2024)
// [720p] [BluRay] [YTS.MX]" becomes "All We Imagine As Light (2024)". Files
// inside are planned by planDir; a nested directory is never renamed, because
// only the arguments themselves name their folder. It never writes.
func planMovieFolders(targets []dirTarget, o options) ([]rename, []conflict, error) {
	if !o.movies {
		return nil, nil, nil
	}
	var renames []rename
	var conflicts []conflict
	for _, t := range targets {
		if t.names != nil {
			continue // a file argument's directory is not renamed
		}
		src := filepath.Clean(t.dir)
		base := filepath.Base(src)
		m, ok := detectMovie(base, o.maxYear)
		if !ok {
			continue
		}
		title := m.title
		if o.title != "" {
			title = o.title
		}
		name := renderMovie(o.template, title, m.year)
		if name == base {
			continue
		}
		if reason := invalidTarget(name); reason != "" {
			conflicts = append(conflicts, conflict{Reason: reason, detail: name, source: src})
			continue
		}
		renames = append(renames, rename{Source: src, Target: filepath.Join(filepath.Dir(src), name)})
	}

	sources := make(map[string]bool, len(renames))
	for _, r := range renames {
		sources[scopeKey(r.Source)] = true
	}
	seen := make(map[string]bool, len(renames))
	kept := renames[:0]
	for _, r := range renames {
		key := scopeKey(r.Target)
		if seen[key] {
			conflicts = append(conflicts, conflict{Reason: "duplicate target", detail: filepath.Base(r.Target), source: r.Source})
			continue
		}
		seen[key] = true
		if !strings.EqualFold(r.Target, r.Source) && !sources[key] {
			switch _, err := os.Lstat(r.Target); {
			case err == nil:
				conflicts = append(conflicts, conflict{Reason: "target exists", detail: filepath.Base(r.Target), source: r.Source})
				continue
			case !errors.Is(err, fs.ErrNotExist):
				return nil, nil, err
			}
		}
		kept = append(kept, r)
	}
	return kept, conflicts, nil
}

// planShowFolders plans renaming the season folders named directly on the
// command line: "Show.Name.S04.1080p.WEB-DL" becomes "Show Name - S04", and an
// episode-named folder counts as its season. A folder already inside its show
// folder, or one with no title text, becomes "Season NN" in place instead. A
// folder with no season marker, and a multi-season pack, is left alone. Files
// inside are planned by planDir; nested directories are never renamed. It never
// writes.
func planShowFolders(targets []dirTarget, o options) ([]rename, []conflict, error) {
	if o.movies {
		return nil, nil, nil
	}
	var renames []rename
	var conflicts []conflict
	for _, t := range targets {
		if t.names != nil {
			continue // a file argument's directory is not renamed
		}
		src := filepath.Clean(t.dir)
		base := filepath.Base(src)
		rawTitle, season, ok := detectShowSeason(base)
		if !ok || seasonRangeRe.MatchString(base) {
			continue
		}
		title := normalizeShow(rawTitle)
		if o.title != "" {
			title = o.title
		}
		if o.seasonSet {
			season = o.season
		}
		number := fmt.Sprintf("%02d", season)
		parent := filepath.Dir(src)
		name := "Season " + number
		// A title that already names the containing folder is not repeated.
		if title != "" && !strings.EqualFold(filepath.Base(parent), title) {
			name = title + " - S" + number
		}
		if reason := invalidTarget(name); reason != "" {
			conflicts = append(conflicts, conflict{Reason: reason, detail: name, source: src})
			continue
		}
		target := filepath.Join(parent, name)
		if strings.EqualFold(target, src) {
			if target == src {
				continue // already named
			}
			renames = append(renames, rename{Source: src, Target: target})
			continue
		}
		if _, err := os.Lstat(target); err == nil {
			conflicts = append(conflicts, conflict{Reason: "target exists", detail: name, source: src})
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, nil, err
		}
		renames = append(renames, rename{Source: src, Target: target})
	}

	seen := make(map[string]bool, len(renames))
	kept := renames[:0]
	for _, r := range renames {
		key := scopeKey(r.Target)
		if seen[key] {
			conflicts = append(conflicts, conflict{Reason: "duplicate target", detail: filepath.Base(r.Target), source: r.Source})
			continue
		}
		seen[key] = true
		kept = append(kept, r)
	}
	return kept, conflicts, nil
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
		info, isEpisode := detect(v.stem)
		switch {
		case o.movies:
			if isEpisode {
				// Movie mode still recognizes episodes so it never renames one
				// as a movie; it just leaves them alone.
				continue
			}
			m, ok := detectMovie(v.stem, o.maxYear)
			if !ok {
				continue
			}
			title := m.title
			if o.title != "" {
				title = o.title
			}
			v.target = renderMovie(o.template, title, m.year)
		case isEpisode:
			show := o.title
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
		default:
			continue
		}
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
