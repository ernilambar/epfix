package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testOptions() options {
	return options{
		template: defaultTemplate,
		maxYear:  2027,
		sidecars: true,
		exts:     defaultExts(),
	}
}

// movieOptions is testOptions in movie mode: the mode picks the template
// vocabulary and default, as parseOptions does.
func movieOptions() options {
	o := testOptions()
	o.movies = true
	o.template = defaultMovieTemplate
	return o
}

func writeFile(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
}

func plannedTargets(dp dirPlan) []string {
	out := make([]string, 0, len(dp.renames))
	for _, r := range dp.renames {
		out = append(out, filepath.Base(r.Target))
	}
	return out
}

// TestGoldenNames is the shipped golden fixture: real release names in, the
// hand-written library name out.
func TestGoldenNames(t *testing.T) {
	tests := []struct {
		name  string
		sub   string // subdirectory to create; "" for the scan root
		file  string
		setup func(*options)
		want  string // expected target base name; "" means no rename
	}{
		{name: "bluray", file: "Show.Name.S04E01.1080p.BluRay.x264-GROUP.mkv", want: "Show Name - S04E01.mkv"},
		{name: "web dl", file: "Severance.S02E07.1080p.ATVP.WEB-DL.DDP5.1.H.264-FLUX.mkv", want: "Severance - S02E07.mkv"},
		{name: "branded", file: "Tom.Clancys.Jack.Ryan.S04E01.1080p.AMZN.WEBRip.x264-GalaxyTV[TGx].mkv", want: "Tom Clancys Jack Ryan - S04E01.mkv"},
		{name: "multi episode", file: "The.Boys.S01E03E04.1080p.WEB.h264-TEPES.mkv", want: "The Boys - S01E03E04.mkv"},
		{name: "x form", file: "Show.1x02.HDTV.mkv", want: "Show - S01E02.mkv"},
		{name: "lowercase", file: "andor.s01e05.720p.web.h264-cakes.mkv", want: "andor - S01E05.mkv"},
		{name: "underscore", file: "Show_Name_S04E01.mkv", want: "Show Name - S04E01.mkv"},
		{name: "bracket group", file: "[SubsPlease] Frieren.S01E01.1080p.mkv", want: "[SubsPlease] Frieren - S01E01.mkv"},
		{name: "specials", file: "Specials.S00E03.1080p.mkv", want: "Specials - S00E03.mkv"},
		{name: "three digit episode", file: "Anime.S01E100.mkv", want: "Anime - S01E100.mkv"},
		{name: "already renamed", file: "Show Name - S04E01.mkv", want: ""},
		{name: "no marker", file: "Show.Name.Complete.Season.4.mkv", want: ""},
		{name: "resolution", file: "Show.1920x1080.mkv", want: ""},
		{name: "glued marker", file: "ShowS04E01.mkv", want: ""},
		{name: "three digit season", file: "Show.S104E01.mkv", want: ""},
		{name: "forced title", file: "Show.Name.S04E01.1080p.mkv", setup: func(o *options) { o.title = "Jack Ryan" }, want: "Jack Ryan - S04E01.mkv"},
		{name: "forced season", file: "Show.Name.S04E01.1080p.mkv", setup: func(o *options) { o.season, o.seasonSet = 2, true }, want: "Show Name - S02E01.mkv"},
		{name: "custom template", file: "Show.Name.S04E01.1080p.mkv", setup: func(o *options) {
			o.template = "{title} S{season}E{episode}"
		}, want: "Show Name S04E01.mkv"},
		{name: "dir fallback", sub: "Some Show", file: "S01E01.mkv", want: "Some Show - S01E01.mkv"},
		{name: "season folder fallback", sub: filepath.Join("Some Show", "Season 04"), file: "S04E02.mkv", want: "Some Show - S04E02.mkv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.sub != "" {
				dir = filepath.Join(dir, tt.sub)
			}
			writeFile(t, dir, tt.file)

			o := testOptions()
			if tt.setup != nil {
				tt.setup(&o)
			}
			dp, err := planDir(dir, nil, o)
			if err != nil {
				t.Fatal(err)
			}
			if len(dp.conflicts) != 0 {
				t.Fatalf("unexpected conflicts: %+v", dp.conflicts)
			}
			got := plannedTargets(dp)
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("want no rename, got %v", got)
				}
				return
			}
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("want [%s], got %v", tt.want, got)
			}
		})
	}
}

