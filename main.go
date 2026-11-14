package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

const usageText = `epfix renames TV episode and movie files from release names to library names.

usage: epfix [options] [path ...]
paths may be directories or video files (default ".")

options:
      --apply                rename files (default: print the plan)
      --movies               rename movies instead of TV episodes
      --template text        output name template (default "{title} - S{season}E{episode}",
                             with --movies "{title} ({year})")
      --title text           force the title (series name, or movie title with --movies)
      --season int           force the season number (TV mode)
  -r, --recursive            recurse into subdirectories
      --sidecars             rename sidecar files (default)
      --no-sidecars          leave sidecar files alone
      --ext list             video extensions (default "mkv,mp4,m4v,avi,mov,ts,webm")
  -q, --quiet                print the summary only
      --version              print the version
  -h, --help                 print this help

exit codes: 0 success or clean plan, 1 conflicts or rename failure, 2 usage error.
`

type options struct {
	apply     bool
	template  string
	movies    bool
	maxYear   int // movie year ceiling: time.Now().Year()+1
	title     string
	season    int
	seasonSet bool
	recursive bool
	sidecars  bool
	exts      []string
	quiet     bool
	version   bool
	help      bool
	paths     []string
}

func defaultExts() []string {
	return []string{"mkv", "mp4", "m4v", "avi", "mov", "ts", "webm"}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	opt, err := parseOptions(args)
	if err != nil {
		fmt.Fprintf(stderr, "epfix: %v\n", err)
		return 2
	}
	switch {
	case opt.help:
		fmt.Fprint(stdout, usageText)
		return 0
	case opt.version:
		fmt.Fprintf(stdout, "epfix %s\n", version)
		return 0
	}
	return runPlan(opt, stdout, stderr)
}

func parseOptions(args []string) (options, error) {
	opt := options{
		maxYear:  time.Now().Year() + 1,
		sidecars: true,
		exts:     defaultExts(),
	}
	fs := flag.NewFlagSet("epfix", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	season := -1
	var extCSV string
	var noSidecars bool
	fs.BoolVar(&opt.apply, "apply", false, "")
	fs.BoolVar(&opt.movies, "movies", false, "")
	fs.StringVar(&opt.template, "template", "", "")
	fs.StringVar(&opt.title, "title", "", "")
	fs.IntVar(&season, "season", -1, "")
	fs.BoolVar(&opt.recursive, "recursive", false, "")
	fs.BoolVar(&opt.recursive, "r", false, "")
	fs.BoolVar(&opt.sidecars, "sidecars", true, "")
	fs.BoolVar(&noSidecars, "no-sidecars", false, "")
	fs.StringVar(&extCSV, "ext", strings.Join(defaultExts(), ","), "")
	fs.BoolVar(&opt.quiet, "quiet", false, "")
	fs.BoolVar(&opt.quiet, "q", false, "")
	fs.BoolVar(&opt.version, "version", false, "")
	fs.BoolVar(&opt.help, "help", false, "")
	fs.BoolVar(&opt.help, "h", false, "")

	// flag stops at the first positional; re-parse the rest so flags may follow
	// paths.
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return opt, err
		}
		if fs.NArg() == 0 {
			break
		}
		opt.paths = append(opt.paths, fs.Arg(0))
		rest = fs.Args()[1:]
	}

	if season < -1 {
		return opt, fmt.Errorf("invalid season %d", season)
	}
	if season >= 0 {
		opt.season = season
		opt.seasonSet = true
	}
	opt.sidecars = opt.sidecars && !noSidecars
	exts, err := parseExts(extCSV)
	if err != nil {
		return opt, err
	}
	opt.exts = exts

	// The template vocabulary belongs to the mode, so one --template serves
	// both: TV tokens for episodes, movie tokens for movies.
	templateSet := false
	fs.Visit(func(f *flag.Flag) { templateSet = templateSet || f.Name == "template" })
	allowed := []string{"title", "season", "episode"}
	if opt.movies {
		if opt.seasonSet {
			return opt, errors.New("--season cannot be used with --movies")
		}
		allowed = []string{"title", "year"}
	}
	if !templateSet {
		opt.template = defaultTemplate
		if opt.movies {
			opt.template = defaultMovieTemplate
		}
	}
	if err := validateTemplate(opt.template, allowed...); err != nil {
		return opt, err
	}
	return opt, nil
}

