package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func testOptions() options {
	return options{maxYear: 2027}
}

// movieOptions is testOptions in movie mode.
func movieOptions() options {
	o := testOptions()
	o.movies = true
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
		{name: "dir fallback", sub: "Some Show", file: "S01E01.mkv", want: "Some Show - S01E01.mkv"},
		{name: "season folder fallback", sub: filepath.Join("Some Show", "Season 04"), file: "S04E02.mkv", want: "Some Show - S04E02.mkv"},
		{name: "short season folder fallback", sub: filepath.Join("Some Show", "S04"), file: "S04E02.mkv", want: "Some Show - S04E02.mkv"},
		{name: "dir fallback normalized", sub: "Show_Name", file: "S01E01.mkv", want: "Show Name - S01E01.mkv"},
		{name: "release season folder fallback", sub: "Show.Name.S04.1080p.WEB-DL", file: "S04E01.mkv", want: "Show Name - S04E01.mkv"},
		{name: "dash e range", file: "Show.S01E01-E02.1080p.mkv", want: "Show - S01E01E02.mkv"},
		{name: "dash range", file: "Show.S01E03-04.1080p.mkv", want: "Show - S01E03E04.mkv"},
		{name: "dot list", file: "Show.S01E05.E06.1080p.mkv", want: "Show - S01E05E06.mkv"},
		{name: "long range", file: "Show.S01E01-E03.mkv", want: "Show - S01E01E02E03.mkv"},
		{name: "descending range skipped", file: "Show.S01E05-E03.mkv", want: ""},
		{name: "dash before resolution", file: "Show.S01E01-720p.mkv", want: "Show - S01E01.mkv"},
		{name: "x form range", file: "Show.1x01-02.HDTV.mkv", want: "Show - S01E01E02.mkv"},
		{name: "x form repeated range", file: "Show.1x01-1x02.HDTV.mkv", want: "Show - S01E01E02.mkv"},
		{name: "x form dot list", file: "Show.1x01.1x02.HDTV.mkv", want: "Show - S01E01E02.mkv"},
		{name: "x form season change skipped", file: "Show.1x10-2x01.mkv", want: ""},
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

// TestRenameIdempotent: planning a directory again after its renames are
// applied plans nothing, so a second run never renames a file twice.
func TestRenameIdempotent(t *testing.T) {
	tests := []struct {
		sub, file string
		movies    bool
	}{
		{sub: "Show_Name", file: "S01E01.mkv"},
		{sub: "Show.Name.S04.1080p.WEB-DL", file: "S04E01.mkv"},
		{sub: "Some Show", file: "Show.S01E01-E02.1080p.mkv"},
		{sub: "Some Show", file: "Show.1x01-1x02.HDTV.mkv"},
		{sub: "Films", file: "[REC].2007.1080p.mkv", movies: true},
		{sub: "Films", file: "(500).Days.Of.Summer.2009.1080p.mkv", movies: true},
		{sub: "Films", file: "The.Guest.DDP5.1.1080p.mkv", movies: true},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), tt.sub)
			writeFile(t, dir, tt.file)
			o := testOptions()
			if tt.movies {
				o = movieOptions()
			}
			dp, err := planDir(dir, nil, o)
			if err != nil {
				t.Fatal(err)
			}
			if len(dp.renames) != 1 || len(dp.conflicts) != 0 {
				t.Fatalf("first run: want one rename, got %+v %+v", dp.renames, dp.conflicts)
			}
			if err := os.Rename(dp.renames[0].Source, dp.renames[0].Target); err != nil {
				t.Fatal(err)
			}
			again, err := planDir(dir, nil, o)
			if err != nil {
				t.Fatal(err)
			}
			if len(again.renames) != 0 || len(again.conflicts) != 0 {
				t.Fatalf("second run renamed %s: %+v %+v", filepath.Base(dp.renames[0].Target), again.renames, again.conflicts)
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
			name:  "bracketed title glued to year",
			files: []string{"[REC].2007.1080p.mkv"},
			want:  []string{"[REC] (2007).mkv"},
		},
		{
			name:  "parenthesized title start",
			files: []string{"(500).Days.Of.Summer.2009.1080p.mkv"},
			want:  []string{"(500) Days Of Summer (2009).mkv"},
		},
		{
			name:  "glued audio channels",
			files: []string{"The.Guest.DDP5.1.1080p.mkv"},
			want:  []string{"The Guest.mkv"},
		},
		{
			name:  "glued frame rate",
			files: []string{"Some.Film.1080p60.x264.mkv"},
			want:  []string{"Some Film.mkv"},
		},
		{
			name:  "glued aac and dts channels",
			files: []string{"Movie.Name.AAC5.1.mkv", "Other.Name.DTS5.1.mkv"},
			want:  []string{"Movie Name.mkv", "Other Name.mkv"},
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
			name:  "anime version",
			files: []string{"[Grp] Show - 12v2 [1080p].mkv"},
		},
		{
			name:  "anime range",
			files: []string{"[Grp] Show - 12-13 [1080p].mkv"},
		},
		{
			name:  "anime en-dash version",
			files: []string{"[Grp] Show – 12v2 [1080p].mkv"},
		},
		{
			name:  "anime en-dash range",
			files: []string{"[Grp] Show – 12–13 [1080p].mkv"},
		},
		{
			name:  "numbered title",
			files: []string{"The.Taking.of.Pelham.123.2009.1080p.mkv"},
			want:  []string{"The Taking of Pelham 123 (2009).mkv"},
		},
		{
			name:  "numbered title with dash",
			files: []string{"Mission - Impossible 2 (2000) [1080p].mkv"},
			want:  []string{"Mission - Impossible 2 (2000).mkv"},
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
}

// TestSidecarOwnership: each sidecar belongs to exactly one video, so two
// videos sharing a stem rename it once, and an out-of-scope video never
// donates or steals a sidecar.
func TestSidecarOwnership(t *testing.T) {
	seed := func(t *testing.T, files ...string) string {
		t.Helper()
		dir := t.TempDir()
		for _, name := range files {
			writeFile(t, dir, name)
		}
		return dir
	}
	plan := func(t *testing.T, dir string, scope ...string) dirPlan {
		t.Helper()
		var names map[string]bool
		if len(scope) > 0 {
			names = map[string]bool{}
			for _, n := range scope {
				names[scopeKey(n)] = true
			}
		}
		dp, err := planDir(dir, names, testOptions())
		if err != nil {
			t.Fatal(err)
		}
		if len(dp.conflicts) != 0 {
			t.Fatalf("unexpected conflicts: %+v", dp.conflicts)
		}
		return dp
	}
	want := func(t *testing.T, dp dirPlan, want ...string) {
		t.Helper()
		got := plannedTargets(dp)
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
	shared := []string{"Show.S01E01.mkv", "Show.S01E01.mp4", "Show.S01E01.en.srt"}

	t.Run("shared stem renames sidecar once", func(t *testing.T) {
		dp := plan(t, seed(t, shared...))
		want(t, dp, "Show - S01E01.mkv", "Show - S01E01.mp4", "Show - S01E01.en.srt")
	})

	t.Run("file-scoped mkv keeps its sidecar", func(t *testing.T) {
		dp := plan(t, seed(t, shared...), "Show.S01E01.mkv")
		want(t, dp, "Show - S01E01.mkv", "Show - S01E01.en.srt")
	})

	t.Run("file-scoped mp4 keeps its sidecar", func(t *testing.T) {
		dp := plan(t, seed(t, shared...), "Show.S01E01.mp4")
		want(t, dp, "Show - S01E01.mp4", "Show - S01E01.en.srt")
	})

	t.Run("out-of-scope longer stem keeps its sidecar", func(t *testing.T) {
		dir := seed(t, "Show.S01E01.mkv", "Show.S01E01.720p.mkv", "Show.S01E01.720p.srt")
		want(t, plan(t, dir, "Show.S01E01.mkv"), "Show - S01E01.mkv")
	})
}

// TestSidecarCase: sidecar stems match case-insensitively, like scopeKey,
// and keep their own suffix spelling.
func TestSidecarCase(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Show.S01E01.1080p.mkv")
	writeFile(t, dir, "show.s01e01.1080p.EN.srt")
	dp, err := planDir(dir, nil, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	got := plannedTargets(dp)
	sort.Strings(got)
	want := []string{"Show - S01E01.EN.srt", "Show - S01E01.mkv"}
	if len(dp.conflicts) != 0 || strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("want %v, got %v %+v", want, got, dp.conflicts)
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
		writeFile(t, dir, "Some.Movie.1080p.mkv")
		o := movieOptions()
		o.title = " " // no year, so the name renders empty
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
		{name: "unknown flag", args: []string{"--nope"}, wantErr: true},
		{name: "negative season", args: []string{"--season", "-5"}, wantErr: true},
		{name: "minus one season", args: []string{"--season", "-1"}, wantErr: true},
		{name: "flags after path", args: []string{"dir", "--apply"}, check: func(t *testing.T, o options) {
			if !o.apply || len(o.paths) != 1 || o.paths[0] != "dir" {
				t.Fatalf("got %+v", o)
			}
		}},
		{name: "short aliases", args: []string{"-a", "-t", "Show", "-s", "2", "-V", "dir"}, check: func(t *testing.T, o options) {
			if !o.apply || o.title != "Show" || !o.seasonSet || o.season != 2 || !o.version {
				t.Fatalf("got %+v", o)
			}
		}},
		{name: "short movies", args: []string{"-m"}, check: func(t *testing.T, o options) {
			if !o.movies {
				t.Fatalf("got %+v", o)
			}
		}},
		{name: "short negative season", args: []string{"-s", "-1"}, wantErr: true},
		{name: "season zero", args: []string{"--season", "0"}, check: func(t *testing.T, o options) {
			if !o.seasonSet || o.season != 0 {
				t.Fatalf("got %+v", o)
			}
		}},
		{name: "movies", args: []string{"--movies"}, check: func(t *testing.T, o options) {
			if !o.movies {
				t.Fatalf("got %+v", o)
			}
		}},
		{name: "movies title", args: []string{"--movies", "--title", "Jailer 2"}, check: func(t *testing.T, o options) {
			if o.title != "Jailer 2" {
				t.Fatalf("got %q", o.title)
			}
		}},
		{name: "movies rejects season", args: []string{"--movies", "--season", "2"}, wantErr: true},
		{name: "terminator keeps trailing apply as path", args: []string{"--", "Show.S01E01.mkv", "-a"}, check: func(t *testing.T, o options) {
			if o.apply || strings.Join(o.paths, "|") != "Show.S01E01.mkv|-a" {
				t.Fatalf("got %+v", o)
			}
		}},
		{name: "terminator after path keeps flag as path", args: []string{"dir", "--", "-m"}, check: func(t *testing.T, o options) {
			if o.movies || strings.Join(o.paths, "|") != "dir|-m" {
				t.Fatalf("got %+v", o)
			}
		}},
		{name: "terminator keeps long apply as path", args: []string{"--", "--apply"}, check: func(t *testing.T, o options) {
			if o.apply || strings.Join(o.paths, "|") != "--apply" {
				t.Fatalf("got %+v", o)
			}
		}},
		{name: "terminator preserves path order", args: []string{"a", "-m", "b", "--", "c", "-d"}, check: func(t *testing.T, o options) {
			if !o.movies || strings.Join(o.paths, "|") != "a|b|c|-d" {
				t.Fatalf("got %+v", o)
			}
		}},
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

	// Regression: case-variant and symlinked spellings of one directory used
	// to plan it twice, so --apply moved the first copy and then failed.
	t.Run("same directory through case and symlink spellings", func(t *testing.T) {
		for _, args := range [][]string{
			{"--apply", "sub", "SUB"},
			{"--apply", "sub", "link"},
			{"--apply", "link", "SUB"},
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
		} {
			renames, stderr, code := planLines(t, args)
			if code != 0 || len(renames) != 1 || stderr != "" {
				t.Fatalf("args %v: exit %d, renames %v, stderr %q", args, code, renames, stderr)
			}
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
		folder = "Inception (2010) [720p] [BluRay] [YTS.MX]"
		want   = "Inception (2010)"
	)
	seed := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		dir := filepath.Join(root, folder)
		writeFile(t, dir, "Inception.2010.720p.BluRay.x264.AAC5.1-[YTS.MX].mp4")
		return dir
	}

	t.Run("folder and file", func(t *testing.T) {
		renames, _, code := planLines(t, []string{"--movies", seed(t)})
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		wantRenames(t, renames,
			[2]string{"Inception.2010.720p.BluRay.x264.AAC5.1-[YTS.MX].mp4", want + ".mp4"},
			[2]string{folder, want})
	})

	t.Run("tv mode leaves the folder", func(t *testing.T) {
		renames, _, code := planLines(t, []string{seed(t)})
		if code != 0 || len(renames) != 0 {
			t.Fatalf("exit %d, renames %v", code, renames)
		}
	})

	t.Run("forced title", func(t *testing.T) {
		renames, _, code := planLines(t, []string{"--movies", "--title", "Forced Name", seed(t)})
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		wantRenames(t, renames,
			[2]string{"Inception.2010.720p.BluRay.x264.AAC5.1-[YTS.MX].mp4", "Forced Name (2010).mp4"},
			[2]string{folder, "Forced Name (2010)"})
	})

	t.Run("episode folder is left alone", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "Show.Name.S01E05.1080p")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		renames, _, code := planLines(t, []string{"--movies", dir})
		if code != 0 || len(renames) != 0 {
			t.Fatalf("exit %d, renames %v", code, renames)
		}
	})

	t.Run("nested folder is left alone", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, folder), "Inception.2010.720p.BluRay.x264.AAC5.1-[YTS.MX].mp4")
		renames, _, code := planLines(t, []string{"--movies", root})
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

// TestFolderTargetIdentity: folder targets are keyed by the physical parent
// plus the folded base name, so mixed spellings of one target conflict and
// nothing moves, while a target that is itself a planned source stays allowed.
func TestFolderTargetIdentity(t *testing.T) {
	mkdirs := func(t *testing.T, names ...string) {
		t.Helper()
		for _, name := range names {
			if err := os.MkdirAll(name, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	// wantAbort runs with --apply and expects a duplicate-target conflict,
	// exit 1, and every listed folder still in place.
	wantAbort := func(t *testing.T, args []string, keep ...string) {
		t.Helper()
		var out, errOut bytes.Buffer
		if code := run(append([]string{"--apply"}, args...), &out, &errOut); code != 1 {
			t.Fatalf("want exit 1, got %d: %s%s", code, out.String(), errOut.String())
		}
		if !strings.Contains(out.String(), "duplicate target") {
			t.Fatalf("duplicate not reported: %s", out.String())
		}
		for _, name := range keep {
			if _, err := os.Lstat(name); err != nil {
				t.Fatalf("%s moved: %v", name, err)
			}
		}
	}
	abs := func(t *testing.T, name string) string {
		t.Helper()
		wd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		return filepath.Join(wd, name)
	}

	t.Run("movie relative and absolute", func(t *testing.T) {
		chdirTemp(t)
		mkdirs(t, "Movie.2020.1080p", "Movie (2020) [720p]")
		wantAbort(t, []string{"-m", "Movie.2020.1080p", abs(t, "Movie (2020) [720p]")},
			"Movie.2020.1080p", "Movie (2020) [720p]")
	})

	t.Run("show relative and absolute", func(t *testing.T) {
		chdirTemp(t)
		mkdirs(t, "Show.S02.WEB", "Show.S02.1080p")
		wantAbort(t, []string{"Show.S02.WEB", abs(t, "Show.S02.1080p")},
			"Show.S02.WEB", "Show.S02.1080p")
	})

	t.Run("movie through symlinked parent", func(t *testing.T) {
		chdirTemp(t)
		mkdirs(t, "real/Movie.2020.1080p", "real/Movie (2020) [720p]")
		if err := os.Symlink("real", "lnk"); err != nil {
			t.Fatal(err)
		}
		wantAbort(t, []string{"-m", "lnk/Movie.2020.1080p", "real/Movie (2020) [720p]"},
			"real/Movie.2020.1080p", "real/Movie (2020) [720p]")
	})

	t.Run("show through symlinked parent", func(t *testing.T) {
		chdirTemp(t)
		mkdirs(t, "real/Show.S02.WEB", "real/Show.S02.1080p")
		if err := os.Symlink("real", "lnk"); err != nil {
			t.Fatal(err)
		}
		wantAbort(t, []string{"lnk/Show.S02.WEB", "real/Show.S02.1080p"},
			"real/Show.S02.WEB", "real/Show.S02.1080p")
	})

	// The naming rules are idempotent, so a canonical folder never renames
	// to another name: the reachable chain is a case-only show-folder rename,
	// whose target is its own planned source and exists on the
	// case-insensitive filesystem. It must not be reported as "target
	// exists". detectMovie rejects an already-canonical movie name, so movie
	// folders have no reachable chain.
	t.Run("show target is a planned source", func(t *testing.T) {
		chdirTemp(t)
		mkdirs(t, "show - s02")
		var out, errOut bytes.Buffer
		if code := run([]string{"--apply", "--title", "Show", "show - s02"}, &out, &errOut); code != 0 {
			t.Fatalf("exit %d: %s%s", code, out.String(), errOut.String())
		}
		if got := names(t, "."); len(got) != 1 || got[0] != "Show - S02" {
			t.Fatalf("want [Show - S02], got %v", got)
		}
	})
}

func TestRenderMovie(t *testing.T) {
	tests := []struct {
		title string
		year  int
		want  string
	}{
		{"Jailer 2", 2026, "Jailer 2 (2026)"},
		{"Jailer 2", 0, "Jailer 2"},
	}
	for _, tt := range tests {
		if got := renderMovie(tt.title, tt.year); got != tt.want {
			t.Errorf("%q %d: want %q, got %q", tt.title, tt.year, tt.want, got)
		}
	}
}

func TestRenderEpisode(t *testing.T) {
	tests := []struct {
		show   string
		season int
		eps    []int
		want   string
	}{
		{"Show Name", 4, []int{1}, "Show Name - S04E01"},
		{"Show Name", 4, []int{3, 4}, "Show Name - S04E03E04"},
		{" Show  Name ", 0, []int{9}, "Show Name - S00E09"},
	}
	for _, tt := range tests {
		if got := renderEpisode(tt.show, tt.season, tt.eps); got != tt.want {
			t.Errorf("%q: want %q, got %q", tt.show, tt.want, got)
		}
	}
}