// TestGoldenMovieNames is the movie golden fixture: real release names in, the
// hand-written library name out. testOptions fixes maxYear so the table never
// changes with the clock.
func TestGoldenMovieNames(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		noMovies bool
		want     []string // expected target base names; nil means no rename
		conflict string   // expected conflict reason, when non-empty
	}{
		{
			name:  "site prefix",
			files: []string{"www.5MovieRulz.company - Jailer 2 (2026) 1080p Telugu HQ HDRip - HEVC - (DD+5.1 - 192Kbps & AAC) - 1.6GB - ESub.mkv"},
			want:  []string{"Jailer 2 (2026).mkv"},
		},
		{
			name:  "year and tags",
			files: []string{"Jailer.2.2026.1080p.Telugu.HQ.HDRip.HEVC.mkv"},
			want:  []string{"Jailer 2 (2026).mkv"},
		},
		{
			name:  "year in parentheses",
			files: []string{"Jailer 2 (2026) Telugu 1080p.mkv"},
			want:  []string{"Jailer 2 (2026).mkv"},
		},
		{
			name:  "no year peels language",
			files: []string{"Jailer.2.Telugu.1080p.HDRip.x264.mkv"},
			want:  []string{"Jailer 2.mkv"},
		},
		{
			name:  "site prefix no year",
			files: []string{"TamilBlasters.xyz - Movie Name - Tamil - 720p - HDRip.mkv"},
			want:  []string{"Movie Name.mkv"},
		},
		{
			name:  "bracket group",
			files: []string{"[YTS.MX] Movie (2020) [1080p].mkv"},
			want:  []string{"Movie (2020).mkv"},
		},
		{
			name:  "title number is not a year",
			files: []string{"Blade Runner 2049 (2017) 1080p.mkv"},
			want:  []string{"Blade Runner 2049 (2017).mkv"},
		},
		{
			name:  "out of range number",
			files: []string{"Blade.Runner.2049.1080p.mkv"},
			want:  []string{"Blade Runner 2049.mkv"},
		},
		{
			name:  "leading year is the title",
			files: []string{"1917.2019.2160p.BluRay.x265-GROUP.mkv"},
			want:  []string{"1917 (2019).mkv"},
		},
		{
			name:  "leading year and no year cut",
			files: []string{"2012.1080p.BluRay.mkv"},
			want:  []string{"2012.mkv"},
		},
		{
			name:  "position zero strong token",
			files: []string{"Cam.2018.1080p.WEBRip.mkv"},
			want:  []string{"Cam (2018).mkv"},
		},
		{
			name:  "glued strong token",
			files: []string{"Camera.2019.1080p.mkv"},
			want:  []string{"Camera (2019).mkv"},
		},
		{
			name:  "bare web is not a token",
			files: []string{"Charlottes.Web.2006.720p.mkv"},
			want:  []string{"Charlottes Web (2006).mkv"},
		},
		{
			name:  "year skips the peel",
			files: []string{"Johnny.English.2003.1080p.mkv"},
			want:  []string{"Johnny English (2003).mkv"},
		},
		{
			name:  "dots without a token",
			files: []string{"Movie.Name.2026.mkv"},
			want:  []string{"Movie Name (2026).mkv"},
		},
		{
			name:  "no year no tokens",
			files: []string{"Movie.1080p.WEB-DL.mkv"},
			want:  []string{"Movie.mkv"},
		},
		{
			name:  "dotted title is not a site",
			files: []string{"Dr.No - 1962 BluRay.mkv"},
			want:  []string{"Dr No (1962).mkv"},
		},
		{
			name:  "dotted title before dash keeps title",
			files: []string{"Mr.Bean - The Movie (1997) 1080p.mkv"},
			want:  []string{"Mr Bean - The Movie (1997).mkv"},
		},
		{
			name:  "cam is a title word",
			files: []string{"The.Cam.Girl.2020.1080p.mkv"},
			want:  []string{"The Cam Girl (2020).mkv"},
		},
		{
			name:  "dd and dv are title words",
			files: []string{"The.Dd.Dv.Movie.2020.1080p.mkv"},
			want:  []string{"The Dd Dv Movie (2020).mkv"},
		},
		{
			name:  "year past maxYear is a title word",
			files: []string{"Movie.2028.1080p.mkv"},
			want:  []string{"Movie 2028.mkv"},
		},
		{
			name:  "sample skipped",
			files: []string{"Movie.2020.1080p.mkv", "Movie.2020.1080p.sample.mkv", "Movie.2020.1080p-Sample.mkv"},
			want:  []string{"Movie (2020).mkv"},
		},
		{
			name:  "trailer skipped",
			files: []string{"Movie.Name.2020.trailer.mkv", "Movie.Name.Trailer.1080p.mkv"},
		},
		{
			name:  "already renamed",
			files: []string{"Jailer 2 (2026).mkv"},
		},
		{
			name:  "spaces without a token",
			files: []string{"Family Trip 2019.mkv"},
		},
		{
			name:  "season pack",
			files: []string{"Show.S01.1080p.WEB-DL.mkv"},
		},
		{
			name:  "season words",
			files: []string{"Show.Season.2.720p.mkv"},
		},
		{
			name:  "anime numbering",
			files: []string{"[SubsPlease] Frieren - 12 (1080p).mkv"},
		},
		{
			name:  "episode words",
			files: []string{"Show.Ep.12.720p.mkv"},
		},
		{
			name:  "digits only",
			files: []string{"12345.1080p.mkv"},
		},
		{
			name:  "episode skipped",
			files: []string{"Show.S01E01.1080p.mkv"},
		},
		{
			name:     "opt-in",
			files:    []string{"Movie.2026.1080p.mkv"},
			noMovies: true,
		},
		{
			name:  "sidecar",
			files: []string{"Movie.2026.1080p.mkv", "Movie.2026.1080p.en.srt"},
			want:  []string{"Movie (2026).mkv", "Movie (2026).en.srt"},
		},
		{
			name:     "duplicate target",
			files:    []string{"Movie.2026.1080p.mkv", "Movie.2026.720p.mkv"},
			conflict: "duplicate target",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tt.files {
				writeFile(t, dir, name)
			}
			o := movieOptions()
			if tt.noMovies {
				o = testOptions()
			}
			dp, err := planDir(dir, nil, o)
			if err != nil {
				t.Fatal(err)
			}
			if tt.conflict != "" {
				if len(dp.conflicts) != 1 || dp.conflicts[0].Reason != tt.conflict {
					t.Fatalf("want conflict %q, got %+v", tt.conflict, dp.conflicts)
				}
				return
			}
			if len(dp.conflicts) != 0 {
				t.Fatalf("unexpected conflicts: %+v", dp.conflicts)
			}
			got := plannedTargets(dp)
			if tt.want == nil {
				if len(got) != 0 {
					t.Fatalf("want no rename, got %v", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("want %v, got %v", tt.want, got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("want %v, got %v", tt.want, got)
				}
			}
		})
	}
}

