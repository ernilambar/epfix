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
with --movies, a directory argument whose own name looks like a release name
is renamed too; in TV mode, a directory argument that names a season is
renamed to "Show Name - S01" (or "Season 01" inside its show folder).

options:
  -a, --apply                rename files (default: print the plan)
  -m, --movies               rename movies instead of TV episodes
  -t, --title text           force the title (series name, or movie title with --movies)
  -s, --season int           force the season number, zero or positive (TV mode)
  -V, --version              print the version
  -h, --help                 print this help

exit codes: 0 success or clean plan, 1 conflicts or rename failure, 2 usage error.
`

type options struct {
	apply     bool
	movies    bool
	maxYear   int // movie year ceiling: time.Now().Year()+1
	title     string
	season    int
	seasonSet bool
	version   bool
	help      bool
	paths     []string
}

// videoExts are the video extensions, lowercase and without the dot.
var videoExts = []string{"mkv", "mp4", "m4v", "avi", "mov", "ts", "webm"}

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
	opt := options{maxYear: time.Now().Year() + 1}
	fs := flag.NewFlagSet("epfix", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	season := 0
	fs.BoolVar(&opt.apply, "apply", false, "")
	fs.BoolVar(&opt.apply, "a", false, "")
	fs.BoolVar(&opt.movies, "movies", false, "")
	fs.BoolVar(&opt.movies, "m", false, "")
	fs.StringVar(&opt.title, "title", "", "")
	fs.StringVar(&opt.title, "t", "", "")
	fs.IntVar(&season, "season", 0, "")
	fs.IntVar(&season, "s", 0, "")
	fs.BoolVar(&opt.version, "version", false, "")
	fs.BoolVar(&opt.version, "V", false, "")
	fs.BoolVar(&opt.help, "help", false, "")
	fs.BoolVar(&opt.help, "h", false, "")

	// Flags may follow paths. A bare "--" ends flag parsing for good, so
	// everything after it is a path, even "-a".
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return opt, err
		}
		if n := len(rest) - fs.NArg(); n > 0 && rest[n-1] == "--" {
			opt.paths = append(opt.paths, fs.Args()...)
			break
		}
		if fs.NArg() == 0 {
			break
		}
		opt.paths = append(opt.paths, fs.Arg(0))
		rest = fs.Args()[1:]
	}

	// Visit reports every flag given, so an explicit --season 0 is told apart
	// from the default and a negative season is rejected instead of ignored.
	seasonGiven := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "season" || f.Name == "s" {
			seasonGiven = true
		}
	})
	if seasonGiven {
		if season < 0 {
			return opt, fmt.Errorf("invalid season %d: must be zero or positive", season)
		}
		opt.season = season
		opt.seasonSet = true
	}
	if opt.movies && opt.seasonSet {
		return opt, errors.New("--season cannot be used with --movies")
	}
	return opt, nil
}

// dirTarget is one path argument after resolution. A whole directory scans
// every matching video (names nil); a file argument scans exactly the named
// video, plus the sidecars it owns (names set, keys folded by scopeKey).
type dirTarget struct {
	dir   string          // first spelling seen; used in printed paths
	key   string          // dirKey(dir)
	names map[string]bool // nil = every video in dir
}

// explicitFile is a file argument as typed, plus its entryKey.
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
// as dir, and a whole directory absorbs file targets in it.
func (o options) targets() (resolution, error) {
	paths := o.paths
	if len(paths) == 0 {
		paths = []string{"."}
	}
	var res resolution
	byDir := make(map[string]int, len(paths)) // dirKey → index into res.targets
	seen := make(map[string]bool, len(paths)) // entryKey of explicit files
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return resolution{}, err
		}
		clean := filepath.Clean(p)
		dir, name := clean, ""
		if !info.IsDir() {
			ext := strings.TrimPrefix(filepath.Ext(clean), ".")
			if !extsContain(videoExts, ext) {
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

// entryKey identifies a directory entry (a file, or a folder rename source or
// target) by its parent's dirKey plus the folded base name. The final
// component is never resolved: a folder argument may itself be a symlink, and
// a folder rename acts on that entry. Resolving the parent keeps two
// spellings of one directory, such as a symlink and its target, from planning
// two renames onto one file.
func entryKey(p string) (string, error) {
	parent, err := dirKey(filepath.Dir(p))
	if err != nil {
		return "", err
	}
	return parent + string(filepath.Separator) + scopeKey(filepath.Base(p)), nil
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
	folderRenames, folderConflicts, err := planMovieFolders(res.targets, opt)
	if err != nil {
		fmt.Fprintf(stderr, "epfix: %v\n", err)
		return 1
	}
	showRenames, showConflicts, err := planShowFolders(res.targets, opt)
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
	renames = append(renames, folderRenames...)
	renames = append(renames, showRenames...)
	conflicts = append(conflicts, folderConflicts...)
	conflicts = append(conflicts, showConflicts...)

	// Explicit file arguments that carry no episode marker (or are already
	// named correctly) get a notice; they never abort the run. The notice is
	// driven by the arguments, in the order given, so it survives being
	// absorbed by a directory argument.
	if len(res.explicit) > 0 {
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
			key, err := entryKey(src)
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
		applied, err := applyAll(plans, folderRenames, showRenames)
		if err != nil {
			fmt.Fprintf(stderr, "epfix: %v\n", err)
			fmt.Fprintf(stderr, "epfix: %d of %d renames applied; the rest were left in place\n", len(applied), len(renames))
			printRenames(stdout, applied)
			return 1
		}
		printResult(stdout, applied, nil, true)
		return 0
	}

	printResult(stdout, renames, conflicts, false)
	if len(conflicts) > 0 {
		return 1
	}
	return 0
}

// applyAll applies the file plans first, then the folder renames: a folder
// rename would otherwise invalidate the sources and targets planned inside it.
func applyAll(plans []dirPlan, folders, shows []rename) ([]rename, error) {
	var applied []rename
	for _, dp := range plans {
		done, err := applyDir(dp)
		applied = append(applied, done...)
		if err != nil {
			return applied, err
		}
	}
	done, err := applyDirRenames(folders)
	applied = append(applied, done...)
	if err != nil {
		return applied, err
	}
	done, err = applyDirRenames(shows)
	return append(applied, done...), err
}

func printResult(w io.Writer, renames []rename, conflicts []conflict, applied bool) {
	printRenames(w, renames)
	fmt.Fprintf(w, "%d %s, %d %s\n", len(renames), plural(len(renames), "rename"), len(conflicts), plural(len(conflicts), "conflict"))
	for _, c := range conflicts {
		fmt.Fprintf(w, "! %s: %s\n", c.Reason, c.detail)
	}
	if !applied && len(renames) > 0 && len(conflicts) == 0 {
		fmt.Fprintf(w, "\n(dry run — pass --apply to rename)\n")
	}
}

// printRenames lists one rename per line, source column padded to the widest
// source.
func printRenames(w io.Writer, renames []rename) {
	if len(renames) == 0 {
		return
	}
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

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
