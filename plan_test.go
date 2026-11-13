package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testOptions() options {
	return options{template: defaultTemplate, sidecars: true, exts: defaultExts()}
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
		{name: "forced show", file: "Show.Name.S04E01.1080p.mkv", setup: func(o *options) { o.show = "Jack Ryan" }, want: "Jack Ryan - S04E01.mkv"},
		{name: "forced season", file: "Show.Name.S04E01.1080p.mkv", setup: func(o *options) { o.season, o.seasonSet = 2, true }, want: "Show Name - S02E01.mkv"},
		{name: "custom template", file: "Show.Name.S04E01.1080p.mkv", setup: func(o *options) {
			o.template = "{show} S{season}E{episode}"
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
			dp, err := planDir(dir, o)
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

	dp, err := planDir(dir, testOptions())
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
	dp, err = planDir(dir, o)
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
		dp, err := planDir(dir, testOptions())
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
		dp, err := planDir(dir, testOptions())
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
		o.show = "Re: Zero"
		dp, err := planDir(dir, o)
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
		o.show = " " // overrides the directory fallback
		dp, err := planDir(dir, o)
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

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		check   func(*testing.T, options)
	}{
		{name: "template separator", args: []string{"--template", "{show}/{season}"}, wantErr: true},
		{name: "unknown token", args: []string{"--template", "{bogus}"}, wantErr: true},
		{name: "malformed token", args: []string{"--template", "{show"}, wantErr: true},
		{name: "empty template", args: []string{"--template", ""}, wantErr: true},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: true},
		{name: "negative season", args: []string{"--season", "-5"}, wantErr: true},
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
		{"{show} S{season}E{episode}", "Show Name", 12, []int{2}, "Show Name S12E02"},
		{"  {show}   {season}  ", " Show  Name ", 1, []int{9}, "Show Name 01"},
		{"S{season}E{episode} {show}", "Show", 0, []int{3}, "S00E03 Show"},
	}
	for _, tt := range tests {
		if got := renderTemplate(tt.template, tt.show, tt.season, tt.eps); got != tt.want {
			t.Errorf("%q: want %q, got %q", tt.template, tt.want, got)
		}
	}
}