func TestSidecars(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"Show.Name.S04E01.1080p.mkv",
		"Show.Name.S04E01.1080p.en.srt",
		"Show.Name.S04E01.1080p.nfo",
		"Orphan.S02E01.en.srt",
	} {
		writeFile(t, dir, name)
	}

	dp, err := planDir(dir, nil, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	got := plannedTargets(dp)
	want := []string{"Show Name - S04E01.mkv", "Show Name - S04E01.en.srt", "Show Name - S04E01.nfo"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
	o := testOptions()
	o.sidecars = false
	dp, err = planDir(dir, nil, o)
	if err != nil {
		t.Fatal(err)
	}
	if got := plannedTargets(dp); len(got) != 1 || got[0] != "Show Name - S04E01.mkv" {
		t.Fatalf("--no-sidecars planned %v", got)
	}
}

func TestPlanConflicts(t *testing.T) {
	t.Run("duplicate target", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "Show.S01E01.mkv")
		writeFile(t, dir, "Show.S01E01.720p.mkv")
		dp, err := planDir(dir, nil, testOptions())
		if err != nil {
			t.Fatal(err)
		}
		if len(dp.conflicts) != 1 || dp.conflicts[0].Reason != "duplicate target" {
			t.Fatalf("want one duplicate, got %+v", dp.conflicts)
		}
	})

	t.Run("target exists", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "Show - S01E01.mkv")
		writeFile(t, dir, "Show.S01E01.mkv")
		dp, err := planDir(dir, nil, testOptions())
		if err != nil {
			t.Fatal(err)
		}
		if len(dp.conflicts) != 1 || dp.conflicts[0].Reason != "target exists" {
			t.Fatalf("want one existing target, got %+v", dp.conflicts)
		}
		if dp.conflicts[0].detail != "Show - S01E01.mkv" {
			t.Fatalf("wrong detail %q", dp.conflicts[0].detail)
		}
	})

	t.Run("invalid character", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "Show.S01E01.mkv")
		o := testOptions()
		o.title = "Re: Zero"
		dp, err := planDir(dir, nil, o)
		if err != nil {
			t.Fatal(err)
		}
		if len(dp.conflicts) != 1 || dp.conflicts[0].Reason != "invalid output name" {
			t.Fatalf("want invalid output name, got %+v", dp.conflicts)
		}
	})

	t.Run("empty output name", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "S01E01.mkv")
		o := testOptions()
		o.template = " "
		o.title = " " // overrides the directory fallback
		dp, err := planDir(dir, nil, o)
		if err != nil {
			t.Fatal(err)
		}
		if len(dp.conflicts) != 1 || dp.conflicts[0].Reason != "empty output name" {
			t.Fatalf("want empty output name, got %+v", dp.conflicts)
		}
	})
}

func TestApplyAbortsOnConflict(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Show.S01E01.mkv")
	writeFile(t, dir, "Show.S01E01.720p.mkv")

	var out, errOut bytes.Buffer
	if code := run([]string{"--apply", dir}, &out, &errOut); code != 1 {
		t.Fatalf("want exit 1, got %d (%s)", code, errOut.String())
	}
	for _, name := range []string{"Show.S01E01.mkv", "Show.S01E01.720p.mkv"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s was touched: %v", name, err)
		}
	}
	if got := names(t, dir); len(got) != 2 {
		t.Fatalf("batch touched the filesystem: %v", got)
	}
	if !strings.Contains(out.String(), "duplicate target") {
		t.Fatalf("conflict not reported: %s", out.String())
	}
}

// TestApplyFailureOutput proves a failed apply reports the error and the
// applied count on stderr, and prints no summary that claims nothing was
// planned.
func TestApplyFailureOutput(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores a read-only directory")
	}
	dir := t.TempDir()
	writeFile(t, dir, "Show.S01E01.1080p.mkv")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755) // let t.TempDir clean up

	var out, errOut bytes.Buffer
	if code := run([]string{"--apply", dir}, &out, &errOut); code != 1 {
		t.Fatalf("want exit 1, got %d (%s)", code, errOut.String())
	}
	if got := errOut.String(); !strings.Contains(got, "0 of 1 renames applied") {
		t.Fatalf("failure not reported: %q", got)
	}
	if got := out.String(); strings.Contains(got, "0 renames") {
		t.Fatalf("misleading summary after failure: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "Show.S01E01.1080p.mkv")); err != nil {
		t.Fatalf("source was touched: %v", err)
	}
}

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		check   func(*testing.T, options)
	}{
		{name: "template separator", args: []string{"--template", "{title}/{season}"}, wantErr: true},
		{name: "unknown token", args: []string{"--template", "{bogus}"}, wantErr: true},
		{name: "malformed token", args: []string{"--template", "{show"}, wantErr: true},
		{name: "empty template", args: []string{"--template", ""}, wantErr: true},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: true},
		{name: "negative season", args: []string{"--season", "-5"}, wantErr: true},
		{name: "minus one season", args: []string{"--season", "-1"}, wantErr: true},
		{name: "repeated ext", args: []string{"--ext", "mkv", "--ext", "mp4"}, wantErr: true},
		{name: "empty ext", args: []string{"--ext", " , "}, wantErr: true},
		{name: "flags after path", args: []string{"dir", "--apply"}, check: func(t *testing.T, o options) {
			if !o.apply || len(o.paths) != 1 || o.paths[0] != "dir" {
				t.Fatalf("got %+v", o)
			}
		}},
		{name: "aliases", args: []string{"-r", "-q", "--no-sidecars", "dir"}, check: func(t *testing.T, o options) {
			if !o.recursive || !o.quiet || o.sidecars {
				t.Fatalf("got %+v", o)
			}
		}},
		{name: "ext list", args: []string{"--ext", ".MKV, mp4"}, check: func(t *testing.T, o options) {
			if strings.Join(o.exts, ",") != "mkv,mp4" {
				t.Fatalf("got %v", o.exts)
			}
		}},
		{name: "season zero", args: []string{"--season", "0"}, check: func(t *testing.T, o options) {
			if !o.seasonSet || o.season != 0 {
				t.Fatalf("got %+v", o)
			}
		}},
		{name: "tv template default", check: func(t *testing.T, o options) {
			if o.movies || o.template != defaultTemplate {
				t.Fatalf("got %+v", o)
			}
		}},
		{name: "tv template rejects movie tokens", args: []string{"--template", "{title} {year}"}, wantErr: true},
		{name: "movies", args: []string{"--movies"}, check: func(t *testing.T, o options) {
			if !o.movies || o.template != defaultMovieTemplate {
				t.Fatalf("got %+v", o)
			}
		}},
		{name: "movies template", args: []string{"--movies", "--template", "{title} - {year}"}, check: func(t *testing.T, o options) {
			if o.template != "{title} - {year}" {
				t.Fatalf("got %q", o.template)
			}
		}},
		{name: "movies rejects tv tokens", args: []string{"--movies", "--template", "{title} S{season}"}, wantErr: true},
		{name: "movies rejects bogus token", args: []string{"--movies", "--template", "{bogus}"}, wantErr: true},
		{name: "movies rejects separator", args: []string{"--movies", "--template", "{title}/{year}"}, wantErr: true},
		{name: "movies rejects empty template", args: []string{"--movies", "--template", ""}, wantErr: true},
		{name: "movies title", args: []string{"--movies", "--title", "Jailer 2"}, check: func(t *testing.T, o options) {
			if o.title != "Jailer 2" {
				t.Fatalf("got %q", o.title)
			}
		}},
		{name: "movies rejects season", args: []string{"--movies", "--season", "2"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := parseOptions(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %+v", o)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.check != nil {
				tt.check(t, o)
			}
		})
	}
}

