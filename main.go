package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

const usageText = `epfix renames TV episode files from release names to library names.

usage: epfix [options] [path ...]

options:
      --apply            rename files (default: print the plan)
      --template text    output name template (default "{show} - S{season}E{episode}")
      --show text        force the show name
      --season int       force the season number
  -r, --recursive        recurse into subdirectories
      --sidecars         rename sidecar files (default)
      --no-sidecars      leave sidecar files alone
      --ext list         video extensions (default "mkv,mp4,m4v,avi,mov,ts,webm")
  -q, --quiet            print the summary only
      --version          print the version
  -h, --help             print this help

exit codes: 0 success or clean plan, 1 conflicts or rename failure, 2 usage error.
`

type options struct {
	apply     bool
	template  string
	show      string
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
	opt := options{template: defaultTemplate, sidecars: true, exts: defaultExts()}
	fs := flag.NewFlagSet("epfix", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	season := -1
	var extCSV string
	var noSidecars bool
	fs.BoolVar(&opt.apply, "apply", false, "")
	fs.StringVar(&opt.template, "template", defaultTemplate, "")
	fs.StringVar(&opt.show, "show", "", "")
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
	if err := validateTemplate(opt.template); err != nil {
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

// dirs returns the directories to scan, defaulting to the working directory.
func (o options) dirs() ([]string, error) {
	paths := o.paths
	if len(paths) == 0 {
		paths = []string{"."}
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s: not a directory", p)
		}
		out = append(out, filepath.Clean(p))
	}
	return out, nil
}

func runPlan(opt options, stdout, stderr io.Writer) int {
	dirs, err := opt.dirs()
	if err != nil {
		fmt.Fprintf(stderr, "epfix: %v\n", err)
		return 1
	}
	plans, err := planPaths(dirs, opt)
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