func parseExts(csv string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(csv, ",") {
		ext := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(part), "."))
		if ext == "" {
			continue
		}
		if strings.ContainsAny(ext, `/\`) {
			return nil, fmt.Errorf("invalid extension %q", ext)
		}
		out = append(out, ext)
	}
	if len(out) == 0 {
		return nil, errors.New("--ext needs at least one extension")
	}
	return out, nil
}

// dirTarget is one path argument after resolution. A whole directory scans
// every matching video (names nil); a file argument scans exactly the named
// video, plus the sidecars it owns (names set, keys folded by scopeKey).
// recursive applies to directory targets only.
type dirTarget struct {
	dir       string          // first spelling seen; used in printed paths
	key       string          // dirKey(dir)
	names     map[string]bool // nil = every video in dir
	recursive bool
}

// explicitFile is a file argument as typed, plus its fileKey.
type explicitFile struct {
	path string
	key  string
}

// resolution is the resolved invocation: the directories to scan, plus the
// file arguments, which drive the "nothing to rename" notice even when a
// directory argument absorbs them.
type resolution struct {
	targets  []dirTarget
	explicit []explicitFile
}

// targets resolves the path arguments, defaulting to the working directory.
// Targets in the same directory merge by dirKey: the first spelling seen wins
// as dir, and a whole directory absorbs file targets in it. Targets below a
// recursive directory are deduplicated by planPaths, which knows what the
// walk actually visits.
func (o options) targets() (resolution, error) {
	paths := o.paths
	if len(paths) == 0 {
		paths = []string{"."}
	}
	var res resolution
	byDir := make(map[string]int, len(paths)) // dirKey → index into res.targets
	seen := make(map[string]bool, len(paths)) // fileKey of explicit files
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return resolution{}, err
		}
		clean := filepath.Clean(p)
		dir, name := clean, ""
		if !info.IsDir() {
			ext := strings.TrimPrefix(filepath.Ext(clean), ".")
			if !extsContain(o.exts, ext) {
				return resolution{}, fmt.Errorf("%s: not a video file", p)
			}
			dir, name = filepath.Dir(clean), filepath.Base(clean)
		}
		key, err := dirKey(dir)
		if err != nil {
			return resolution{}, err
		}
		i, ok := byDir[key]
		if !ok {
			i = len(res.targets)
			byDir[key] = i
			res.targets = append(res.targets, dirTarget{dir: dir, key: key, names: map[string]bool{}})
		}
		t := &res.targets[i]
		if name == "" {
			t.names = nil // a whole directory absorbs file targets in it
			t.recursive = o.recursive
			continue
		}
		if fk := key + string(filepath.Separator) + scopeKey(name); !seen[fk] {
			seen[fk] = true
			res.explicit = append(res.explicit, explicitFile{path: clean, key: fk})
		}
		if t.names != nil {
			t.names[scopeKey(name)] = true
		}
	}
	return res, nil
}

// dirKey identifies a directory regardless of spelling: relative or absolute,
// through a symlink, or in a different case (the supported platform has a
// case-insensitive filesystem). It reads the filesystem but never writes.
func dirKey(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return scopeKey(real), nil
}

// fileKey identifies a file by its directory's dirKey and its folded name.
func fileKey(p string) (string, error) {
	key, err := dirKey(filepath.Dir(p))
	if err != nil {
		return "", err
	}
	return key + string(filepath.Separator) + scopeKey(filepath.Base(p)), nil
}

// scopeKey folds a file name for comparison: the supported platform has a
// case-insensitive filesystem, so an argument spelled Show.S01E01.mkv must
// match show.s01e01.mkv on disk.
func scopeKey(name string) string {
	return strings.ToLower(name)
}

func extsContain(exts []string, ext string) bool {
	ext = strings.ToLower(ext)
	for _, e := range exts {
		if e == ext {
			return true
		}
	}
	return false
}

func runPlan(opt options, stdout, stderr io.Writer) int {
	res, err := opt.targets()
	if err != nil {
		fmt.Fprintf(stderr, "epfix: %v\n", err)
		return 1
	}
	plans, err := planPaths(res.targets, opt)
	if err != nil {
		fmt.Fprintf(stderr, "epfix: %v\n", err)
		return 1
	}

	var renames []rename
	var conflicts []conflict
	for _, dp := range plans {
		renames = append(renames, dp.renames...)
		conflicts = append(conflicts, dp.conflicts...)
	}

	// Explicit file arguments that carry no episode marker (or are already
	// named correctly) get a notice; they never abort the run. The notice is
	// driven by the arguments, in the order given, so it survives being
	// absorbed by a directory argument.
	if !opt.quiet && len(res.explicit) > 0 {
		planned := make(map[string]bool, len(renames)+len(conflicts))
		sources := make([]string, 0, len(renames)+len(conflicts))
		for _, r := range renames {
			sources = append(sources, r.Source)
		}
		for _, c := range conflicts {
			if c.source != "" {
				sources = append(sources, c.source)
			}
		}
		for _, src := range sources {
			key, err := fileKey(src)
			if err != nil {
				fmt.Fprintf(stderr, "epfix: %v\n", err)
				return 1
			}
			planned[key] = true
		}
		for _, f := range res.explicit {
			if !planned[f.key] {
				fmt.Fprintf(stderr, "epfix: %s: nothing to rename\n", f.path)
			}
		}
	}

	if opt.apply && len(conflicts) == 0 {
		var applied []rename
		for _, dp := range plans {
			done, err := applyDir(dp)
			applied = append(applied, done...)
			if err != nil {
				printResult(stdout, applied, nil, true, opt)
				fmt.Fprintf(stderr, "epfix: %s: %v\n", dp.dir, err)
				fmt.Fprintf(stderr, "epfix: %d of %d renames applied\n", len(applied), len(renames))
				return 1
			}
		}
		printResult(stdout, applied, nil, true, opt)
		return 0
	}

	printResult(stdout, renames, conflicts, false, opt)
	if len(conflicts) > 0 {
		return 1
	}
	return 0
}

func printResult(w io.Writer, renames []rename, conflicts []conflict, applied bool, opt options) {
	if !opt.quiet && len(renames) > 0 {
		width := 0
		for _, r := range renames {
			if n := len(r.Source); n > width {
				width = n
			}
		}
		for _, r := range renames {
			fmt.Fprintf(w, "%-*s  →  %s\n", width, r.Source, r.Target)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "%d %s, %d %s\n", len(renames), plural(len(renames), "rename"), len(conflicts), plural(len(conflicts), "conflict"))
	for _, c := range conflicts {
		fmt.Fprintf(w, "! %s: %s\n", c.Reason, c.detail)
	}
	if !opt.quiet && !applied && len(renames) > 0 && len(conflicts) == 0 {
		fmt.Fprintf(w, "\n(dry run — pass --apply to rename)\n")
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