// chdirTemp creates a temp directory and makes it the working directory for
// the duration of the test. Argument resolution merges by absolute spelling, so
// relative spellings must be exercised from inside the directory. Tests must
// not call t.Parallel: chdir is process-global.
func chdirTemp(t *testing.T) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
}

// planLines runs the CLI and returns the plan lines as [source, target] base
// names, plus stderr and the exit code.
func planLines(t *testing.T, args []string) (renames [][2]string, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" || strings.HasSuffix(line, "rename") || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "(") {
			continue
		}
		parts := strings.SplitN(line, "  →  ", 2)
		if len(parts) == 2 {
			renames = append(renames, [2]string{filepath.Base(strings.TrimSpace(parts[0])), filepath.Base(strings.TrimSpace(parts[1]))})
		}
	}
	return renames, errOut.String(), code
}

func wantRenames(t *testing.T, got [][2]string, want ...[2]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
}

// seedEpisodeDir writes two episodes plus their sidecars and returns the dir.
func seedEpisodeDir(t *testing.T, extra ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"Show.S01E01.mkv", "Show.S01E01.srt", "Show.S01E02.mkv", "Show.S01E02.srt"} {
		writeFile(t, dir, name)
	}
	for _, name := range extra {
		writeFile(t, dir, name)
	}
	return dir
}

// TestInputScope covers path arguments: a file argument plans only that file
// and the sidecars it owns, overlapping targets plan once, and non-video or
// missing files abort the invocation. Renames are verified through the real
// CLI entry point (run), so targets() and the notice are exercised too.
func TestInputScope(t *testing.T) {
	const (
		s01 = "Show.S01E01.mkv"
		s02 = "Show.S01E02.mkv"
	)
	seed := seedEpisodeDir

	t.Run("directory unchanged", func(t *testing.T) {
		dir := seed(t)
		renames, _, code := planLines(t, []string{dir})
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		wantRenames(t, renames,
			[2]string{s01, "Show - S01E01.mkv"},
			[2]string{"Show.S01E01.srt", "Show - S01E01.srt"},
			[2]string{s02, "Show - S01E02.mkv"},
			[2]string{"Show.S01E02.srt", "Show - S01E02.srt"})
	})

	t.Run("single file with sidecar", func(t *testing.T) {
		dir := seed(t)
		renames, _, code := planLines(t, []string{filepath.Join(dir, s01)})
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		wantRenames(t, renames,
			[2]string{s01, "Show - S01E01.mkv"},
			[2]string{"Show.S01E01.srt", "Show - S01E01.srt"})
	})

	t.Run("movie file leaves sibling episode alone", func(t *testing.T) {
		dir := seed(t, "Jailer.2.2026.1080p.mkv", "Jailer.2.2026.1080p.srt")
		renames, _, code := planLines(t, []string{"--movies", filepath.Join(dir, "Jailer.2.2026.1080p.mkv")})
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		wantRenames(t, renames,
			[2]string{"Jailer.2.2026.1080p.mkv", "Jailer 2 (2026).mkv"},
			[2]string{"Jailer.2.2026.1080p.srt", "Jailer 2 (2026).srt"})
	})

	t.Run("file does not steal sibling sidecar", func(t *testing.T) {
		dir := seed(t, "Show.S01E01.720p.mkv", "Show.S01E01.720p.srt")
		renames, _, code := planLines(t, []string{filepath.Join(dir, s01)})
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		wantRenames(t, renames,
			[2]string{s01, "Show - S01E01.mkv"},
			[2]string{"Show.S01E01.srt", "Show - S01E01.srt"})
	})

	// The cases below use relative spellings from inside the directory: an
	// absolute-only test cannot catch a merge key that is compared against the
	// wrong form of the path.
	t.Run("overlapping arguments plan once", func(t *testing.T) {
		tests := []struct {
			name string
			args []string
			want int
		}{
			{"same dot twice", []string{".", "."}, 2},
			{"dot and dot slash", []string{".", "./"}, 2},
			{"dir twice", []string{"sub", "sub"}, 2},
			{"dir and dot dir", []string{"sub", "./sub"}, 2},
			{"dir and file in it", []string{"sub", "sub/Show.S01E01.mkv"}, 2},
			{"file and its dir", []string{"sub/Show.S01E01.mkv", "sub"}, 2},
			{"dir and abs dir", []string{"sub", ""}, 2}, // abs filled in below
			{"dir and abs file", []string{"sub", ""}, 2},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				chdirTemp(t)
				// Two renames in the root and two in sub, so a ".", "sub" or
				// mixed invocation that plans once yields exactly two.
				writeFile(t, ".", "Root.S01E01.mkv")
				writeFile(t, ".", "Root.S01E01.srt")
				writeFile(t, "sub", "Show.S01E01.mkv")
				writeFile(t, "sub", "Show.S01E01.srt")
				abs, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				args := make([]string, len(tt.args))
				copy(args, tt.args)
				for i, a := range args {
					if a != "" {
						continue
					}
					if strings.Contains(tt.name, "file") {
						args[i] = filepath.Join(abs, "sub", "Show.S01E01.mkv")
					} else {
						args[i] = filepath.Join(abs, "sub")
					}
				}
				renames, stderr, code := planLines(t, args)
				if code != 0 {
					t.Fatalf("exit %d (%s)", code, stderr)
				}
				if len(renames) != tt.want {
					t.Fatalf("args %v: want %d renames, got %d: %v", args, tt.want, len(renames), renames)
				}
				seen := map[string]bool{}
				for _, r := range renames {
					if seen[r[0]] {
						t.Fatalf("args %v: %s planned twice", args, r[0])
					}
					seen[r[0]] = true
				}
			})
		}
	})

	t.Run("recursive arguments plan once", func(t *testing.T) {
		tests := []struct {
			name string
			args []string
		}{
			{"same dot twice", []string{"-r", ".", "."}},
			{"dir twice", []string{"-r", "sub", "sub"}},
			{"dir and dot dir", []string{"-r", "sub", "./sub"}},
			{"dir and nested file", []string{"-r", "sub", "sub/deep/Other.S01E01.mkv"}},
			{"dir and nested dir", []string{"-r", "sub", "sub/deep"}},
			{"nested file then dir", []string{"sub/deep/Other.S01E01.mkv", "-r", "sub"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				chdirTemp(t)
				writeFile(t, "sub", "Show.S01E01.mkv")
				writeFile(t, filepath.Join("sub", "deep"), "Other.S01E01.mkv")
				renames, stderr, code := planLines(t, tt.args)
				if code != 0 {
					t.Fatalf("exit %d (%s)", code, stderr)
				}
				if len(renames) != 2 {
					t.Fatalf("args %v: want 2 renames, got %d: %v", tt.args, len(renames), renames)
				}
			})
		}
	})

	t.Run("case-insensitive file argument", func(t *testing.T) {
		dir := seed(t)
		renames, _, code := planLines(t, []string{filepath.Join(dir, "show.s01e01.mkv")})
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		wantRenames(t, renames,
			[2]string{s01, "Show - S01E01.mkv"},
			[2]string{"Show.S01E01.srt", "Show - S01E01.srt"})
	})

	t.Run("file plus other file", func(t *testing.T) {
		dir := seed(t)
		renames, _, code := planLines(t, []string{filepath.Join(dir, s01), filepath.Join(dir, s02)})
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		if n := len(renames); n != 4 {
			t.Fatalf("want 4 renames, got %d: %v", n, renames)
		}
	})

	t.Run("no arguments default", func(t *testing.T) {
		chdirTemp(t)
		for _, name := range []string{s01, "Show.S01E01.srt", s02, "Show.S01E02.srt"} {
			writeFile(t, ".", name)
		}
		renames, _, code := planLines(t, nil)
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		if n := len(renames); n != 4 {
			t.Fatalf("want 4 renames, got %d", n)
		}
	})

	t.Run("apply single file", func(t *testing.T) {
		dir := seed(t)
		var out, errOut bytes.Buffer
		if code := run([]string{"--apply", filepath.Join(dir, s01)}, &out, &errOut); code != 0 {
			t.Fatalf("exit %d (%s)", code, errOut.String())
		}
		for _, name := range []string{"Show - S01E01.mkv", "Show - S01E01.srt", s02, "Show.S01E02.srt"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, s01)); !os.IsNotExist(err) {
			t.Fatalf("source still present: %v", err)
		}
	})

	t.Run("apply two files conflicting", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "Show.S01E01.mkv")
		writeFile(t, dir, "Show.S01E01.720p.mkv")
		var out, errOut bytes.Buffer
		if code := run([]string{"--apply", filepath.Join(dir, "Show.S01E01.mkv"), filepath.Join(dir, "Show.S01E01.720p.mkv")}, &out, &errOut); code != 1 {
			t.Fatalf("want exit 1, got %d (%s)", code, errOut.String())
		}
		for _, name := range []string{"Show.S01E01.mkv", "Show.S01E01.720p.mkv"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Fatalf("%s was touched: %v", name, err)
			}
		}
		if got := names(t, dir); len(got) != 2 {
			t.Fatalf("batch touched the filesystem: %v", got)
		}
		if !strings.Contains(out.String(), "duplicate target") {
			t.Fatalf("conflict not reported: %s", out.String())
		}
	})

	t.Run("duplicate target detected across spellings", func(t *testing.T) {
		chdirTemp(t)
		// Two files whose targets collide, named with different spellings. A
		// missed merge would plan both and silently drop one.
		writeFile(t, ".", "Show.S01E01.mkv")
		writeFile(t, ".", "Show.S01E01.720p.mkv")
		for _, args := range [][]string{
			{"Show.S01E01.mkv", "./Show.S01E01.720p.mkv"},
			{"--apply", "Show.S01E01.mkv", "./Show.S01E01.720p.mkv"},
			{"--apply", ".", "."},
		} {
			renames, _, code := planLines(t, args)
			if code != 1 {
				t.Fatalf("args %v: want exit 1, got %d (%v)", args, code, renames)
			}
		}
		for _, name := range []string{"Show.S01E01.mkv", "Show.S01E01.720p.mkv"} {
			if _, err := os.Stat(name); err != nil {
				t.Fatalf("%s was touched: %v", name, err)
			}
		}
	})

	t.Run("overlapping scopes never move a file twice", func(t *testing.T) {
		chdirTemp(t)
		writeFile(t, ".", s01)
		writeFile(t, ".", "Show.S01E01.srt")
		for _, args := range [][]string{
			{"--apply", ".", "."},
			{"--apply", ".", "./"},
			{"--apply", s01, "."},
		} {
			// Recreate after each successful apply.
			for _, name := range []string{s01, "Show.S01E01.srt"} {
				if _, err := os.Stat(name); err != nil {
					writeFile(t, ".", name)
				}
			}
			var out, errOut bytes.Buffer
			if code := run(args, &out, &errOut); code != 0 {
				t.Fatalf("args %v: exit %d (%s)", args, code, errOut.String())
			}
			if _, err := os.Stat("Show - S01E01.mkv"); err != nil {
				t.Fatalf("args %v: rename missing: %v", args, err)
			}
			// Restore the source names for the next round.
			for _, r := range [][2]string{{"Show - S01E01.mkv", s01}, {"Show - S01E01.srt", "Show.S01E01.srt"}} {
				if _, err := os.Stat(r[0]); err == nil {
					os.Rename(r[0], r[1])
				}
			}
		}
	})

	t.Run("out-of-scope invalid target does not abort", func(t *testing.T) {
		dir := seed(t, "Foo:Bar.S01E01.mkv")
		renames, stderr, code := planLines(t, []string{filepath.Join(dir, s01)})
		if code != 0 {
			t.Fatalf("want exit 0, got %d (%s)", code, stderr)
		}
		if n := len(renames); n != 2 {
			t.Fatalf("want 2 renames, got %v", renames)
		}
		if strings.Contains(stderr, "invalid output name") {
			t.Fatalf("out-of-scope conflict leaked: %s", stderr)
		}
	})

	t.Run("invalid target reported for the whole directory", func(t *testing.T) {
		dir := seed(t, "Foo:Bar.S01E01.mkv")
		var out, errOut bytes.Buffer
		if code := run([]string{dir}, &out, &errOut); code != 1 {
			t.Fatalf("want exit 1, got %d (%s)", code, errOut.String())
		}
		if !strings.Contains(out.String(), "invalid output name") {
			t.Fatalf("conflict not reported: %s", out.String())
		}
	})

	t.Run("non-video file", func(t *testing.T) {
		dir := seed(t, "notes.txt")
		_, stderr, code := planLines(t, []string{filepath.Join(dir, "notes.txt")})
		if code != 1 {
			t.Fatalf("want exit 1, got %d", code)
		}
		if !strings.Contains(stderr, "notes.txt: not a video file") {
			t.Fatalf("wrong stderr %q", stderr)
		}
	})

	t.Run("non-video sibling still ignored", func(t *testing.T) {
		dir := seed(t, "notes.txt", "poster.jpg")
		if _, stderr, code := planLines(t, []string{dir}); code != 0 {
			t.Fatalf("exit %d (%s)", code, stderr)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		dir := seed(t)
		_, stderr, code := planLines(t, []string{filepath.Join(dir, "missing.mkv")})
		if code != 1 {
			t.Fatalf("want exit 1, got %d", code)
		}
		if !strings.Contains(stderr, "no such file") {
			t.Fatalf("wrong stderr %q", stderr)
		}
	})

	t.Run("nothing to rename", func(t *testing.T) {
		dir := seed(t, "Random.mkv")
		_, stderr, code := planLines(t, []string{filepath.Join(dir, "Random.mkv")})
		if code != 0 {
			t.Fatalf("want exit 0, got %d", code)
		}
		if !strings.Contains(stderr, "nothing to rename") {
			t.Fatalf("notice missing: %q", stderr)
		}
	})

	t.Run("nothing to rename survives absorption", func(t *testing.T) {
		dir := seed(t, "Random.mkv")
		_, stderr, code := planLines(t, []string{dir, filepath.Join(dir, "Random.mkv")})
		if code != 0 {
			t.Fatalf("want exit 0, got %d", code)
		}
		if !strings.Contains(stderr, "nothing to rename") {
			t.Fatalf("notice lost when absorbed by a directory: %q", stderr)
		}
	})

	t.Run("notices follow argument order", func(t *testing.T) {
		dir := seed(t, "Random.mkv")
		writeFile(t, dir, "Show - S01E09.mkv")
		_, stderr, code := planLines(t, []string{
			filepath.Join(dir, "Show - S01E09.mkv"),
			filepath.Join(dir, "Random.mkv"),
		})
		if code != 0 {
			t.Fatalf("want exit 0, got %d", code)
		}
		first := strings.Index(stderr, "Show - S01E09.mkv")
		second := strings.Index(stderr, "Random.mkv")
		if first < 0 || second < 0 || first > second {
			t.Fatalf("notice order wrong: %q", stderr)
		}
	})

	t.Run("a repeated file argument yields one notice", func(t *testing.T) {
		chdirTemp(t)
		writeFile(t, ".", "Random.mkv")
		_, stderr, _ := planLines(t, []string{"Random.mkv", "./Random.mkv"})
		if n := strings.Count(stderr, "nothing to rename"); n != 1 {
			t.Fatalf("want 1 notice, got %d: %q", n, stderr)
		}
	})

	t.Run("quiet suppresses the notice", func(t *testing.T) {
		dir := seed(t, "Random.mkv")
		_, stderr, code := planLines(t, []string{"-q", filepath.Join(dir, "Random.mkv")})
		if code != 0 {
			t.Fatalf("want exit 0, got %d", code)
		}
		if stderr != "" {
			t.Fatalf("quiet printed %q", stderr)
		}
	})

	// Regression: case-variant and symlinked spellings of one directory used
	// to plan it twice, so --apply moved the first copy and then failed.
	t.Run("same directory through case and symlink spellings", func(t *testing.T) {
		for _, args := range [][]string{
			{"--apply", "sub", "SUB"},
			{"--apply", "sub", "link"},
			{"--apply", "-r", "sub", "LINK"},
			{"--apply", "sub/Show.S01E01.mkv", "SUB/show.s01e01.mkv"},
		} {
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				chdirTemp(t)
				writeFile(t, "sub", s01)
				if err := os.Symlink("sub", "link"); err != nil {
					t.Fatal(err)
				}
				var out, errOut bytes.Buffer
				if code := run(args, &out, &errOut); code != 0 {
					t.Fatalf("exit %d (%s)", code, errOut.String())
				}
				if !strings.Contains(out.String(), "1 rename, 0 conflicts") {
					t.Fatalf("want one rename, got %s", out.String())
				}
				if _, err := os.Stat(filepath.Join("sub", "Show - S01E01.mkv")); err != nil {
					t.Fatal(err)
				}
			})
		}
	})

	// Regression: the notice compared typed spellings, so an absolute
	// directory plus a relative file in it reported a planned file as
	// "nothing to rename".
	t.Run("no notice for a planned file spelled differently", func(t *testing.T) {
		chdirTemp(t)
		writeFile(t, "sub", s01)
		abs, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{filepath.Join(abs, "sub"), "sub/" + s01},
			{"sub", filepath.Join(abs, "sub", s01)},
			{"-r", ".", "SUB/show.s01e01.mkv"},
		} {
			renames, stderr, code := planLines(t, args)
			if code != 0 || len(renames) != 1 || stderr != "" {
				t.Fatalf("args %v: exit %d, renames %v, stderr %q", args, code, renames, stderr)
			}
		}
	})

	// Regression: -r used to absorb explicit targets the walk never enters
	// (hidden directories), silently dropping them.
	t.Run("recursive keeps explicit targets in hidden directories", func(t *testing.T) {
		for _, args := range [][]string{
			{"-r", ".", ".hid"},
			{"-r", ".", ".hid/Other.S01E01.mkv"},
		} {
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				chdirTemp(t)
				writeFile(t, ".", s01)
				writeFile(t, ".hid", "Other.S01E01.mkv")
				renames, stderr, code := planLines(t, args)
				if code != 0 || stderr != "" {
					t.Fatalf("exit %d (%s)", code, stderr)
				}
				wantRenames(t, renames, [2]string{s01, "Show - S01E01.mkv"}, [2]string{"Other.S01E01.mkv", "Other - S01E01.mkv"})
			})
		}
	})

	t.Run("hidden video file argument", func(t *testing.T) {
		dir := seed(t, ".Show.S01E01.mkv")
		_, stderr, code := planLines(t, []string{filepath.Join(dir, ".Show.S01E01.mkv")})
		if code != 0 {
			t.Fatalf("want exit 0, got %d", code)
		}
		if !strings.Contains(stderr, "nothing to rename") {
			t.Fatalf("notice missing: %q", stderr)
		}
	})
}

// TestMovieTitleOverride: --title replaces the detected movie title and keeps
// the detected year.
func TestMovieTitleOverride(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Jailer.2.2026.1080p.mkv")
	o := movieOptions()
	o.title = "Jailer II"
	dp, err := planDir(dir, nil, o)
	if err != nil {
		t.Fatal(err)
	}
	if got := plannedTargets(dp); len(got) != 1 || got[0] != "Jailer II (2026).mkv" {
		t.Fatalf("got %v", got)
	}
}

// TestMovieFolderRename: in --movies mode a directory named on the command
// line is renamed with the movie template, while nested directories are not.
func TestMovieFolderRename(t *testing.T) {
	const (
		folder = "All We Imagine As Light (2024) [720p] [BluRay] [YTS.MX]"
		want   = "All We Imagine As Light (2024)"
	)
	seed := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		dir := filepath.Join(root, folder)
		writeFile(t, dir, "All.We.Imagine.As.Light.2024.720p.BluRay.x264.AAC5.1-[YTS.MX].mp4")
		return dir
	}

	t.Run("folder and file", func(t *testing.T) {
		renames, _, code := planLines(t, []string{"--movies", seed(t)})
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		wantRenames(t, renames,
			[2]string{"All.We.Imagine.As.Light.2024.720p.BluRay.x264.AAC5.1-[YTS.MX].mp4", want + ".mp4"},
			[2]string{folder, want})
	})

	t.Run("tv mode leaves the folder", func(t *testing.T) {
		renames, _, code := planLines(t, []string{seed(t)})
		if code != 0 || len(renames) != 0 {
			t.Fatalf("exit %d, renames %v", code, renames)
		}
	})

	t.Run("template and forced title", func(t *testing.T) {
		renames, _, code := planLines(t, []string{"--movies", "--template", "{title}", "--title", "Forced Name", seed(t)})
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		wantRenames(t, renames,
			[2]string{"All.We.Imagine.As.Light.2024.720p.BluRay.x264.AAC5.1-[YTS.MX].mp4", "Forced Name.mp4"},
			[2]string{folder, "Forced Name"})
	})

	t.Run("nested folder is left alone", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, folder), "All.We.Imagine.As.Light.2024.720p.BluRay.x264.AAC5.1-[YTS.MX].mp4")
		renames, _, code := planLines(t, []string{"--movies", "-r", root})
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		for _, r := range renames {
			if r[0] == folder {
				t.Fatalf("nested folder was renamed: %v", renames)
			}
		}
	})
}

// TestShowFolderRename: in TV mode a season folder named on the command line is
// renamed to <title> - S<NN>; inside its show folder it becomes Season NN.
// Episodes count as their season, and a folder with no season marker or a
// multi-season range is left alone.
func TestShowFolderRename(t *testing.T) {
	root := t.TempDir()
	dir := func(name string) string {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	plan := func(t *testing.T, dir string, o options) ([]rename, []conflict) {
		t.Helper()
		renames, conflicts, err := planShowFolders([]dirTarget{{dir: dir}}, o)
		if err != nil {
			t.Fatal(err)
		}
		return renames, conflicts
	}

	t.Run("season pack", func(t *testing.T) {
		renames, conflicts := plan(t, dir("Show.Name.S04.1080p.WEB-DL-GROUP"), testOptions())
		if len(conflicts) != 0 || len(renames) != 1 {
			t.Fatalf("want one rename, got %+v %+v", renames, conflicts)
		}
		want := filepath.Join(root, "Show Name - S04")
		if renames[0].Target != want {
			t.Fatalf("want %q, got %q", want, renames[0].Target)
		}
	})

	t.Run("episode folder counts as its season", func(t *testing.T) {
		renames, _ := plan(t, dir("Show.Name.S01E05.1080p"), testOptions())
		want := filepath.Join(root, "Show Name - S01")
		if len(renames) != 1 || renames[0].Target != want {
			t.Fatalf("want %q, got %+v", want, renames)
		}
	})

	t.Run("season word", func(t *testing.T) {
		renames, _ := plan(t, dir("Show.Name.Season.3.Complete"), testOptions())
		want := filepath.Join(root, "Show Name - S03")
		if len(renames) != 1 || renames[0].Target != want {
			t.Fatalf("want %q, got %+v", want, renames)
		}
	})

	t.Run("pure season folder stays in place", func(t *testing.T) {
		show := dir("Show Name")
		renames, _ := plan(t, dir(filepath.Join("Show Name", "S04")), testOptions())
		want := filepath.Join(show, "Season 04")
		if len(renames) != 1 || renames[0].Target != want {
			t.Fatalf("want %q, got %+v", want, renames)
		}
	})

	t.Run("no season marker skipped", func(t *testing.T) {
		if renames, conflicts := plan(t, dir("Plain.Show.1080p.WEB-DL"), testOptions()); len(renames) != 0 || len(conflicts) != 0 {
			t.Fatalf("want nothing, got %+v %+v", renames, conflicts)
		}
	})

	t.Run("season range skipped", func(t *testing.T) {
		if renames, conflicts := plan(t, dir("Other.Show.S01-S05.1080p"), testOptions()); len(renames) != 0 || len(conflicts) != 0 {
			t.Fatalf("want nothing, got %+v %+v", renames, conflicts)
		}
	})

	t.Run("title and season override", func(t *testing.T) {
		o := testOptions()
		o.title, o.season, o.seasonSet = "Forced", 7, true
		renames, _ := plan(t, dir("Show.Name.S04.1080p"), o)
		want := filepath.Join(root, "Forced - S07")
		if len(renames) != 1 || renames[0].Target != want {
			t.Fatalf("want %q, got %+v", want, renames)
		}
	})

	t.Run("existing season is a conflict", func(t *testing.T) {
		dir("Show Name - S04")
		renames, conflicts := plan(t, dir("Show.Name.S04.1080p"), testOptions())
		if len(renames) != 0 || len(conflicts) != 1 || conflicts[0].Reason != "target exists" {
			t.Fatalf("want one target-exists conflict, got %+v %+v", renames, conflicts)
		}
	})
}

// TestMovieFolderConflict: an existing folder at the target aborts the run,
// so neither the folder nor the files inside it move.
func TestMovieFolderConflict(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Jailer 2 (2026) 1080p")
	writeFile(t, dir, "Jailer.2.2026.1080p.mkv")
	if err := os.Mkdir(filepath.Join(root, "Jailer 2 (2026)"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"--movies", dir}, &out, &errOut); code != 1 {
		t.Fatalf("want exit 1, got %d (%s)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "target exists") {
		t.Fatalf("conflict not reported: %s", out.String())
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("folder moved: %v", err)
	}
}

func TestRenderMovie(t *testing.T) {
	tests := []struct {
		template string
		title    string
		year     int
		want     string
	}{
		{defaultMovieTemplate, "Jailer 2", 2026, "Jailer 2 (2026)"},
		{defaultMovieTemplate, "Jailer 2", 0, "Jailer 2"},
		{"[{year}] {title}", "Jailer 2", 0, "Jailer 2"},
		{"{title} - {year}", "Jailer 2", 0, "Jailer 2"},
		{"{title}({year})", "Movie", 0, "Movie"},
		{"{year} {title}", "Jailer 2", 2026, "2026 Jailer 2"},
		{"[{year}] {title}", "Jailer 2", 2026, "[2026] Jailer 2"},
	}
	for _, tt := range tests {
		if got := renderMovie(tt.template, tt.title, tt.year); got != tt.want {
			t.Errorf("%q: want %q, got %q", tt.template, tt.want, got)
		}
	}
}

func TestRenderTemplate(t *testing.T) {
	tests := []struct {
		template string
		show     string
		season   int
		eps      []int
		want     string
	}{
		{defaultTemplate, "Show Name", 4, []int{1}, "Show Name - S04E01"},
		{defaultTemplate, "Show Name", 4, []int{3, 4}, "Show Name - S04E03E04"},
		{"{title} S{season}E{episode}", "Show Name", 12, []int{2}, "Show Name S12E02"},
		{"  {title}   {season}  ", " Show  Name ", 1, []int{9}, "Show Name 01"},
		{"S{season}E{episode} {title}", "Show", 0, []int{3}, "S00E03 Show"},
	}
	for _, tt := range tests {
		if got := renderTemplate(tt.template, tt.show, tt.season, tt.eps); got != tt.want {
			t.Errorf("%q: want %q, got %q", tt.template, tt.want, got)
		}
	}
}
